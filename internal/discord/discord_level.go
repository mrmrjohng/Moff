package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/bwmarrin/discordgo"
	"github.com/fatih/structs"
	"github.com/go-redis/redis/v8"
	"math"
	"moff.io/moff-social/internal/aws"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/config"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"time"
)

func showUserLevelInfo(s *discordgo.Session, m *discordgo.MessageCreate) {
	levelContent, err := discordUserLevelContent(m.GuildID, m.Author.ID)
	if err != nil {
		log.Error(err)
		return
	}
	_, err = s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{
			{
				Title:       m.Author.Username,
				Description: levelContent,
				Author:      moffAuthor,
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "show user level"))
	}
}

func levelsCommandHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 快速响应，等待后续响应用户
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick response to level command"))
		return
	}
	defer logHandlerDuration("levels command", time.Now())
	levelContent, err := discordUserLevelContent(i.GuildID, i.Member.User.ID)
	if err != nil {
		log.Error(err)
		interactionResponseEditOnError(s, i)
		return
	}
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title:       i.Member.User.Username,
				Description: levelContent,
				Author:      moffAuthor,
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "levels response edit"))
	}
}

func discordUserLevelContent(guildID, memberID string) (string, error) {
	member, err := database.DiscordMember{}.SelectOne(guildID, memberID)
	if err != nil {
		return "", err
	}
	var content string

	if member == nil {
		content = "Your Discord Level is 0"
	} else {
		exp2LevelUp := 5*(member.Level*member.Level) + (50 * member.Level) + 100
		progress := int(math.Floor(float64(member.Exp) * 20 / float64(exp2LevelUp)))
		if progress == 0 {
			progress = 1
		}
		var (
			currExpProgress string
			leftExpProgress string
		)
		for i := 0; i < 20; i++ {
			if i < progress {
				currExpProgress += "\U0001F7E9"
				continue
			}
			leftExpProgress += "⬜️"
		}
		content = fmt.Sprintf("Your Discord Level is **%v**\nYour EXP `%v`/%v　🐲%v%v",
			member.Level, member.Exp, exp2LevelUp, currExpProgress, leftExpProgress)
	}
	return content, nil
}

const (
	discordReactionKey     = "exp:discord_reaction"
	discordInteractionKey  = "exp:discord_interaction"
	discordSendMessageKey  = "exp:discord_send_message"
	defaultExpCalcInterval = time.Second * 15
)

type memberExpAction interface {
	Action() string
	Exp() int
	Guild() string
	Member() *discordgo.Member
}

type discordSendMessage struct {
	*discordgo.MessageCreate
}

func newDiscordSendMessage(message *discordgo.MessageCreate) memberExpAction {
	return &discordSendMessage{message}
}

func (in *discordSendMessage) Action() string {
	return discordSendMessageKey
}

func (in *discordSendMessage) Exp() int {
	contentLen := common.CharCount(in.Message.Content)
	switch {
	case contentLen > 30:
		return config.Global.DiscordExpRule.OnThirtyCharMessage
	case contentLen > 20:
		return config.Global.DiscordExpRule.OnTwentyCharMessage
	case contentLen > 10:
		return config.Global.DiscordExpRule.OnTenCharMessage
	default:
		return 0
	}
}

func (in *discordSendMessage) Guild() string {
	return in.GuildID
}

func (in *discordSendMessage) Member() *discordgo.Member {
	if in.Message.Member == nil {
		in.Message.Member = &discordgo.Member{}
	}
	in.MessageCreate.Member.User = in.MessageCreate.Author
	return in.MessageCreate.Member
}

type discordInteraction struct {
	*discordgo.MessageCreate
}

func newDiscordInteraction(m *discordgo.MessageCreate) memberExpAction {
	return &discordInteraction{m}
}

func (in *discordInteraction) Action() string {
	return discordInteractionKey
}

func (in *discordInteraction) Exp() int {
	return config.Global.DiscordExpRule.OnInteraction
}

func (in *discordInteraction) Guild() string {
	return in.GuildID
}
func (in *discordInteraction) Member() *discordgo.Member {
	if in.Message.Member == nil {
		in.Message.Member = &discordgo.Member{}
	}
	in.MessageCreate.Member.User = in.MessageCreate.Author
	return in.MessageCreate.Member
}

type discordReaction struct {
	*discordgo.MessageReactionAdd
}

func newDiscordReaction(reaction *discordgo.MessageReactionAdd) memberExpAction {
	return &discordReaction{reaction}
}

func (in *discordReaction) Action() string {
	return discordReactionKey
}

func (in *discordReaction) Exp() int {
	return config.Global.DiscordExpRule.OnReaction
}

func (in *discordReaction) Guild() string {
	return in.GuildID
}
func (in *discordReaction) Member() *discordgo.Member {
	return in.MessageReactionAdd.Member
}

