package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/fatih/structs"
	"github.com/go-redis/redis/v8"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/internal/databus"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"time"
)

const (
	discordTopic = "discord_topic"
)

func saveMessage(m *discordgo.MessageCreate) {
	defer func() {
		if i := recover(); i != nil {
			log.Errorf("save message:%v", i)
		}
	}()
	now := time.Now().UnixMilli()
	postKey := fmt.Sprintf("discord_forum_post:%v:%v", m.GuildID, m.ChannelID)
	forumID, err := cache.Redis.Get(context.TODO(), postKey).Result()

	if errors.Is(err, redis.Nil) {
		// 普通消息
		message := database.DiscordMessages{
			GuildID:     m.GuildID,
			ChannelID:   m.ChannelID,
			AuthorID:    messageAuthor(m.Message),
			MessageID:   m.ID,
			Content:     m.Content,
			ContentLen:  common.CharCount(m.Content),
			CreatedTime: now,
			UpdatedTime: now,
		}
		if len(m.Attachments) > 0 {
			var images database.JSONBArray
			for _, attach := range m.Attachments {
				images = append(images, attach.URL)
			}
			message.Images = images
		}
		if err := message.Create(); err != nil {
			log.Error(err)
		}
		return
	}
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query forum post"))
		return
	}

	// 论坛消息
	post := database.DiscordForums{
		GuildID:     m.GuildID,
		ForumID:     forumID,
		PostID:      m.ChannelID,
		Action:      database.DiscordForumActionReplyPost,
		AuthorID:    messageAuthor(m.Message),
		MessageID:   m.ID,
		Content:     m.Content,
		ContentLen:  common.CharCount(m.Content),
		CreatedTime: now,
		UpdatedTime: now,
	}
	if len(m.Attachments) > 0 {
		var images database.JSONBArray
		for _, attach := range m.Attachments {
			images = append(images, attach.URL)
		}
		post.Images = images
	}
	if err := post.Create(); err != nil {
		log.Error(err)
	}
}

func dumpEvent(event *database.DiscordEvents) {
	return
	NewSingleWriteStorageEngine().Enqueue(func() {
		err := database.CommunityPostgres.Create(event).Error
		if err != nil {
			log.Error(errors.WrapAndReport(err, "save discord event"))
		}
	})
}

func pubDiscordEvent(event interface{}) {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "serialize discord events"))
		return
	}
	err = databus.GetDataBus().PublishRaw(discordTopic, payload)
	if err != nil {
		log.Error(err)
	}
}

func GuildMembersChunkEventHandler(s *discordgo.Session, m *discordgo.GuildMembersChunk) {
	dumpEvent(&database.DiscordEvents{
		GuildID:   m.GuildID,
		EventType: database.DiscordEventTypeGuildMemberChunk,
		Event:     structs.Map(m),
		EventTime: time.Now(),
	})
}

func MessageDeleteEventHandler(s *discordgo.Session, m *discordgo.MessageDelete) {
	dumpEvent(&database.DiscordEvents{
		GuildID:   m.GuildID,
		EventType: database.DiscordEventTypeMessageDelete,
		Event:     structs.Map(m),
		EventTime: time.Now(),
	})

	now := time.Now().UnixMilli()
	postKey := fmt.Sprintf("discord_forum_post:%v:%v", m.GuildID, m.ChannelID)
	forumID, err := cache.Redis.Get(context.TODO(), postKey).Result()
	if errors.Is(err, redis.Nil) {
		// 普通消息
		message := database.DiscordMessages{
			GuildID:     m.GuildID,
			ChannelID:   m.ChannelID,
			MessageID:   m.ID,
			UpdatedTime: now,
			DeletedTime: &now,
		}
		if err := message.Delete(); err != nil {
			log.Error(err)
		}
		return
	}
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query forum post"))
		return
	}

	// 论坛消息
	post := database.DiscordForums{
		GuildID:     m.GuildID,
		ForumID:     forumID,
		PostID:      m.ChannelID,
		MessageID:   m.ID,
		UpdatedTime: now,
		DeletedTime: &now,
	}
	if err := post.DeleteMessage(); err != nil {
		log.Error(err)
	}
}

func MessageDeleteBulkEventHandler(s *discordgo.Session, m *discordgo.MessageDeleteBulk) {
	dumpEvent(&database.DiscordEvents{
		GuildID:   m.GuildID,
		EventType: database.DiscordEventTypeMessageDeleteBulk,
		Event:     structs.Map(m),
		EventTime: time.Now(),
	})
}

func MessageReactionRemoveAllEventHandler(s *discordgo.Session, m *discordgo.MessageReactionRemoveAll) {
	dumpEvent(&database.DiscordEvents{
		GuildID:   m.GuildID,
		EventType: database.DiscordEventTypeMessageReactionRemoveAll,
		Event:     structs.Map(m),
		EventTime: time.Now(),
	})
}

