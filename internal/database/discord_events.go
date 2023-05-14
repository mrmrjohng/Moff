package database

import "time"

type DiscordEventType string

const (
	DiscordEventTypeGuildMemberAdd    = DiscordEventType("GuildMemberAdd")
	DiscordEventTypeGuildMemberRemove = DiscordEventType("GuildMemberRemove")
	DiscordEventTypeGuildMemberUpdate = DiscordEventType("GuildMemberUpdate")
	DiscordEventTypeGuildMemberChunk  = DiscordEventType("GuildMemberChunk")

	DiscordEventTypeMessageCreate            = DiscordEventType("MessageCreate")
	DiscordEventTypeMessageDelete            = DiscordEventType("MessageDelete")
	DiscordEventTypeMessageDeleteBulk        = DiscordEventType("MessageDeleteBulk")
	DiscordEventTypeMessageReactionAdd       = DiscordEventType("MessageReactionAdd")
	DiscordEventTypeMessageReactionRemove    = DiscordEventType("MessageReactionRemove")
	DiscordEventTypeMessageReactionRemoveAll = DiscordEventType("MessageReactionRemoveAll")
	DiscordEventTypeMessageUpdate            = DiscordEventType("MessageUpdate")

	DiscordEventTypeTypingStart      = DiscordEventType("TypingStart")
	DiscordEventTypePresenceUpdate   = DiscordEventType("PresenceUpdate")
	DiscordEventTypePresencesReplace = DiscordEventType("PresencesReplace")
	DiscordEventTypeUserUpdate       = DiscordEventType("UserUpdate")
	DiscordEventTypeVoiceStateUpdate = DiscordEventType("VoiceStateUpdate")
)

type DiscordEvents struct {
	ID        int64            `gorm:"primaryKey"`
	GuildID   string           `gorm:"type:varchar(255)"`
	EventType DiscordEventType `gorm:"type:varchar(255)"`
	Event     JSONBMap         `gorm:"type:jsonb"`
	EventTime time.Time        `gorm:"type:timestamptz"`
}

type DiscordInviteEvent struct {
	GuildID     string
	EventType   DiscordEventType
	InviterId   string
	InviterName string
	InviteeId   string
	InviteeName string
	InviteCode  string
	RawEvent    string
	EventTime   string
	TotalMember int
}

type DiscordMemberRemoveEvent struct {
	GuildID   string
	EventType DiscordEventType
	UserId    string
	UserName  string
	RawEvent  string
	EventTime string
}

type DiscordActiveEvent struct {
	GuildID     string
	EventType   DiscordEventType
	UserId      string
	UserName    string
	ChannelId   string
	ChannelName string
	RawEvent    string
	EventTime   string
}

type DiscordMessageEvent struct {
	GuildID     string
	EventType   DiscordEventType
	UserId      string
	UserName    string
	Message     string
	ChannelId   string
	ChannelName string
	RawEvent    string
	EventTime   string
}
