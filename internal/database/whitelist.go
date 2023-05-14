package database

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"moff.io/moff-social/pkg/errors"
)

type WhitelistEntityType string

const (
	WhitelistEntityTypeTwitterID = WhitelistEntityType("twitter_ids")
	WhitelistEntityTypeDiscordID = WhitelistEntityType("discord_ids")
)

type Whitelist struct {
	WhitelistID string
	EntityType  WhitelistEntityType
	EntityID    string
}

func (Whitelist) WriteTwitterIds(tx *gorm.DB, whitelistID string, ids []string) error {
	var wl []*Whitelist
	for _, id := range ids {
		wl = append(wl, &Whitelist{
			WhitelistID: whitelistID,
			EntityType:  WhitelistEntityTypeTwitterID,
			EntityID:    id,
		})
	}
	err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&wl).Error
	return errors.WrapAndReport(err, "write twitter id to whitelist")
}

func (Whitelist) WriteDiscordIds(tx *gorm.DB, whitelistID string, ids []string) error {
	var wl []*Whitelist
	for _, id := range ids {
		wl = append(wl, &Whitelist{
			WhitelistID: whitelistID,
			EntityType:  WhitelistEntityTypeDiscordID,
			EntityID:    id,
		})
	}
	err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&wl).Error
	return errors.WrapAndReport(err, "write twitter id to whitelist")
}
