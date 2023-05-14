package database

import (
	"gorm.io/gorm"
	"moff.io/moff-social/pkg/errors"
	"time"
)

type TwitterWebAuthorizationHeartbeats struct {
	ID              int64     `gorm:"primaryKey"`
	TwitterSpaceID  string    `gorm:"type:varchar"`
	AuthorizationID int64     `gorm:"type:varchar"`
	HeartbeatTime   time.Time `gorm:"type:timestamptz"`
}

func (in TwitterWebAuthorizationHeartbeats) Beat() error {
	sql := "INSERT INTO community.twitter_web_authorization_heartbeats (twitter_space_id,authorization_id,heartbeat_time) " +
		"VALUES (?,?,?) ON CONFLICT (twitter_space_id,authorization_id) DO UPDATE SET heartbeat_time = excluded.heartbeat_time"
	err := PublicPostgres.Exec(sql, in.TwitterSpaceID, in.AuthorizationID, time.Now()).Error
	return errors.WrapAndReport(err, "update twitter web authorization heartbeat")
}

type TwitterWebAuthorization struct {
	ID            int64      `gorm:"primaryKey"`
	Cookies       string     `gorm:"type:text"`
	CsrfToken     string     `gorm:"type:text"`
	Authorization string     `gorm:"type:text"`
	ExpiredTime   *time.Time `gorm:"type:timestamptz"`
}

const (
	MaxHeartbeatCount = 5
)

func (TwitterWebAuthorization) FindHeartbeatableAndNotExpired() ([]*TwitterWebAuthorization, error) {
	var auth []*TwitterWebAuthorization
	sql := "SELECT a.* FROM community.twitter_web_authorizations a WHERE a.expired_time is NULL AND (" +
		"SELECT count(id) < ? FROM community.twitter_web_authorization_heartbeats WHERE authorization_id=a.id AND heartbeat_time > ?)"
	err := PublicPostgres.Raw(sql, MaxHeartbeatCount, time.Now().Add(-time.Minute)).Scan(&auth).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "select twitter web authorizations")
	}
	return auth, nil
}

func (in TwitterWebAuthorization) Expire() error {
	err := PublicPostgres.Exec("UPDATE community.twitter_web_authorizations SET expired_time=? WHERE id=? AND expired_time is null",
		time.Now(), in.ID).Error
	return errors.WrapAndReport(err, "update twitter web authorization expired")
}

type TwitterSpaceSnapshotOwns struct {
	TwitterSpaceOwnerships
	TwitterSpaceSnapshots
}

func (in TwitterSpaceSnapshotOwns) Save() error {
	err := PublicPostgres.Transaction(func(tx *gorm.DB) error {
		saveOwnerSql := "INSERT INTO community.twitter_space_ownerships (discord_guild_id,starter_discord_id,twitter_space_id," +
			"linked_campaign_id,campaign_whitelist_id,snapshot_min_seconds,created_time) VALUES (?,?,?,?,?,?,?) ON CONFLICT DO NOTHING"
		err := tx.Exec(saveOwnerSql, in.DiscordGuildID, in.StarterDiscordID, in.TwitterSpaceID, in.LinkedCampaignID, in.CampaignWhitelistID,
			in.SnapshotMinSeconds, in.CreatedTime).Error
		if err != nil {
			return err
		}
		saveSnapshotSql := "INSERT INTO community.twitter_space_snapshots (space_id,space_url,scheduled_started_at,started_at," +
			"ended_at,space_title) VALUES (?,?,?,?,?,?) ON CONFLICT DO NOTHING"
		return tx.Exec(saveSnapshotSql, in.SpaceID, in.SpaceURL, in.ScheduledStartedAt, in.StartedAt, in.EndedAt,
			in.SpaceTitle).Error
	})
	return errors.WrapAndReport(err, "save twitter space snapshot owner")
}

func (TwitterSpaceSnapshotOwns) SelectOne(guildID, spaceID string) (*TwitterSpaceSnapshotOwns, error) {
	var snapshot TwitterSpaceSnapshotOwns
	sql := "SELECT * FROM community.twitter_space_ownerships so JOIN community.twitter_space_snapshots ss " +
		"ON so.twitter_space_id=ss.space_id WHERE so.discord_guild_id=? AND so.twitter_space_id = ? AND so.deleted_time = 0"
	err := PublicPostgres.Raw(sql, guildID, spaceID).Scan(&snapshot).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query twitter snapshot")
	}
	if snapshot.SpaceID == "" {
		return nil, nil
	}
	return &snapshot, nil
}

func (in TwitterSpaceSnapshotOwns) Delete() error {
	err := PublicPostgres.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UnixMilli()
		return tx.Exec("UPDATE community.twitter_space_ownerships SET deleted_time=? WHERE discord_guild_id=? AND twitter_space_id=? AND deleted_time=0",
			now, in.DiscordGuildID, in.TwitterSpaceID).Error
	})
	return errors.WrapAndReport(err, "delete twitter space")
}

func (TwitterSpaceSnapshotOwns) SelectOngoing(guildID string) ([]*TwitterSpaceSnapshotOwns, error) {
	var snapshots []*TwitterSpaceSnapshotOwns
	sql := "SELECT * FROM community.twitter_space_ownerships so JOIN community.twitter_space_snapshots ss " +
		"ON so.twitter_space_id=ss.space_id WHERE so.discord_guild_id=? AND so.deleted_time = 0 AND ss.ended_at IS NULL"
	err := PublicPostgres.Raw(sql, guildID).Scan(&snapshots).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "select ongoing twitter snapshots")
	}
	return snapshots, nil
}

