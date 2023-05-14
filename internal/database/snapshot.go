package database

import (
	"github.com/bwmarrin/discordgo"
	"moff.io/moff-social/pkg/errors"
	"time"
)

type TwitterSpaceBackups struct {
	ID          int64     `gorm:"primaryKey"`
	SpaceID     string    `gorm:"type:varchar(100)"`
	Response    string    `gorm:"type:text"`
	CreatedTime time.Time `gorm:"type:timestamptz"`
}

func (in TwitterSpaceBackups) Create() error {
	err := CommunityPostgres.Create(&in).Error
	return errors.WrapAndReport(err, "create twitter space backups")
}

func (TwitterSpaceBackups) DeleteBefore(createdTime time.Time) error {
	err := CommunityPostgres.Where("created_time < ?", createdTime).Delete(TwitterSpaceBackups{}).Error
	return errors.WrapAndReport(err, "delete twitter space backups")
}

type DiscordSnapshot struct {
	ID                   int64               `gorm:"primaryKey"`
	SnapshotID           string              `gorm:"type:varchar(100);uniqueIndex"`
	GuildID              string              `gorm:"type:varchar(100);index"`
	ChannelID            string              `gorm:"type:varchar(100);index"`
	Type                 DiscordSnapshotType `gorm:"type:varchar(100)"`
	SnapshotSeconds      *int64              `gorm:"type:int"`
	MinimumWords         *int64              `gorm:"type:int"`
	TotalParticipantsNum *int                `gorm:"type:int"`
	TotalMessageNum      *int                `gorm:"type:int"`
	ValidParticipantsNum *int                `gorm:"type:int"`
	Whitelist            JSONBArray          `gorm:"type:jsonb"`
	SheetURL             *string             `gorm:"type:varchar(200)"`

	CreatedAt    *int64    `gorm:"type:int8"`
	CreatedBy    *string   `gorm:"type:varchar(100)"`
	FinishedAt   *int64    `gorm:"type:int8"`
	FinishedBy   *string   `gorm:"type:varchar(100)"`
	CampaignID   *string   `gorm:"type:varchar(100)"`
	CampaignName *string   `gorm:"type:varchar(200)"`
	UpdatedAt    time.Time `gorm:"type:timestamp"`
}

func (in DiscordSnapshot) UpdateFinished() error {
	err := CommunityPostgres.Where("snapshot_id = ?", in.SnapshotID).Updates(DiscordSnapshot{
		FinishedAt:           in.FinishedAt,
		FinishedBy:           in.FinishedBy,
		SnapshotSeconds:      in.SnapshotSeconds,
		MinimumWords:         in.MinimumWords,
		TotalParticipantsNum: in.TotalParticipantsNum,
		TotalMessageNum:      in.TotalMessageNum,
		ValidParticipantsNum: in.ValidParticipantsNum,
		Whitelist:            in.Whitelist,
		SheetURL:             in.SheetURL,
		CampaignID:           in.CampaignID,
		CampaignName:         in.CampaignName,
		UpdatedAt:            time.Now(),
	}).Error
	return errors.WrapAndReport(err, "update snapshot finished")
}

func (DiscordSnapshot) SelectLatest(top int, guildID string) ([]*DiscordSnapshot, error) {
	var entities []*DiscordSnapshot
	err := CommunityPostgres.Where("guild_id = ?", guildID).
		Order("created_at desc").Limit(top).Find(&entities).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query snapshot")
	}
	return entities, nil
}

func (DiscordSnapshot) SelectOne(snapshotID string) (*DiscordSnapshot, error) {
	var entity DiscordSnapshot
	err := CommunityPostgres.Where("snapshot_id = ?", snapshotID).First(&entity).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query snapshot")
	}
	return &entity, nil
}

func (in DiscordSnapshot) GoogleSheetComponent() *[]discordgo.MessageComponent {
	if in.SheetURL == nil || *in.SheetURL == "" {
		return nil
	}
	url := *in.SheetURL
	return &[]discordgo.MessageComponent{
		&discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				&discordgo.Button{
					Disabled: in.SheetURL == nil,
					Style:    discordgo.LinkButton,
					Label:    "Click to see full snapshot",
					URL:      url,
					Emoji: discordgo.ComponentEmoji{
						Name: "ℹ️",
					},
				},
			},
		},
	}
}

