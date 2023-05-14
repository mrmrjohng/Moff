package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/fatih/structs"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strconv"
	"time"
)

const (
	guildInvitesCacheKeyPrefix = "guild_invites:"
)

var (
	// 邀请者队列, 用于GuildMemberAddEventHandler读取加入工会的成员的邀请者:
	// 用户加工会事件发生时，触发daemon去discord拉取最新的invites列表，
	// 同本地缓存邀请列表进行比较，当邀请者的邀请使用数增加N,会把N份邀请者放入该队列
	inviterMatchPipes chan *inviterMatchPipe
)

type inviterMatchPipe struct {
	guildID             string
	inviteeID           string
	inviterNotification chan *guildInviter
}

type guildInviter struct {
	Invite     *discordgo.Invite `json:"invite"`
	BufferTime time.Time         `json:"buffer_time"`
}

func (guildInviter) Dequeue(ctx context.Context, guildID string) (*guildInviter, error) {
	result, err := cache.Redis.RPop(ctx, fmt.Sprintf("%v%v", discordPotentialInviterCacheKeyPrefix, guildID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.WrapfAndReport(err, "query potential inviter from cache")
	}
	var inviter guildInviter
	if err := json.Unmarshal([]byte(result), &inviter); err != nil {
		return nil, errors.WrapfAndReport(err, "unmarshal potential inviter")
	}
	return &inviter, nil
}

func newInviterMatchPipe(guildID, inviteeID string) *inviterMatchPipe {
	return &inviterMatchPipe{
		guildID:             guildID,
		inviteeID:           inviteeID,
		inviterNotification: make(chan *guildInviter, 2),
	}
}

const (
	discordPotentialInviterCacheKeyPrefix = "discord_potential_inviter:"
)

func blockingTrackGuildInviter(ctx context.Context, s *discordgo.Session) {
	log.Info("Discord guild inviter tracker running...")
	defer func() {
		if i := recover(); i != nil {
			log.Errorf("guild inviter tracker panic:%v", i)
		}
		log.Info("Discord guild inviter tracker stopped...")
	}()
	for pipe := range inviterMatchPipes {
		var (
			foundPotential bool
		)
		// 潜在的工会邀请人
		for {
			potential, err := guildInviter{}.Dequeue(ctx, pipe.guildID)
			if err != nil {
				log.Error(err)
				continue
			}
			// 找不到潜在的邀请人
			if potential == nil {
				break
			}
			sec := time.Since(potential.BufferTime).Milliseconds()
			if sec > 5000 {
				log.Warnf("drop potential inviter %v which cached %v milliseconds", potential.Invite.Inviter.ID, sec)
				continue
			}
			pipe.inviterNotification <- potential
			foundPotential = true
			break
		}
		if foundPotential {
			continue
		}

		// 查找当前工会的邀请记录与缓存记录进行对比, 查找潜在的邀请人
		latestGuildInvites, err := GetGuildInvitesFromDiscord(s, &discordgo.UserGuild{ID: pipe.guildID})
		if err != nil {
			log.Errorf("discord guild inviter:%v", err)
			continue
		}
		latestMemberInvites := deduplicateGuildInvites(latestGuildInvites[pipe.guildID])
		cachedInvites, err := cache.Redis.HGetAll(ctx, fmt.Sprintf("%v%v", guildInvitesCacheKeyPrefix, pipe.guildID)).Result()
		if err != nil {
			log.Errorf("query cache guild invites:%v", err)
			continue
		}
		var (
			inviterMatched         bool
			potentialInvitersCache []interface{}
			invitesCacheValues     []interface{}
		)
		cached := convertMapStringValueToInt(cachedInvites)
		guildInvitesCacheKey := fmt.Sprintf("%v%v", guildInvitesCacheKeyPrefix, pipe.guildID)
		for inviterKey, invite := range latestMemberInvites {
			invitesCacheValues = append(invitesCacheValues, inviterKey, invite.Uses)
			used := cached[inviterKey]
			// TODO 此处需要根据使用次数、缓存对应次数该用户
			if invite.Uses > used {
				// 检查是否已经触发邀请者匹配，有更多的邀请者则把邀请者放入潜在的邀请者队列
				if !inviterMatched {
					inviterMatched = true
					pipe.inviterNotification <- &guildInviter{
						Invite:     invite,
						BufferTime: time.Now(),
					}
					log.Infof("Discord invite matched: inviter %v code %v, invitee %v", invite.Inviter.ID,
						invite.Code, pipe.inviteeID)
					// 尝试把该用户的邀请的使用数+1
					if e := cache.Redis.HIncrBy(ctx, guildInvitesCacheKey, inviterKey, 1).Err(); e != nil {
						log.Error(errors.WrapAndReport(err, "increment user invite use count"))
					}
					continue
				}
				log.Debugf("discord potential inviter %v found:latest %v, used %v", invite.Inviter.ID, invite.Uses, used)
				var inviterJSON []byte
				// 同时多次使用?实际此时没法判定，此时仅按一个计算，多余的按找不到处理
				inviterJSON, err = json.Marshal(guildInviter{
					Invite:     invite,
					BufferTime: time.Now(),
				})
				if err != nil {
					err = errors.WrapAndReport(err, "marshal potential inviter")
					break
				}
				potentialInvitersCache = append(potentialInvitersCache, string(inviterJSON))
			}
		}
		// 先检查是否匹配到邀请者，再检查是否发生错误
		if !inviterMatched {
			pipe.inviterNotification <- &guildInviter{}
		}
		if err != nil {
			log.Error(err)
			continue
		}

		// 缓存潜在邀请者队列与邀请数快照
		potentialInvitersCacheKey := fmt.Sprintf("%v%v", discordPotentialInviterCacheKeyPrefix, pipe.guildID)
		_, err = cache.Redis.TxPipelined(ctx, func(pipeliner redis.Pipeliner) error {
			if len(invitesCacheValues) > 0 {
				if err := pipeliner.HMSet(ctx, guildInvitesCacheKey, invitesCacheValues...).Err(); err != nil {
					return errors.WrapAndReport(err, "cache guild invites")
				}
			}
			if len(potentialInvitersCache) > 0 {
				if err := pipeliner.LPush(ctx, potentialInvitersCacheKey, potentialInvitersCache...).Err(); err != nil {
					return errors.WrapAndReport(err, "cache potential inviters")
				}
			}
			return nil
		})
		if err != nil {
			log.Error(errors.Wrap(err, "exec redis tx pipelined"))
		}
	}
}

func convertMapStringValueToInt(m map[string]string) map[string]int {
	result := make(map[string]int, len(m))
	for k, v := range m {
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			log.Errorf("guild inviter %v cache value not int", i)
			continue
		}
		result[k] = int(i)
	}
	return result
}