func addMemberExpMessage2SQS(action memberExpAction) {
	if action.Exp() == 0 {
		return
	}
	if action.Member().User.Bot {
		//log.Debugf("skip calculate exp for bot %v", action.Member().User.Username)
		return
	}

	key := fmt.Sprintf("%v:%v:%v", action.Action(), action.Guild(), action.Member().User.ID)
	ctx := context.TODO()
	ok, err := cache.Redis.SetNX(ctx, key, 1, defaultExpCalcInterval).Result()
	if err != nil {
		log.Error(errors.WrapAndReport(err, "cache discord reaction"))
		return
	}
	if !ok {
		//log.Debugf("skip add exp to guild %v member %v %v", action.Guild(), action.Member().User.ID, action.Action())
		return
	}
	exp := memberExp{
		GuildID:       action.Guild(),
		MemberID:      action.Member().User.ID,
		Avatar:        action.Member().User.Avatar,
		Discriminator: action.Member().User.Discriminator,
		Username:      action.Member().User.Username,
		Exp:           action.Exp(),
		Action:        action.Action(),
		CreatedAt:     time.Now(),
	}
	bts, err := json.Marshal(exp)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "marshal member exp"))
		return
	}
	err = aws.Client.SendMessageToSQS(ctx, config.Global.DiscordBot.MessageQueues.MemberExpQueueURL, string(bts))
	if err != nil {
		log.Errorf("send message to sqs:%v", err)
		return
	}
}

func messageReactionAddEventHandler(s *discordgo.Session, i *discordgo.MessageReactionAdd) {
	//saveMessageReaction(i)
	dumpEvent(&database.DiscordEvents{
		GuildID:   i.GuildID,
		EventType: database.DiscordEventTypeMessageReactionAdd,
		Event:     structs.Map(i),
		EventTime: time.Now(),
	})
	pubDiscordEvent(&database.DiscordMessageEvent{
		GuildID:     i.GuildID,
		EventType:   database.DiscordEventTypeMessageReactionAdd,
		UserId:      i.UserID,
		UserName:    cache.GetOrUpdateUserInfo(s, i.UserID),
		Message:     i.Emoji.Name,
		ChannelId:   i.ChannelID,
		ChannelName: cache.GetOrUpdateChannelInfo(s, i.ChannelID),
		RawEvent:    common.MustGetJSONString(i),
		EventTime:   time.Now().UTC().Format("2006-01-02 15:04:05.000 UTC"),
	})
	addMemberExpMessage2SQS(newDiscordReaction(i))
	err := database.DiscordMember{}.UpdateActive(i.GuildID, i.UserID)
	if err != nil {
		log.Error(err)
	}
}

func messageReactionRemoveEventHandler(s *discordgo.Session, i *discordgo.MessageReactionRemove) {
	dumpEvent(&database.DiscordEvents{
		GuildID:   i.GuildID,
		EventType: database.DiscordEventTypeMessageReactionRemove,
		Event:     structs.Map(i),
		EventTime: time.Now(),
	})

	pubDiscordEvent(&database.DiscordMessageEvent{
		GuildID:     i.GuildID,
		EventType:   database.DiscordEventTypeMessageReactionRemove,
		UserId:      i.UserID,
		UserName:    cache.GetOrUpdateUserInfo(s, i.UserID),
		Message:     i.Emoji.Name,
		ChannelId:   i.ChannelID,
		ChannelName: cache.GetOrUpdateChannelInfo(s, i.ChannelID),
		RawEvent:    common.MustGetJSONString(i),
		EventTime:   time.Now().UTC().Format("2006-01-02 15:04:05.000 UTC"),
	})

	err := database.DiscordMember{}.UpdateActive(i.GuildID, i.UserID)
	if err != nil {
		log.Error(err)
	}
	return
	NewSingleWriteStorageEngine().pipeline <- func() {
		err := database.PublicPostgres.Where("guild_id = ? and channel_id = ? and message_id = ? and discord_id = ? and emoji_name = ?",
			i.MessageReaction.GuildID, i.MessageReaction.ChannelID, i.MessageReaction.MessageID, i.MessageReaction.UserID,
			i.MessageReaction.Emoji.Name).Delete(&database.DiscordMessageReaction{}).Error
		if err != nil {
			log.Error(errors.WrapAndReport(err, "delete message reaction"))
		}
	}
}

func saveMessageReaction(i *discordgo.MessageReactionAdd) {
	_, err := cache.Redis.Get(context.TODO(), fmt.Sprintf("channel_reaction:%s:%s",
		i.MessageReaction.GuildID, i.MessageReaction.ChannelID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return
		}
		log.Error(errors.WrapAndReport(err, "query channel reaction"))
		return
	}
	reaction := database.DiscordMessageReaction{
		GuildID:     i.MessageReaction.GuildID,
		ChannelID:   i.MessageReaction.ChannelID,
		MessageID:   i.MessageReaction.MessageID,
		DiscordID:   i.MessageReaction.UserID,
		EmojiName:   i.MessageReaction.Emoji.Name,
		CreatedTime: time.Now().UnixMilli(),
	}
	if err := reaction.Save(); err != nil {
		log.Error(err)
	}
}