func (in DiscordSnapshot) Create() error {
	err := CommunityPostgres.Create(&in).Error
	return errors.WrapAndReport(err, "create discord snapshot")
}

type DiscordSnapshotType string

const (
	DiscordSnapshotTypeVoice = DiscordSnapshotType("voice_channel")
	DiscordSnapshotTypeText  = DiscordSnapshotType("text_channel")
)

type DiscordTextChannelPresence struct {
	ID         int64       `gorm:"primaryKey"`
	SnapshotID string      `gorm:"type:varchar(100);index"`
	GuildID    string      `gorm:"type:varchar(100);index"`
	ChannelID  string      `gorm:"type:varchar(100);index"`
	DiscordID  string      `gorm:"type:varchar(100)"`
	MessageID  string      `gorm:"type:varchar(100);uniqueIndex"`
	Text       string      `gorm:"type:text"`
	Images     *JSONBArray `gorm:"type:jsonb"`
	CreatedAt  int64       `gorm:"type:int8"`
	DeletedAt  *int64      `gorm:"type:int8"`
}

type SnapshotPresence struct {
	DiscordID string
	Messages  []*DiscordTextChannelPresence
}

func (DiscordTextChannelPresence) SelectSnapshotPresences(snapshotID string) ([]*SnapshotPresence, error) {
	var (
		offset    = 0
		batch     = 5000
		presences = make(map[string][]*DiscordTextChannelPresence)
	)
	for {
		var entities []*DiscordTextChannelPresence
		err := CommunityPostgres.Where("snapshot_id = ?", snapshotID).Select("discord_id,text,images,created_at").
			Order("id asc").Limit(batch).Offset(offset).Find(&entities).Error
		if err != nil {
			return nil, errors.WrapAndReport(err, "query text channel presence")
		}
		offset += len(entities)
		for _, en := range entities {
			presences[en.DiscordID] = append(presences[en.DiscordID], en)
		}
		if len(entities) < batch {
			break
		}
	}
	var results []*SnapshotPresence
	for k, v := range presences {
		results = append(results, &SnapshotPresence{
			DiscordID: k,
			Messages:  v,
		})
	}
	return results, nil
}

func (in DiscordTextChannelPresence) Create() error {
	err := CommunityPostgres.Create(&in).Error
	return errors.WrapAndReport(err, "create text channel presence")
}

type DiscordTextChannelSnapshotParticipant struct {
	TotalMessage int `bson:"total_message"`
	TotalMember  int `bson:"total_member"`
}

func (DiscordTextChannelPresence) CountSnapshotParticipant(snapshotID string) (*DiscordTextChannelSnapshotParticipant, error) {
	var entity DiscordTextChannelSnapshotParticipant
	totalMsg := CommunityPostgres.Model(&DiscordTextChannelPresence{}).Where("snapshot_id = ?",
		snapshotID).Select("count(*) as total_message")
	totalMember := CommunityPostgres.Model(&DiscordTextChannelPresence{}).Where("snapshot_id = ?",
		snapshotID).Select("count(distinct(discord_id)) as total_member")
	err := CommunityPostgres.Model(&DiscordTextChannelPresence{}).Select("(?),(?)", totalMsg, totalMember).
		Limit(1).Scan(&entity).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "count participant")
	}
	return &entity, nil
}

// DiscordVoiceChannelPresence 用户每次只能在一个语音房间，故一定唯一
type DiscordVoiceChannelPresence struct {
	ID        int64  `gorm:"primaryKey"`
	GuildID   string `gorm:"type:varchar(100);index"`
	ChannelID string `gorm:"type:varchar(100);index"`
	DiscordID string `gorm:"type:varchar(100);index"`
	JoinedAt  int64  `bson:"joined_at"`
	LeftAt    *int64 `bson:"left_at"`
}

func (in DiscordVoiceChannelPresence) Join() error {
	err := CommunityPostgres.Create(&in).Error
	return errors.WrapAndReport(err, "discord voice channel presence join")
}

func (in DiscordVoiceChannelPresence) Leave() error {
	now := time.Now().UnixMilli()
	err := CommunityPostgres.Where("guild_id = ? AND discord_id = ? AND left_at IS NULL",
		in.GuildID, in.DiscordID).Updates(DiscordVoiceChannelPresence{
		LeftAt: &now,
	}).Error
	return errors.WrapAndReport(err, "discord voice channel presence left")
}
