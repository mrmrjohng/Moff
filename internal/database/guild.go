package database

import (
	"github.com/bwmarrin/discordgo"
	"gorm.io/gorm/clause"
	"moff.io/moff-social/pkg/errors"
	"time"
)

type UserGuild struct {
	ID          int64  `gorm:"primaryKey"`
	UserID      string `gorm:"type:varchar(50);uniqueIndex:uni_srv"`
	GuildID     string `gorm:"type:varchar(50);uniqueIndex:uni_srv"`
	GuildName   string `gorm:"type:varchar(200)"`
	Permission  int64  `gorm:"type:int8"`
	DeletedTime int64  `gorm:"type:int8;uniqueIndex:uni_srv"`
}

func (in UserGuild) Create() error {
	err := CommunityPostgres.Clauses(clause.OnConflict{DoNothing: true}).Create(&in).Error
	return errors.WrapAndReport(err, "create user guild")
}

func (UserGuild) Delete(userID, guildID string) error {
	err := CommunityPostgres.Where("user_id = ? and guild_id = ? and deleted_time = 0",
		userID, guildID).Model(&UserGuild{}).Updates(UserGuild{
		DeletedTime: time.Now().UnixMilli(),
	}).Error
	return errors.WrapAndReport(err, "delete user guild")
}

func (UserGuild) BatchSave(guilds []*UserGuild) error {
	err := CommunityPostgres.Clauses(clause.OnConflict{DoNothing: true}).Create(&guilds).Error
	return errors.WrapAndReport(err, "create user guild")
}

type DiscordRole struct {
	ID              int64  `gorm:"primaryKey"`
	GuildID         string `gorm:"type:varchar(100);uniqueIndex:uni_role"`
	RoleID          string `gorm:"type:varchar(100);uniqueIndex:uni_role"`
	RoleName        string `gorm:"type:varchar(255)"`
	Color           int    `gorm:"type:int"`
	Position        int    `gorm:"type:int"`
	RolePermissions int64  `gorm:"type:int8"`
	Managed         bool   `gorm:"type:bool"`
}

type DiscordChannel struct {
	ID        int64       `gorm:"primaryKey"`
	GuildID   string      `gorm:"type:varchar(100);uniqueIndex:uni_chan"`
	ChannelID string      `gorm:"type:varchar(100);uniqueIndex:uni_chan"`
	Name      string      `gorm:"type:varchar(255)"`
	Topic     string      `gorm:"type:text"`
	Type      ChannelType `gorm:"type:varchar(255)"`
}

type ChannelType string

const (
	ChannelTypeGuildText          = ChannelType("guild_text")
	ChannelTypeDM                 = ChannelType("direct_message")
	ChannelTypeGuildVoice         = ChannelType("guild_voice")
	ChannelTypeGroupDM            = ChannelType("group_dm")
	ChannelTypeGuildCategory      = ChannelType("guild_category")
	ChannelTypeGuildNews          = ChannelType("guild_news")
	ChannelTypeGuildStore         = ChannelType("guild_store")
	ChannelTypeGuildNewsThread    = ChannelType("guild_news_thread")
	ChannelTypeGuildPublicThread  = ChannelType("guild_public_thread")
	ChannelTypeGuildPrivateThread = ChannelType("guild_private_thread")
	ChannelTypeGuildStageVoice    = ChannelType("guild_stage_voice")
)

func NewChannelType(tp discordgo.ChannelType) ChannelType {
	switch tp {
	case discordgo.ChannelTypeGuildText:
		return ChannelTypeGuildText
	case discordgo.ChannelTypeDM:
		return ChannelTypeDM
	case discordgo.ChannelTypeGuildVoice:
		return ChannelTypeGuildVoice
	case discordgo.ChannelTypeGroupDM:
		return ChannelTypeGroupDM
	case discordgo.ChannelTypeGuildCategory:
		return ChannelTypeGuildCategory
	case discordgo.ChannelTypeGuildNews:
		return ChannelTypeGuildNews
	case discordgo.ChannelTypeGuildStore:
		return ChannelTypeGuildStore
	case discordgo.ChannelTypeGuildNewsThread:
		return ChannelTypeGuildNewsThread
	case discordgo.ChannelTypeGuildPublicThread:
		return ChannelTypeGuildPublicThread
	case discordgo.ChannelTypeGuildPrivateThread:
		return ChannelTypeGuildPrivateThread
	case discordgo.ChannelTypeGuildStageVoice:
		return ChannelTypeGuildStageVoice
	default:
		return "unknown"
	}
}

type DiscordUserTrace struct {
	ID           int64     `gorm:"primaryKey"`
	DiscordID    string    `gorm:"type:varchar(255);index"`
	ClientIP     string    `gorm:"type:varchar(255);index"`
	UserAgent    string    `gorm:"type:varchar(500)"`
	CountryName  string    `gorm:"type:varchar(100)"`
	CountryCode2 string    `gorm:"type:varchar(100)"`
	DeviceFromUA string    `gorm:"type:varchar(100)"`
	CreatedTime  time.Time `gorm:"type:timestamp"`
}
