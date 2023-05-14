package discord

import (
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/fatih/structs"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/config"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"sync"
	"time"
)

const (
	botGuildsCacheKeyPrefix = "bot_guilds:"
)

var (
	guildChannelRW sync.RWMutex
	guildChannels  = make(map[string]map[string]*discordgo.Channel)

	memGuildsLock sync.RWMutex
	memGuilds     = make(map[string]*discordgo.UserGuild)
)

func botGuild(guildID string) *discordgo.UserGuild {
	memGuildsLock.Lock()
	defer memGuildsLock.Unlock()

	if memGuilds[guildID] != nil {
		return memGuilds[guildID]
	}

	// 内存不存在，直接查询
	guild, err := session.Guild(guildID)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query guild"))
		return nil
	}
	if guild != nil {
		g := &discordgo.UserGuild{
			ID:          guild.ID,
			Name:        guild.Name,
			Icon:        guild.Icon,
			Owner:       guild.Owner,
			Permissions: guild.Permissions,
		}
		memGuilds[g.ID] = g
		return g
	}
	return nil
}

func initializeBotGuilds(s *discordgo.Session) ([]*discordgo.UserGuild, error) {
	log.Info("Initializing bot guilds...")
	defer log.Info("Initializing bot guilds done...")
	guilds, err := getBotGuildsFromDiscord(s)
	if err != nil {
		return nil, err
	}
	if err := cacheBotGuilds(guilds); err != nil {
		return nil, err
	}

	// 缓存工会信息
	var dbguilds []*database.UserGuild
	for _, guild := range guilds {
		dbguilds = append(dbguilds, &database.UserGuild{
			UserID:     config.Global.DiscordBot.AppID,
			GuildID:    guild.ID,
			GuildName:  guild.Name,
			Permission: guild.Permissions,
		})
	}
	err = database.UserGuild{}.BatchSave(dbguilds)
	if err != nil {
		return nil, err
	}

	// 缓存频道信息
	for _, guild := range guilds {
		channels, err := s.GuildChannels(guild.ID)
		if err != nil {
			return nil, err
		}
		channelMapping := make(map[string]*discordgo.Channel)
		for _, ch := range channels {
			channelMapping[ch.ID] = ch
		}
		guildChannels[guild.ID] = channelMapping
	}
	return guilds, nil
}

func getBotGuildsFromDiscord(s *discordgo.Session) ([]*discordgo.UserGuild, error) {
	var (
		limit       = 100
		total       []*discordgo.UserGuild
		lastGuildID string
	)
	for {
		// 拉取指定id后的工会信息
		guilds, err := s.UserGuilds(limit, "", lastGuildID)
		if err != nil {
			return nil, errors.WrapAndReport(err, "query bot guilds from discord")
		}
		// 获取最后的工会id以拉取该id后的工会
		if len(guilds) > 0 {
			lastGuildID = guilds[len(guilds)-1].ID
		}
		total = append(total, guilds...)
		if len(guilds) < limit {
			// 没有更多的工会，直接返回
			return total, nil
		}
	}
}

func cacheBotGuilds(guilds []*discordgo.UserGuild) error {
	ctx := context.TODO()
	for _, guild := range guilds {
		memGuilds[guild.ID] = guild
		guildCacheKey := fmt.Sprintf("%v%v", botGuildsCacheKeyPrefix, guild.ID)
		err := cache.Redis.HMSet(ctx, guildCacheKey,
			"name", guild.Name,
			"icon", guild.Icon,
			"permissions", guild.Permissions).Err()
		if err != nil {
			return errors.WrapAndReport(err, "cache guild into redis")
		}
	}

	return nil
}

type GuildRole struct {
	GuildID string
	Roles   []*discordgo.Role
}

func getGuildsRolesFromDiscord(s *discordgo.Session, guilds ...*discordgo.UserGuild) ([]*GuildRole, error) {
	var roles []*GuildRole
	for _, guild := range guilds {
		result, err := s.GuildRoles(guild.ID)
		if err != nil {
			return nil, errors.WrapAndReport(err, "query guild roles from discord")
		}
		if len(result) == 0 {
			continue
		}
		var noneBotsRoles []*discordgo.Role
		for _, r := range result {
			if r.Managed {
				continue
			}
			noneBotsRoles = append(noneBotsRoles, r)
		}
		if len(noneBotsRoles) == 0 {
			continue
		}
		roles = append(roles, &GuildRole{
			GuildID: guild.ID,
			Roles:   result,
		})
	}
	return roles, nil
}