func initializeGuildInvites(s *discordgo.Session, guilds []*discordgo.UserGuild) error {
	log.Info("Initializing guild invites...")
	defer log.Info("Initializing guild invites done...")
	invites, err := GetGuildInvitesFromDiscord(s, guilds...)
	if err != nil {
		return err
	}
	return cacheGuildInvites(invites)
}

func GetGuildInvitesFromDiscord(s *discordgo.Session, guilds ...*discordgo.UserGuild) (map[string][]*discordgo.Invite, error) {
	log.Info("Query guild invites from discord...")
	defer log.Info("Query guild invites from discord done...")
	var (
		guildInvites = make(map[string][]*discordgo.Invite)
	)
	for _, guild := range guilds {
		invites, err := s.GuildInvites(guild.ID)
		if err != nil {
			return nil, errors.WrapfAndReport(err, "query guild %v invites from discord", guild.ID)
		}
		guildInvites[guild.ID] = invites
	}
	return guildInvites, nil
}

func cacheGuildInvites(guildInvites map[string][]*discordgo.Invite) error {
	ctx := context.TODO()
	for guildID, invites := range guildInvites {
		if len(invites) == 0 {
			log.Infof("skip cache zero invites from guild %v", guildID)
			continue
		}
		memberInvite := deduplicateGuildInvites(invites)
		guildCacheKey := fmt.Sprintf("%v%v", guildInvitesCacheKeyPrefix, guildID)
		var values []interface{}
		for userid, invite := range memberInvite {
			values = append(values, userid, invite.Uses)
		}
		if err := cache.Redis.HMSet(ctx, guildCacheKey, values...).Err(); err != nil {
			return errors.WrapAndReport(err, "cache guild invites record")
		}
		log.Infof("Cached guild %v invites num %v", guildID, len(invites))
	}
	return nil
}

