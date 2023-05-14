package discord

import (
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/fatih/structs"
	"github.com/go-redis/redis/v8"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/config"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strings"
	"time"
)

func checkMessageAuthorBot(msg *discordgo.Message) bool {
	if msg.Author != nil {
		return msg.Author.Bot
	}
	if msg.Member != nil {
		return msg.Member.User.Bot
	}
	return false
}

func messageReceiverHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	defer func() {
		if i := recover(); i != nil {
			log.Errorf("message receive handler:%v", i)
		}
	}()
	initializeGuildCommand(s, m)
	if !checkMessageAuthorBot(m.Message) {
		saveMessage(m)
		dumpEvent(&database.DiscordEvents{
			GuildID:   m.GuildID,
			EventType: database.DiscordEventTypeMessageCreate,
			Event:     structs.Map(m),
			EventTime: time.Now(),
		})
		pubDiscordEvent(&database.DiscordMessageEvent{
			GuildID:     m.GuildID,
			EventType:   database.DiscordEventTypeMessageCreate,
			UserId:      messageAuthor(m.Message),
			UserName:    m.Author.Username,
			Message:     m.Content,
			ChannelId:   m.ChannelID,
			ChannelName: cache.GetOrUpdateChannelInfo(s, m.ChannelID),
			RawEvent:    common.MustGetJSONString(m),
			EventTime:   m.Timestamp.UTC().Format("2006-01-02 15:04:05.000 UTC"),
		})
		err := database.DiscordMember{}.UpdateActive(m.GuildID, messageAuthor(m.Message))
		if err != nil {
			log.Error(err)
		}
	}

	if h, ok := messageCommandHandler[m.Content]; ok {
		if requirePermissionCommands[m.Content] {
			// 校验权限
			p, err := s.State.MessagePermissions(m.Message)
			if err != nil {
				log.Error(errors.WrapAndReport(err, "check user permissions from message"))
				return
			}
			if !IsAdminPermission(p) {
				log.Warnf("rejected text command %v from none admin member %v from guild %v",
					m.Content, m.Author.ID, m.GuildID)
				return
			}
		}
		if requiremoffGuildCommands[m.Content] && !ismoffGuild(m.GuildID) {
			log.Warnf("Calling command %v from none moff guild %v", m.Content, m.GuildID)
			return
		}
		if requireAuthorizedGuildCommands[m.Content] && !isAuthorizedGuild(m.GuildID) {
			log.Warnf("Calling command %v from unauthorized guild %v", m.Content, m.GuildID)
			return
		}
		h(s, m)
	}

	switch m.Type {
	case discordgo.MessageTypeDefault:
		addMemberExpMessage2SQS(newDiscordSendMessage(m))
	default:
		addMemberExpMessage2SQS(newDiscordInteraction(m))
	}
	snapshotTextChannel(m)
}

func snapshotTextChannel(m *discordgo.MessageCreate) {
	if m.Author != nil && m.Author.Bot {
		return
	}
	// 获取文字频道快照开关
	ctx := context.TODO()
	snapshotSwitchCacheKey := fmt.Sprintf("%v:%v", discordChannelSnapshotSwitchKeyPrefix, m.GuildID)
	snapshotPointStr, err := cache.Redis.HGet(ctx, snapshotSwitchCacheKey, m.ChannelID).Result()
	if errors.Is(err, redis.Nil) {
		return
	}
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query text channel snapshot switch"))
		return
	}
	snapshots := strings.Split(snapshotPointStr, "&")
	if m.Type != discordgo.MessageTypeDefault {
		return
	}
	// 保存消息
	presence := database.DiscordTextChannelPresence{
		SnapshotID: snapshots[1],
		GuildID:    m.GuildID,
		ChannelID:  m.ChannelID,
		DiscordID:  messageAuthor(m.Message),
		MessageID:  m.Message.ID,
		Text:       m.Content,
		CreatedAt:  time.Now().UnixMilli(),
	}
	if len(m.Attachments) > 0 {
		var images database.JSONBArray
		for _, attach := range m.Attachments {
			images = append(images, attach.URL)
		}
		presence.Images = &images
	}
	NewSingleWriteStorageEngine().pipeline <- func() {
		for i := 0; i < 3; i++ {
			if err := presence.Create(); err != nil {
				log.Errorf("save text channel snapshot message:%v", err)
				continue
			}
			return
		}
	}
}

var (
	messageCommandHandler = map[string]func(s *discordgo.Session, i *discordgo.MessageCreate){
		"!!invites":          showUserInvitesInfo,
		"!!levels":           showUserLevelInfo,
		"!SetUpTPRBot":       sendVerifyUserAssetsMessage,
		"!OverwriteCommands": overwriteAppCommands,
	}

	requirePermissionCommands = map[string]bool{
		"!SetUpTPRBot":       true,
		"!OverwriteCommands": true,
		"!SetUpCasino":       true,
		"!AddCoreCasino":     true,
	}

	requiremoffGuildCommands = map[string]bool{
		"!!invites":      true,
		"!!levels":       true,
		"!SetUpTPRBot":   true,
		"!SetUpCasino":   true,
		"!AddCoreCasino": true,
	}

	requireAuthorizedGuildCommands = map[string]bool{
		"!OverwriteCommands": true,
	}
)

func overwriteAppCommands(s *discordgo.Session, m *discordgo.MessageCreate) {
	var commands []*discordgo.ApplicationCommand
	if ismoffGuild(m.GuildID) {
		commands = moffCommands
	} else {
		commands = authorizedCommands
	}
	_, err := s.ApplicationCommandBulkOverwrite(config.Global.DiscordBot.AppID, m.GuildID, commands)
	if err != nil {
		log.Errorf("Cannot register commands: %v", err)
		return
	}
	log.Infof("Overwrite app commands in guild %v", m.GuildID)
}

func checkRemoveAppCommands(s *discordgo.Session, a *discordgo.GuildMemberRemove) {
	if !config.Global.DiscordBot.IsMe(a.Member.User.ID) {
		return
	}
	err1 := database.UserGuild{}.Delete(a.Member.User.ID, a.GuildID)
	if err1 != nil {
		log.Error(err1)
	}

	_, err := s.ApplicationCommandBulkOverwrite(config.Global.DiscordBot.AppID, a.GuildID, []*discordgo.ApplicationCommand{})
	if err != nil {
		log.Errorf("Cannot register commands: %v", err)
		return
	}
	log.Infof("Remove app commands from guild %v when bot leaved", a.GuildID)
}
