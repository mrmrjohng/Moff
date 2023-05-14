package database

import (
	"github.com/bwmarrin/discordgo"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"time"
)

type DiscordGuildMemberInvites struct {
	ID                  int64  `gorm:"primaryKey"`
	GuildID             string `gorm:"type:varchar(100);index"`
	InviteCode          string `gorm:"type:varchar(100);index"`
	InviterID           string `gorm:"type:varchar(100);index"`
	InviteeID           string `gorm:"type:varchar(100);index"`
	InviteeJoinedAt     int64  `gorm:"type:int8"`
	InviteeRegisteredAt int64  `gorm:"type:int8"`
	InviteeLeftAt       *int64 `gorm:"type:int8"`
}

func NewDiscordGuildMemberInvites(invite *discordgo.Invite, invitee *discordgo.Member) *DiscordGuildMemberInvites {
	invites := &DiscordGuildMemberInvites{
		GuildID:         invitee.GuildID,
		InviteeID:       invitee.User.ID,
		InviteeJoinedAt: invitee.JoinedAt.UnixMilli(),
	}
	if invite != nil {
		inviter := invite.Inviter
		invites.InviteCode = invite.Code
		invites.InviterID = inviter.ID
		invites.InviteeRegisteredAt = common.DecodeTimeInSnowflake(invitee.User.ID).UnixMilli()
	}
	return invites
}

func (in DiscordGuildMemberInvites) Create() error {
	err := CommunityPostgres.Create(&in).Error
	return errors.WrapAndReport(err, "create discord member invite")
}

func (in DiscordGuildMemberInvites) UpdateInviteeLeave() error {
	now := time.Now().UnixMilli()
	err := CommunityPostgres.Where("guild_id = ? AND invitee_id = ? AND invitee_left_at IS NULL",
		in.GuildID, in.InviteeID).Updates(DiscordGuildMemberInvites{
		InviteeLeftAt: &now,
	}).Error
	return errors.WrapAndReport(err, "update member leave")
}

type DiscordInviteLeaderboard struct {
	InviterID string
	// 所有的邀请
	InviteNum int64
	// 邀请后用户离开
	Leave int64
	// 账户注册不超过指定时间(暂时按一个月)
	Newbee int64
}

func (l *DiscordInviteLeaderboard) GetValidInvitesCount() int64 {
	return l.InviteNum - l.Leave - l.Newbee
}

func (DiscordGuildMemberInvites) UserTotalInvites(guildID, memberID string) ([]*DiscordInviteLeaderboard, error) {
	var entities []*DiscordInviteLeaderboard
	err := CommunityPostgres.Raw("SELECT t.*, \n(SELECT count(*) FROM community.discord_guild_member_invites WHERE guild_id = ? AND inviter_id = t.inviter_id AND invitee_left_at IS NOT NULL) leave,\n(SELECT count(*) FROM community.discord_guild_member_invites WHERE guild_id = ? AND inviter_id = t.inviter_id AND invitee_left_at IS NULL AND invitee_registered_at > ?) newbee \nFROM (\nSELECT inviter_id, count(*) invite_num \nFROM community.discord_guild_member_invites WHERE guild_id = ? AND inviter_id = ?\nGROUP BY inviter_id\n) t",
		guildID, guildID, time.Now().Add(-time.Hour*24*30).UnixMilli(), guildID, memberID).Scan(&entities).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query user leaderboard")
	}
	return entities, nil
}

func (DiscordGuildMemberInvites) QueryTotalLeaderboard(guildID string, offset, limit int) ([]*DiscordInviteLeaderboard, error) {
	var entities []*DiscordInviteLeaderboard
	err := CommunityPostgres.Raw("SELECT t.*, \n(SELECT count(*) FROM community.discord_guild_member_invites WHERE guild_id = ? AND inviter_id = t.inviter_id AND invitee_left_at IS NOT NULL) leave,\n(SELECT count(*) FROM community.discord_guild_member_invites WHERE guild_id = ? AND inviter_id = t.inviter_id AND invitee_left_at IS NULL AND invitee_registered_at > ?) newbee \nFROM (\nSELECT inviter_id, count(*) invite_num \nFROM community.discord_guild_member_invites WHERE guild_id = ? AND inviter_id IS NOT NULL \nGROUP BY inviter_id ORDER BY invite_num DESC LIMIT ? OFFSET ?\n) t",
		guildID, guildID, time.Now().Add(-time.Hour*24*30).UnixMilli(), guildID, limit, offset).Scan(&entities).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query total leaderboard")
	}
	return entities, nil
}

type DiscordGuildInvites struct {
	ID                int64                      `gorm:"primaryKey"`
	GuildID           string                     `gorm:"type:varchar(255);index"`
	ChannelID         string                     `gorm:"type:varchar(255);index"`
	InviterID         string                     `gorm:"type:varchar(255);index"`
	InviteCode        string                     `gorm:"type:varchar(255);index"`
	CreatedAt         time.Time                  `gorm:"type:timestamptz"`
	MaxAge            int                        `gorm:"type:int8"`
	UsedCount         int                        `gorm:"type:int8"`
	MaxUseCount       int                        `gorm:"type:int8"`
	Revoked           bool                       `gorm:"type:bool"`
	Temporary         bool                       `gorm:"type:bool"`
	Unique            bool                       `gorm:"type:bool"`
	TargetUser        JSONBMap                   `gorm:"type:jsonb"`
	TargetType        discordgo.InviteTargetType `gorm:"type:int"`
	TargetApplication JSONBMap                   `gorm:"type:jsonb"`
}

type DiscordCampaignInvite struct {
	ID             int64     `gorm:"primaryKey"`
	InviteCode     string    `gorm:"type:varchar(255);uniqueIndex"`
	CampaignName   string    `gorm:"type:varchar(255)"`
	CampaignSource string    `gorm:"type:varchar(255)"`
	CreatedTime    time.Time `gorm:"type:timestamp"`
}

func (in DiscordCampaignInvite) Create() error {
	err := CommunityPostgres.Create(&in).Error
	return errors.WrapAndReport(err, "create discord invite campaign")
}

func (DiscordCampaignInvite) SelectServerCount(guildID string) (int64, error) {
	var count int64
	err := CommunityPostgres.Table("community.discord_guild_invites dgi").
		Joins("JOIN community.discord_campaign_invites dci ON dgi.invite_code=dci.invite_code").
		Where("dgi.guild_id = ?", guildID).Count(&count).Error
	if err != nil {
		return 0, errors.WrapAndReport(err, "select server count")
	}
	return count, nil
}

func (DiscordCampaignInvite) SelectPagination(guildID string, limit, offset int) ([]*DiscordCampaignInvite, error) {
	var entities []*DiscordCampaignInvite
	err := CommunityPostgres.Table("community.discord_guild_invites dgi").
		Joins("JOIN community.discord_campaign_invites dci ON dgi.invite_code=dci.invite_code").
		Where("dgi.guild_id = ?", guildID).Select("dci.*").Order("dci.created_time desc").
		Limit(limit).Offset(offset).Scan(&entities).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query campaign invites pagination")
	}
	return entities, nil
}
