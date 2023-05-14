package database

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"moff.io/moff-social/pkg/errors"
	"time"
)

type DiscordQuizGameLotteryStatus string

const (
	DiscordQuizGameLotteryStatusNotStarted = DiscordQuizGameLotteryStatus("not_started")
	DiscordQuizGameLotteryStatusFinished   = DiscordQuizGameLotteryStatus("finished")
)

type DiscordQuizGameLotteryRewardType string

const (
	DiscordQuizGameLotteryRewardTypeArc        = "arc_token"
	DiscordQuizGameLotteryRewardTypeDragonball = "dragonball"
)

func (in DiscordQuizGameLotteryRewardType) IsValid() bool {
	switch in {
	case DiscordQuizGameLotteryRewardTypeArc:
		return true
	case DiscordQuizGameLotteryRewardTypeDragonball:
		return true
	default:
		return false
	}
}

type DiscordQuizGameLottery struct {
	ID                           int64                            `gorm:"primaryKey"`
	LotteryID                    string                           `gorm:"type:varchar(100);uniqueIndex"`
	Status                       DiscordQuizGameLotteryStatus     `gorm:"type:varchar(100)"`
	AllowedWinnerNum             int                              `gorm:"type:int"`
	RewardType                   DiscordQuizGameLotteryRewardType `gorm:"type:varchar(100)"`
	RewardAmount                 int                              `gorm:"type:int"`
	TotalQuizNum                 int                              `gorm:"type:int"`
	WinnerRequiredCorrectQuizNum int                              `gorm:"type:int"`
	Winners                      JSONBArray                       `gorm:"type:jsonb"`
	CreatedAt                    time.Time                        `gorm:"type:timestamp"`
	EndedAt                      *time.Time                       `gorm:"type:timestamp"`
	DeletedAt                    *time.Time                       `gorm:"type:timestamp"`
}

func (DiscordQuizGameLottery) SelectUnfinished() ([]*DiscordQuizGameLottery, error) {
	var entities []*DiscordQuizGameLottery
	err := CommunityPostgres.Where("status != ? AND deleted_at IS NULL",
		DiscordQuizGameLotteryStatusFinished).Find(&entities).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query unfinished lotteries")
	}
	return entities, nil
}

func (DiscordQuizGameLottery) SelectOne(lotteryID string) (*DiscordQuizGameLottery, error) {
	var entity DiscordQuizGameLottery
	err := CommunityPostgres.Where("lottery_id = ? and deleted_at IS NULL", lotteryID).First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &entity, nil
}

func (in DiscordQuizGameLottery) UpdateFinished() error {
	err := CommunityPostgres.Where("lottery_id = ?", in.LotteryID).Updates(DiscordQuizGameLottery{
		Status:                       DiscordQuizGameLotteryStatusFinished,
		Winners:                      in.Winners,
		EndedAt:                      in.EndedAt,
		TotalQuizNum:                 in.TotalQuizNum,
		WinnerRequiredCorrectQuizNum: in.WinnerRequiredCorrectQuizNum,
	}).Error
	return errors.WrapAndReport(err, "update lottery finished")
}

func (in DiscordQuizGameLottery) Save() error {
	err := CommunityPostgres.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "lottery_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"reward_type":                      in.RewardType,
			"reward_amount":                    in.RewardAmount,
			"allowed_winner_num":               in.AllowedWinnerNum,
			"total_quiz_num":                   in.TotalQuizNum,
			"winner_required_correct_quiz_num": in.WinnerRequiredCorrectQuizNum,
			"deleted_at":                       in.DeletedAt,
		}),
	}).Create(&in).Error
	return errors.WrapAndReport(err, "save lottery")
}

type DiscordQuizGameStatus string

const (
	DiscordQuizGameStatusNotStarted = DiscordQuizGameStatus("not_started")
	DiscordQuizGameStatusInProgress = DiscordQuizGameStatus("in_progress")
	DiscordQuizGameStatusFinished   = DiscordQuizGameStatus("finished")
)

func (in DiscordQuizGameStatus) Is(target DiscordQuizGameStatus) bool {
	return in == target
}

