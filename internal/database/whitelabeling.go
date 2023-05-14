package database

import "moff.io/moff-social/pkg/errors"

type WhiteLabelingApps struct {
	ID                              int64  `gorm:"primaryKey"`
	AppID                           string `gorm:"type:varchar(255)"`
	DiscordGuildId                  string
	AutoSnapshotTwitterSpaceEnabled bool
	CommunityDashboardURL           string
}

func (WhiteLabelingApps) SelectOne(guildID string) (*WhiteLabelingApps, error) {
	var app *WhiteLabelingApps
	sql := "SELECT * FROM admin.white_labeling_apps WHERE discord_guild_id = ?"
	err := PublicPostgres.Raw(sql, guildID).Scan(&app).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "select white-labeling apps")
	}
	return app, nil
}

func (WhiteLabelingApps) SelectApps(appIds []string) ([]*WhiteLabelingApps, error) {
	var apps []*WhiteLabelingApps
	sql := "SELECT * FROM admin.white_labeling_apps WHERE app_id in (?)"
	err := PublicPostgres.Raw(sql, appIds).Scan(&apps).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "select white-labeling apps")
	}
	return apps, nil
}