func guildMemberUpdateEventHandler(s *discordgo.Session, mem *discordgo.GuildMemberUpdate) {
	dumpEvent(&database.DiscordEvents{
		GuildID:   mem.GuildID,
		EventType: database.DiscordEventTypeGuildMemberUpdate,
		Event:     structs.Map(mem),
		EventTime: time.Now(),
	})
	member := &database.DiscordMember{
		GuildID:       mem.GuildID,
		DiscordID:     mem.User.ID,
		Avatar:        mem.User.Avatar,
		Discriminator: mem.User.Discriminator,
		Username:      mem.User.Username,
		Roles:         database.Convert2JsonbArray(mem.Roles),
		ServerNick:    mem.Nick,
		Muted:         mem.Mute,
		Deafened:      mem.Deaf,
		Permissions:   mem.Permissions,
		JoinedAt:      mem.JoinedAt,
		IsBot:         mem.User.Bot,
		UpdatedAt:     time.Now(),
	}
	err := database.DiscordMember{}.BatchSave([]*database.DiscordMember{
		member,
	})
	if err != nil {
		log.Error(err)
	}
}

func syncGuildsMembers() {
	memGuildsLock.RLock()
	defer memGuildsLock.RUnlock()
	for _, guild := range memGuilds {
		if err := syncGuildMembers(guild.ID); err != nil {
			log.Error(err)
		}
	}
}

func syncGuildMembers(guildID string) error {
	var (
		afterUser string
		limit     = 1000
		count     int
	)
	log.Infof("Syncing guild %v members", guildID)
	for {
		members, err := session.GuildMembers(guildID, afterUser, limit)
		if err != nil {
			return errors.WrapAndReport(err, "query guild members")
		}
		count += len(members)
		var entities []*database.DiscordMember
		for _, mem := range members {
			entities = append(entities, &database.DiscordMember{
				GuildID:       guildID,
				DiscordID:     mem.User.ID,
				Avatar:        mem.User.Avatar,
				Discriminator: mem.User.Discriminator,
				Username:      mem.User.Username,
				Roles:         database.Convert2JsonbArray(mem.Roles),
				ServerNick:    mem.Nick,
				Muted:         mem.Mute,
				Deafened:      mem.Deaf,
				Permissions:   mem.Permissions,
				IsBot:         mem.User.Bot,
				RegisterAt:    *common.DecodeTimeInSnowflake(mem.User.ID),
				JoinedAt:      mem.JoinedAt,
				UpdatedAt:     time.Now(),
			})
		}
		if len(entities) > 0 {
			err := database.CommunityPostgres.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "guild_id"}, {Name: "discord_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"avatar", "discriminator", "username", "roles", "server_nick",
					"muted", "deafened", "permissions", "left_at", "updated_at", "joined_at", "register_at"}),
			}).Create(&entities).Error
			if err != nil {
				return err
			}
		}
		if len(members) < limit {
			log.Infof("Synced guild %v %v members", guildID, count)
			return nil
		}
		afterUser = members[limit-1].User.ID
	}
}

func overwriteGuildRolesScheduler(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		memGuildsLock.RLock()
		for _, guild := range memGuilds {
			roles, err := session.GuildRoles(guild.ID)
			if err != nil {
				log.Error(err)
				continue
			}
			var entities []*database.DiscordRole
			for _, role := range roles {
				entities = append(entities, &database.DiscordRole{
					GuildID:         guild.ID,
					RoleID:          role.ID,
					RoleName:        role.Name,
					Color:           role.Color,
					Position:        role.Position,
					RolePermissions: role.Permissions,
					Managed:         role.Managed,
				})
			}
			err = database.CommunityPostgres.Transaction(func(tx *gorm.DB) error {
				// 移除历史role，添加新的角色
				err := tx.Where("guild_id = ?", guild.ID).Delete(&database.DiscordRole{}).Error
				if err != nil {
					return err
				}
				// 添加新的角色
				return tx.Create(&entities).Error
			})
			if err != nil {
				log.Error(err)
			}
		}
		memGuildsLock.RUnlock()
	}
}

func overwriteGuildChannelsScheduler(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		memGuildsLock.RLock()
		for _, guild := range memGuilds {
			channels, err := session.GuildChannels(guild.ID)
			if err != nil {
				log.Error(err)
				continue
			}
			var entities []*database.DiscordChannel
			for _, channel := range channels {
				entities = append(entities, &database.DiscordChannel{
					GuildID:   guild.ID,
					ChannelID: channel.ID,
					Name:      channel.Name,
					Topic:     channel.Topic,
					Type:      database.NewChannelType(channel.Type),
				})
			}
			err = database.CommunityPostgres.Transaction(func(tx *gorm.DB) error {
				err := tx.Where("guild_id = ?", guild.ID).Delete(&database.DiscordChannel{}).Error
				if err != nil {
					return err
				}
				return tx.Create(&entities).Error
			})
			if err != nil {
				log.Error(err)
			}
		}
		memGuildsLock.RUnlock()
	}
}