type memberExp struct {
	GuildID       string    `json:"guild_id"`
	MemberID      string    `json:"member_id"`
	Avatar        string    `json:"avatar"`
	Discriminator string    `json:"discriminator"`
	Username      string    `json:"username"`
	Exp           int       `json:"exp"`
	Action        string    `json:"action"`
	CreatedAt     time.Time `json:"created_at"`
}

const (
	discordMemberLockKeyPrefix = "discord_member_lock"
	discordMemberLockTimeout   = time.Second * 20
	calculateMemberExpTimeout  = time.Second * 25
)

func calculateDiscordMemberExp(msg *types.Message) (deleteMsg bool, err error) {
	ctx, cancel := context.WithTimeout(context.TODO(), calculateMemberExpTimeout)
	defer cancel()
	// 解析用户经验
	var exp memberExp
	if err := json.Unmarshal([]byte(*msg.Body), &exp); err != nil {
		log.Error(errors.WrapAndReport(err, "decode discord member exp"))
		return false, nil
	}
	if exp.GuildID == "" || exp.MemberID == "" || exp.Exp == 0 {
		log.Error(errors.ErrorfAndReport("invalid sqs message %v", *msg.Body))
		return true, nil
	}
	// 锁定当前用户
	lockKey := fmt.Sprintf("%v:%v:%v", discordMemberLockKeyPrefix, exp.GuildID, exp.MemberID)
	set, err := cache.Redis.SetNX(ctx, lockKey, 1, discordMemberLockTimeout).Result()
	if err != nil {
		return false, errors.WrapfAndReport(err, "lock discord member")
	}
	if !set {
		return false, nil
	}
	// 获取discord用户并刷新其等级与经验
	member, err := database.DiscordMember{}.SelectOne(exp.GuildID, exp.MemberID)
	if err != nil {
		return false, err
	}
	if member == nil {
		member = &database.DiscordMember{
			GuildID:   exp.GuildID,
			DiscordID: exp.MemberID,
			JoinedAt:  exp.CreatedAt,
		}
	}
	switch exp.Action {
	case discordReactionKey:
		member.ExpComponentReactionExp += exp.Exp
		member.ExpComponentReactionNum++
	case discordInteractionKey:
		member.ExpComponentInteractionExp += exp.Exp
		member.ExpComponentInteractionNum++
	case discordSendMessageKey:
		member.ExpComponentMessageExp += exp.Exp
		member.ExpComponentMessageNum++
	}
	member.Avatar = exp.Avatar
	member.Discriminator = exp.Discriminator
	member.Username = exp.Username
	member.Exp += exp.Exp
	member.TotalExp += exp.Exp
	member.LeftAt = nil
	member.UpdatedAt = time.Now()
	if err := member.Update(); err != nil {
		return false, err
	}
	// 尝试删除用户锁定
	if err := cache.Redis.Del(ctx, lockKey).Err(); err != nil {
		log.Error(errors.WrapAndReport(err, "delete discord member lock"))
	}

	// 刷新用户的等级stat
	//log.Debugf("add %v exp to guild %v member %v", exp.Exp, exp.GuildID, exp.MemberID)
	return true, nil
}

type userCommunityQuestTrigger string

const (
	userCommunityQuestTriggerWhitelist  = userCommunityQuestTrigger("whitelist")
	userCommunityQuestTriggerForceCheck = userCommunityQuestTrigger("force_check")
)

type userCommunityQuest struct {
	Trigger     userCommunityQuestTrigger        `json:"trigger"`
	CommunityID string                           `json:"community_id,omitempty"`
	Quest       *database.CommunityQuestTemplate `bson:"quest"`
	DiscordIDs  []string                         `json:"discord_ids"`
}

func newDiscordUserCommunityQuestForceCheck(discordID string) *userCommunityQuest {
	return &userCommunityQuest{
		Trigger:     userCommunityQuestTriggerForceCheck,
		CommunityID: discordID,
	}
}

func newDiscordUserCommunityQuestRewardFromWhitelist(quest *database.CommunityQuestTemplate, discordIDs []string) *userCommunityQuest {
	return &userCommunityQuest{
		Trigger:    userCommunityQuestTriggerWhitelist,
		DiscordIDs: discordIDs,
		Quest:      quest,
	}
}

func (s *userCommunityQuest) Marshal() string {
	bts, err := json.Marshal(s)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "marshal user community quest"))
	}
	return string(bts)
}
