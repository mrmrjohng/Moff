package database

type CommunityQuestTemplateRequirementsType string

const (
	CommunityQuestTemplateRequirementsTypeWhitelist = CommunityQuestTemplateRequirementsType("whitelist")
	CommunityQuestTemplateRequirementsTypeStats     = CommunityQuestTemplateRequirementsType("stats")
	CommunityQuestTemplateRequirementsTypeBadges    = CommunityQuestTemplateRequirementsType("badges")
)

type CommunityQuestTemplate struct {
	QuestID                string
	QuestName              string
	Sort                   int
	Dragonball             int
	ClaimableDurationHours int
	QuestDescription       string
	StartTime              *int64
	EndTime                *int64
	Requirements           JSONBMap
	RequirementsType       CommunityQuestTemplateRequirementsType
}

type RequiredQuizGameLotteryWinners struct {
	LotteryID string `bson:"lottery_id"`
}

type CommunityQuestWhitelist struct {
	WhitelistID   string
	WhitelistName string
}

type CommunityQuestWhitelistUserIdentityType string

const (
	CommunityQuestWhitelistUserIdentityTypeUserIds     = CommunityQuestWhitelistUserIdentityType("user_ids")
	CommunityQuestWhitelistUserIdentityTypeWalletAddrs = CommunityQuestWhitelistUserIdentityType("wallet_addrs")
	CommunityQuestWhitelistUserIdentityTypeDiscordIds  = CommunityQuestWhitelistUserIdentityType("discord_ids")
	CommunityQuestWhitelistUserIdentityTypeTwitterIds  = CommunityQuestWhitelistUserIdentityType("twitter_ids")
)

type CommunityQuestWhitelistUser struct {
	WhitelistID  string
	IdentityType CommunityQuestWhitelistUserIdentityType
	Identity     string
}