type DiscordQuizGame struct {
	ID                  int64                 `gorm:"primaryKey"`
	GameID              string                `gorm:"type:varchar(100);uniqueIndex" json:"game_id"`
	LotteryID           string                `gorm:"type:varchar(100);index" json:"lottery_id"`
	GuildID             string                `gorm:"type:varchar(100);index" json:"guild_id"`
	ChannelID           string                `gorm:"type:varchar(100);index" json:"channel_id"`
	TimeLimitSec        int                   `gorm:"type:int" json:"time_limit_sec"`
	SendQuizAt          time.Time             `gorm:"type:timestamp" json:"send_quiz_at"`
	Status              DiscordQuizGameStatus `gorm:"type:varchar(100)" json:"status"`
	QuestionDescription string                `gorm:"type:varchar(500)" json:"question_description"`
	AnswerOptions       JSONBArray            `gorm:"type:jsonb" json:"answer_options"`
	CorrectAnswerOption string                `gorm:"type:varchar(100)" json:"correct_answer_option"`
	Participants        *JSONBArray           `gorm:"type:jsonb" json:"participants"`
	Winners             *JSONBArray           `gorm:"type:jsonb" json:"winners"`
	QuestionMessageID   *string               `gorm:"type:varchar(100)" json:"question_message_id"`
	AnswerMessageID     *string               `gorm:"type:varchar(100)" json:"answer_message_id"`
	DeletedAt           *time.Time            `gorm:"type:timestamp" json:"deleted_at"`
}

func (in DiscordQuizGame) Save() error {
	err := CommunityPostgres.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "game_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"guild_id":              in.GuildID,
			"lottery_id":            in.LotteryID,
			"channel_id":            in.ChannelID,
			"time_limit_sec":        in.TimeLimitSec,
			"send_quiz_at":          in.SendQuizAt,
			"question_description":  in.QuestionDescription,
			"answer_options":        in.AnswerOptions,
			"correct_answer_option": in.CorrectAnswerOption,
			"status":                in.Status,
			"question_message_id":   in.QuestionMessageID,
			"answer_message_id":     in.AnswerMessageID,
			"participants":          in.Participants,
			"winners":               in.Winners,
			"deleted_at":            in.DeletedAt,
		}),
	}).Create(&in).Error
	return errors.WrapAndReport(err, "save lottery")
}

type LotteryGames []*DiscordQuizGame

func (in LotteryGames) StartTime() time.Time {
	start := time.Now()
	for _, game := range in {
		if game.SendQuizAt.Before(start) {
			start = game.SendQuizAt
		}
	}
	return start
}

func (DiscordQuizGame) SelectByLotteryIds(lotteryIds []string) (LotteryGames, error) {
	if len(lotteryIds) == 0 {
		return []*DiscordQuizGame{}, nil
	}
	var entities []*DiscordQuizGame
	err := CommunityPostgres.Where("lottery_id in (?)", lotteryIds).Find(&entities).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query discord games")
	}
	return entities, nil
}

func (DiscordQuizGame) SelectOne(gameID string) (*DiscordQuizGame, error) {
	var entity DiscordQuizGame
	err := CommunityPostgres.Where("game_id = ? AND deleted_at IS NULL", gameID).First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.WrapAndReport(err, "query discord quiz game")
	}
	return &entity, nil
}

func (in DiscordQuizGame) UpdateGameStarted() error {
	err := CommunityPostgres.Where("game_id = ? AND deleted_at IS NULL", in.GameID).Updates(DiscordQuizGame{
		QuestionMessageID: in.QuestionMessageID,
	}).Error
	return errors.WrapAndReport(err, "update game started")
}

func (in DiscordQuizGame) UpdateGameFinished() error {
	err := CommunityPostgres.Where("game_id = ? AND deleted_at IS NULL", in.GameID).Updates(DiscordQuizGame{
		Status:            DiscordQuizGameStatusFinished,
		AnswerMessageID:   in.AnswerMessageID,
		QuestionMessageID: in.QuestionMessageID,
		Participants:      in.Participants,
		Winners:           in.Winners,
	}).Error
	return errors.WrapAndReport(err, "update game finished")
}
