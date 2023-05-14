package database

import (
	"moff.io/moff-social/pkg/errors"
)

type DiscordTokenPermissionedRole struct {
	ID              int64  `gorm:"primaryKey"`
	GuildID         string `gorm:"type:varchar(100);index"`
	RoleID          string `gorm:"type:varchar(100);index"`
	TokenID         string `gorm:"type:varchar(100)"`
	ChainName       string `gorm:"type:varchar(100)"`
	ChainID         string `gorm:"type:varchar(100)"`
	ContractAddress string `gorm:"type:varchar(255)"`
	MinOwnAmount    int64  `gorm:"type:int"`
	RoleName        string `gorm:"->"`
}

func (in DiscordTokenPermissionedRole) Create() error {
	err := CommunityPostgres.Create(&in).Error
	return errors.WrapAndReport(err, "create tpr role")
}

func (DiscordTokenPermissionedRole) SelectByGuildIDAndChainID(guildID, chainID string) ([]*DiscordTokenPermissionedRole, error) {
	var entities []*DiscordTokenPermissionedRole
	err := CommunityPostgres.Where("guild_id = ? AND chain_Id = ?", guildID, chainID).Find(&entities).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query tpr roles")
	}
	return entities, nil
}