func deduplicateGuildInvites(invites []*discordgo.Invite) map[string]*discordgo.Invite {
	num := make(map[string]*discordgo.Invite)
	for _, invite := range invites {
		inviteKey := fmt.Sprintf("%v:%v", invite.Inviter.ID, invite.Code)
		last := num[inviteKey]
		if last == nil {
			num[inviteKey] = invite
			continue
		}
		// 如果存在重复？则使用最新的那个invitation.
		lastExpireAt := last.CreatedAt.Add(time.Duration(last.MaxAge) * time.Second)
		nowExpireAt := invite.CreatedAt.Add(time.Duration(invite.MaxAge) * time.Second)
		if lastExpireAt.Before(nowExpireAt) {
			invite.Uses += last.Uses
			num[inviteKey] = invite
		} else {
			last.Uses += invite.Uses
			num[inviteKey] = last
		}
	}
	return num
}

func overwriteGuildInvitesScheduler(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for {
		<-ticker.C
		guilds, err := getBotGuildsFromDiscord(session)
		if err != nil {
			log.Error(err)
			continue
		}
		invites, err := GetGuildInvitesFromDiscord(session, guilds...)
		if err != nil {
			log.Error(err)
			continue
		}
		overwriteGuildInvites(invites)
	}
}

func overwriteGuildInvites(guildInvites map[string][]*discordgo.Invite) {
	defer func() {
		if i := recover(); i != nil {
			log.Errorf("overwrite guild invites panic:%v", i)
		}
	}()
	locked, err := cache.Redis.SetNX(context.TODO(), "overwrite_guild_invites_interval", 1, time.Minute*10).Result()
	if err != nil {
		log.Error(errors.WrapAndReport(err, "set overwrite guild invites interval"))
		return
	}
	if !locked {
		return
	}
	for guildID, invites := range guildInvites {
		log.Infof("Overwriting database guild %v invites...", guildID)
		var data []*database.DiscordGuildInvites
		for _, inv := range invites {
			dgi := &database.DiscordGuildInvites{
				GuildID:     inv.Guild.ID,
				ChannelID:   inv.Channel.ID,
				InviterID:   inv.Inviter.ID,
				InviteCode:  inv.Code,
				CreatedAt:   inv.CreatedAt,
				MaxAge:      inv.MaxAge,
				UsedCount:   inv.Uses,
				MaxUseCount: inv.MaxUses,
				Revoked:     inv.Revoked,
				Temporary:   inv.Temporary,
				Unique:      inv.Unique,
				TargetType:  inv.TargetType,
			}
			if inv.TargetUser != nil {
				dgi.TargetUser = structs.Map(inv.TargetUser)
			}
			if inv.TargetApplication != nil {
				dgi.TargetApplication = structs.Map(inv.TargetApplication)
			}
			data = append(data, dgi)
		}
		if len(data) == 0 {
			continue
		}
		err := database.CommunityPostgres.Transaction(func(tx *gorm.DB) error {
			// 删除历史数据
			err := tx.Where("guild_id = ?", guildID).Delete(&database.DiscordGuildInvites{}).Error
			if err != nil {
				return err
			}
			// 使用新数据
			return tx.Create(data).Error
		})
		if err != nil {
			log.Error(errors.WrapAndReport(err, "overwriting guild invites"))
		}
	}
}
