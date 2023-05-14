package database

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"moff.io/moff-social/pkg/errors"
	"time"
)

type DiscordMember struct {
	ID                         int64      `gorm:"primaryKey"`
	GuildID                    string     `gorm:"type:varchar(100);uniqueIndex:uni_mem"`
	DiscordID                  string     `gorm:"type:varchar(100);uniqueIndex:uni_mem"`
	Avatar                     string     `gorm:"type:varchar(500)"`
	Discriminator              string     `gorm:"type:varchar(200)"`
	Username                   string     `gorm:"type:varchar(200)"`
	Level                      int        `gorm:"type:int"`
	TotalExp                   int        `gorm:"type:int"`
	Exp                        int        `gorm:"type:int"`
	ExpComponentMessageNum     int        `gorm:"type:int"`
	ExpComponentMessageExp     int        `gorm:"type:int"`
	ExpComponentReactionNum    int        `gorm:"type:int"`
	ExpComponentReactionExp    int        `gorm:"type:int"`
	ExpComponentInteractionNum int        `gorm:"type:int"`
	ExpComponentInteractionExp int        `gorm:"type:int"`
	JoinedAt                   time.Time  `gorm:"type:timestamp"`
	UpdatedAt                  time.Time  `gorm:"type:timestamp"`
	NotificationEnabled        bool       `gorm:"type:bool;default:true"`
	LeftAt                     *time.Time `gorm:"type:timestamp"`
	RegisterAt                 time.Time  `gorm:"type:timestamp"`
	LastActiveAt               time.Time  `gorm:"type:timestamp"`
	Roles                      JSONBArray `gorm:"type:jsonb"`
	ServerNick                 string     `gorm:"type:varchar(200)"`
	Muted                      bool       `gorm:"type:bool"`
	Deafened                   bool       `gorm:"type:bool"`
	Permissions                int64      `gorm:"type:int8"`
	IsBot                      bool       `gorm:"type:bool"`
}

func (DiscordMember) DisableNotification(guildID, discordID string) error {
	err := CommunityPostgres.Where("guild_id = ? AND discord_id = ?",
		guildID, discordID).Updates(DiscordMember{
		NotificationEnabled: false,
		UpdatedAt:           time.Now(),
	}).Error
	return errors.WrapAndReport(err, "update user discord notification disabled")
}

func (DiscordMember) EnableNotification(guildID, discordID string) error {
	err := CommunityPostgres.Where("guild_id = ? AND discord_id = ?",
		guildID, discordID).Updates(DiscordMember{
		NotificationEnabled: true,
		UpdatedAt:           time.Now(),
	}).Error
	return errors.WrapAndReport(err, "update user discord notification enabled")
}

func (DiscordMember) UpdateLeave(guildID, memberID string) error {
	now := time.Now()
	err := CommunityPostgres.Where("guild_id = ? AND discord_id = ? AND left_at IS NULL",
		guildID, memberID).Updates(DiscordMember{
		LeftAt:    &now,
		UpdatedAt: now,
	}).Error
	return errors.WrapAndReport(err, "update discord member left")
}

func (DiscordMember) UpdateActive(guildID, memberID string) error {
	now := time.Now()
	err := CommunityPostgres.Where("guild_id = ? AND discord_id = ?",
		guildID, memberID).Updates(DiscordMember{
		LeftAt:       nil,
		UpdatedAt:    now,
		LastActiveAt: now,
	}).Error
	return errors.WrapAndReport(err, "update discord member active")
}

func (in DiscordMember) BatchSave(members []*DiscordMember) error {
	err := CommunityPostgres.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "guild_id"}, {Name: "discord_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"avatar", "discriminator", "username",
			"roles", "server_nick", "muted", "deafened", "permissions", "left_at", "updated_at", "joined_at"}),
	}).Create(&members).Error
	return errors.WrapAndReport(err, "batch save data points")
}

func (in DiscordMember) NewJoined() error {
	err := CommunityPostgres.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "guild_id"}, {Name: "discord_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"avatar":        in.Avatar,
			"discriminator": in.Discriminator,
			"username":      in.Username,
			"joined_at":     in.JoinedAt,
			"updated_at":    in.UpdatedAt,
			"left_at":       nil,
		}),
	}).Create(&in).Error
	return errors.WrapAndReport(err, "upsert new joined discord member")
}

func (DiscordMember) SelectOne(guildID, memberID string) (*DiscordMember, error) {
	var entity DiscordMember
	err := CommunityPostgres.Where("guild_id = ? AND discord_id = ?", guildID, memberID).First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.WrapAndReport(err, "query discord member")
	}
	return &entity, nil
}

func (in DiscordMember) Update() error {
	err := CommunityPostgres.Where("guild_id = ? AND discord_id = ?",
		in.GuildID, in.DiscordID).Updates(in).Error
	return errors.WrapAndReport(err, "update discord member")
}