func MessageUpdateEventHandler(s *discordgo.Session, m *discordgo.MessageUpdate) {
	dumpEvent(&database.DiscordEvents{
		GuildID:   m.GuildID,
		EventType: database.DiscordEventTypeMessageUpdate,
		Event:     structs.Map(m),
		EventTime: time.Now(),
	})

	postKey := fmt.Sprintf("discord_forum_post:%v:%v", m.GuildID, m.ChannelID)
	forumID, err := cache.Redis.Get(context.TODO(), postKey).Result()
	if errors.Is(err, redis.Nil) {
		// 普通消息
		message := database.DiscordMessages{
			GuildID:     m.GuildID,
			ChannelID:   m.ChannelID,
			AuthorID:    messageAuthor(m.Message),
			MessageID:   m.ID,
			Content:     m.Content,
			ContentLen:  common.CharCount(m.Content),
			UpdatedTime: time.Now().UnixMilli(),
		}
		if len(m.Attachments) > 0 {
			var images database.JSONBArray
			for _, attach := range m.Attachments {
				images = append(images, attach.URL)
			}
			message.Images = images
		}
		if err := message.UpdateMessage(); err != nil {
			log.Error(err)
		}
		return
	}
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query forum post"))
		return
	}

	// 论坛消息
	post := database.DiscordForums{
		GuildID:     m.GuildID,
		ForumID:     forumID,
		PostID:      m.ChannelID,
		Action:      database.DiscordForumActionReplyPost,
		AuthorID:    messageAuthor(m.Message),
		MessageID:   m.ID,
		Content:     m.Content,
		ContentLen:  common.CharCount(m.Content),
		UpdatedTime: time.Now().UnixMilli(),
	}
	if len(m.Attachments) > 0 {
		var images database.JSONBArray
		for _, attach := range m.Attachments {
			images = append(images, attach.URL)
		}
		post.Images = images
	}
	if err := post.UpdateMessage(); err != nil {
		log.Error(err)
	}
}

func messageAuthor(m *discordgo.Message) string {
	if m.Author != nil {
		return m.Author.ID
	}
	if m.Member != nil && m.Member.User != nil {
		return m.Member.User.ID
	}
	return ""
}

func TypingStartEventHandler(s *discordgo.Session, m *discordgo.TypingStart) {
	dumpEvent(&database.DiscordEvents{
		GuildID:   m.GuildID,
		EventType: database.DiscordEventTypeTypingStart,
		Event:     structs.Map(m),
		EventTime: time.Now(),
	})
	pubDiscordEvent(&database.DiscordActiveEvent{
		GuildID:     m.GuildID,
		EventType:   database.DiscordEventTypeTypingStart,
		UserId:      m.UserID,
		UserName:    cache.GetOrUpdateUserInfo(s, m.UserID),
		ChannelId:   m.ChannelID,
		ChannelName: cache.GetOrUpdateChannelInfo(s, m.ChannelID),
		RawEvent:    common.MustGetJSONString(m),
		EventTime:   time.Unix(int64(m.Timestamp), 0).UTC().Format("2006-01-02 15:04:05.000 UTC"),
	})
}

func PresenceUpdateEventHandler(s *discordgo.Session, m *discordgo.PresenceUpdate) {
	dumpEvent(&database.DiscordEvents{
		GuildID:   m.GuildID,
		EventType: database.DiscordEventTypePresenceUpdate,
		Event:     structs.Map(m),
		EventTime: time.Now(),
	})
	// only update it when it's online
	if m.Status == "online" {
		pubDiscordEvent(&database.DiscordActiveEvent{
			GuildID:   m.GuildID,
			EventType: database.DiscordEventTypePresenceUpdate,
			UserId:    m.User.ID,
			UserName:  m.User.Username,
			RawEvent:  common.MustGetJSONString(m),
			EventTime: time.Now().UTC().Format("2006-01-02 15:04:05.000 UTC"),
		})
	}
}

func PresencesReplaceEventHandler(s *discordgo.Session, m *discordgo.PresencesReplace) {
	dumpEvent(&database.DiscordEvents{
		EventType: database.DiscordEventTypePresencesReplace,
		Event:     structs.Map(m),
		EventTime: time.Now(),
	})
}

func UserUpdateEventHandler(s *discordgo.Session, m *discordgo.UserUpdate) {
	dumpEvent(&database.DiscordEvents{
		EventType: database.DiscordEventTypeUserUpdate,
		Event:     structs.Map(m),
		EventTime: time.Now(),
	})
}

func ThreadCreateEventHandler(s *discordgo.Session, m *discordgo.ThreadCreate) {
	if !isForumChannel(m.GuildID, m.ParentID) {
		return
	}
	// 缓存帖子
	postKey := fmt.Sprintf("discord_forum_post:%v:%v", m.GuildID, m.ID)
	if err := cache.Redis.Set(context.TODO(), postKey, m.ParentID, 0).Err(); err != nil {
		log.Error(errors.WrapAndReport(err, "cache post"))
	}
	// 保存论坛帖子
	post := database.DiscordForums{
		GuildID:     m.GuildID,
		ForumID:     m.ParentID,
		PostID:      m.ID,
		AuthorID:    m.OwnerID,
		Action:      database.DiscordForumActionPubPost,
		CreatedTime: time.Now().UnixMilli(),
	}
	if err := post.Create(); err != nil {
		log.Error(err)
	}
}

func ChannelCreateEventHandler(s *discordgo.Session, m *discordgo.ChannelCreate) {
	if m.Type == discordgo.ChannelTypeGuildForum {
		guildChannelRW.Lock()
		defer guildChannelRW.Unlock()

		channels := guildChannels[m.GuildID]
		if channels == nil {
			channels = make(map[string]*discordgo.Channel)
		}
		channels[m.ID] = m.Channel
		guildChannels[m.GuildID] = channels
	}
}

func isForumChannel(guildID, channelID string) bool {
	if channelID == "" {
		return false
	}
	guildChannelRW.RLock()
	defer guildChannelRW.RUnlock()
	channels := guildChannels[guildID]
	if channels == nil {
		return false
	}
	parent := channels[channelID]
	if parent == nil {
		return false
	}
	return parent.Type == discordgo.ChannelTypeGuildForum
}