func (TwitterSpaceSnapshotOwns) SelectFinished(guildID string) ([]*TwitterSpaceSnapshotOwns, error) {
	var snapshots []*TwitterSpaceSnapshotOwns
	sql := "SELECT * FROM community.twitter_space_ownerships so JOIN community.twitter_space_snapshots ss " +
		"ON so.twitter_space_id=ss.space_id WHERE so.discord_guild_id=? AND so.deleted_time = 0 AND ss.ended_at IS NOT NULL " +
		"ORDER BY ss.ended_at DESC LIMIT 10"
	err := PublicPostgres.Raw(sql, guildID).Scan(&snapshots).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "select finished twitter snapshots")
	}
	return snapshots, nil
}

type TwitterSpaceOwnerships struct {
	ID                  int64  `gorm:"primaryKey"`
	DiscordGuildID      string `gorm:"type:varchar(255)"`
	StarterDiscordID    string `gorm:"type:varchar(255)"`
	TerminatorDiscordID string `gorm:"type:varchar(255)"`
	TwitterSpaceID      string `gorm:"type:varchar(255)"`
	LinkedCampaignID    string `gorm:"type:varchar(255)"`
	CampaignWhitelistID string `gorm:"type:varchar(255)"`
	SnapshotMinSeconds  int64  `gorm:"type:int8"`
	CreatedTime         int64  `gorm:"type:int8"`
	DeletedTime         int64  `gorm:"type:int8"`
}

func (TwitterSpaceOwnerships) SelectOne(guildID, spaceID string) (*TwitterSpaceOwnerships, error) {
	var owner TwitterSpaceOwnerships
	sql := "SELECT * FROM community.twitter_space_ownerships WHERE discord_guild_id = ? AND twitter_space_id = ? AND deleted_time = 0"
	err := PublicPostgres.Raw(sql, guildID, spaceID).Scan(&owner).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "select twitter snapshot owner")
	}
	if owner.DiscordGuildID == "" {
		return nil, nil
	}
	return &owner, nil
}

func (TwitterSpaceOwnerships) SelectOwnerCount(spaceID string) (int64, error) {
	var count int64
	err := PublicPostgres.Raw("SELECT count(*) FROM community.twitter_space_ownerships WHERE twitter_space_id = ? AND deleted_time = 0",
		spaceID).Count(&count).Error
	return count, errors.WrapAndReport(err, "query ownership count")
}

func (TwitterSpaceOwnerships) SelectSpaceOwners(spaceID string) ([]*TwitterSpaceOwnerships, error) {
	var owners []*TwitterSpaceOwnerships
	sql := "SELECT * FROM community.twitter_space_ownerships WHERE twitter_space_id = ? AND deleted_time = 0"
	err := PublicPostgres.Raw(sql, spaceID).Scan(&owners).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "select twitter snapshot owners")
	}
	return owners, nil
}

type TwitterSpaceSnapshots struct {
	ID                 int64      `gorm:"primaryKey"`
	SpaceID            string     `gorm:"type:varchar(100);uniqueIndex"`
	SpaceTitle         string     `gorm:"type:text"`
	SpaceURL           string     `gorm:"type:varchar(500)"`
	ScheduledStartedAt *time.Time `gorm:"type:timestamptz"`
	StartedAt          *time.Time `gorm:"type:timestamptz"`
	EndedAt            *time.Time `gorm:"type:timestamptz"`
	TotalParticipants  int        `gorm:"type:int8"`
	ParticipantLink    string     `gorm:"type:varchar"`
}

func (s TwitterSpaceSnapshots) StartTime() *time.Time {
	if s.StartedAt != nil {
		return s.StartedAt
	}
	return s.ScheduledStartedAt
}

func (TwitterSpaceSnapshots) SelectOne(spaceID string) (*TwitterSpaceSnapshots, error) {
	var snapshot TwitterSpaceSnapshots
	sql := "SELECT * FROM community.twitter_space_snapshots WHERE space_id = ?"
	err := PublicPostgres.Raw(sql, spaceID).Scan(&snapshot).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query twitter snapshot")
	}
	if snapshot.SpaceID == "" {
		return nil, nil
	}
	return &snapshot, nil
}

func (TwitterSpaceSnapshots) SelectOngoing() ([]*TwitterSpaceSnapshots, error) {
	var snapshots []*TwitterSpaceSnapshots
	sql := "SELECT s.* FROM community.twitter_space_snapshots s WHERE s.ended_at IS NULL AND (" +
		"SELECT count(*) > 0 FROM community.twitter_space_ownerships WHERE twitter_space_id=s.space_id AND deleted_time = 0)"
	err := PublicPostgres.Raw(sql).Scan(&snapshots).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "select ongoing twitter snapshots")
	}
	return snapshots, nil
}

func (in TwitterSpaceSnapshots) Update() error {
	err := PublicPostgres.Exec("UPDATE community.twitter_space_snapshots SET started_at=?,ended_at=?,total_participants=?,participant_link=? WHERE space_id=? AND ended_at IS NULL",
		in.StartedAt, in.EndedAt, in.TotalParticipants, in.ParticipantLink, in.SpaceID).Error
	return errors.WrapAndReport(err, "update twitter snapshot")
}

type TwitterSpacePresences struct {
	ID        int64     `gorm:"primaryKey"`
	SpaceID   int64     `gorm:"type:varchar(100):"`
	TwitterID string    `gorm:"type:varchar(100)"`
	JoinedAt  time.Time `gorm:"type:timestamptz"`
	LeftAt    time.Time `gorm:"type:timestamptz"`
}
