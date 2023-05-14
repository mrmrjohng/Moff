package database

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"moff.io/moff-social/pkg/errors"
	"time"
)

type DiscordTempRole struct {
	ID             int64  `gorm:"primaryKey"`
	GuildID        string `gorm:"type:varchar(100);index"`
	ChannelID      string `gorm:"type:varchar(100);index"`
	TempRoleID     string `gorm:"type:varchar(100);index"`
	ExpirationMins int64  `gorm:"type:int8"`
	Note           string `gorm:"type:varchar(500)"`
	CreatedAt      int64  `gorm:"type:int8"`
	DeletedAt      *int64 `gorm:"type:int8"`
}

func (in DiscordTempRole) Create() error {
	err := CommunityPostgres.Create(&in).Error
	return errors.WrapAndReport(err, "create discord temp role")
}

func (in DiscordTempRole) Save() error {
	err := CommunityPostgres.Clauses(clause.OnConflict{DoNothing: true}).Create(&in).Error
	return errors.WrapAndReport(err, "save discord temp role")
}

func (DiscordTempRole) SelectOne(guildID, channelID, roleID string) (*DiscordTempRole, error) {
	var entity DiscordTempRole
	err := CommunityPostgres.Where("guild_id = ? AND channel_id = ? AND temp_role_id = ? AND deleted_at IS NULL",
		guildID, channelID, roleID).First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.WrapAndReport(err, "query temp role")
	}
	return &entity, nil
}

func (DiscordTempRole) SelectAll() ([]*DiscordTempRole, error) {
	var entities []*DiscordTempRole
	err := CommunityPostgres.Where("deleted_at IS NULL").Find(&entities).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query expired access")
	}
	return entities, nil
}

func (in DiscordTempRole) NewAccessForMember(discordID string) *DiscordTempRoleAccess {
	return &DiscordTempRoleAccess{
		GuildID:    in.GuildID,
		TempRoleID: in.TempRoleID,
		DiscordID:  discordID,
		CreatedAt:  time.Now().UnixMilli(),
	}
}

type DiscordTempRoleAccess struct {
	ID         int64  `gorm:"primaryKey"`
	GuildID    string `gorm:"type:varchar(100);index"`
	TempRoleID string `gorm:"type:varchar(100);index"`
	DiscordID  string `gorm:"type:varchar(100)"`
	CreatedAt  int64  `gorm:"type:int8"`
	DeletedAt  *int64 `gorm:"type:int8"`
}

func (DiscordTempRoleAccess) SelectAccessExpired(roleID string, expiredCreatedAt int64) (DiscordTempRoleAccesses, error) {
	var entities []*DiscordTempRoleAccess
	err := CommunityPostgres.Where("temp_role_id = ? AND deleted_at IS NULL AND created_at <= ?",
		roleID, expiredCreatedAt).Find(&entities).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query expired access")
	}
	return entities, nil
}

type DiscordTempRoleAccesses []*DiscordTempRoleAccess

func (in DiscordTempRoleAccesses) Delete() error {
	if len(in) == 0 {
		return nil
	}
	var ids []int64
	for _, as := range in {
		ids = append(ids, as.ID)
	}
	now := time.Now().UnixMilli()
	err := CommunityPostgres.Where("id in ?", ids).Updates(DiscordTempRoleAccess{
		DeletedAt: &now,
	}).Error
	return errors.WrapAndReport(err, "batch update discord temp access")
}
