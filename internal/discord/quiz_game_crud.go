package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"net/http"
	"time"
)

type saveQuizGameLotteryRequest struct {
	// 开奖id
	LotteryID string `json:"lottery_id"`
	// 奖励类型
	RewardType database.DiscordQuizGameLotteryRewardType `json:"reward_type"`
	// 奖励数量
	RewardAmount int `json:"reward_amount"`
	// 获取者数量
	WinnerNum int `json:"winner_num"`

	// 总题目数
	TotalQuizNum int `json:"total_quiz_num"`
	// 胜者要求正确题目数
	WinnerRequiredCorrectQuizNum int `json:"winner_required_correct_quiz_num"`
}

func SaveQuizGameLottery(ctx *gin.Context) {
	var lottery saveQuizGameLotteryRequest
	if err := ctx.ShouldBindJSON(&lottery); err != nil {
		log.Errorf("bind quiz game lottery json:%v", err)
		ctx.String(http.StatusBadRequest, "invalid request")
		return
	}
	if lottery.WinnerNum <= 0 {
		ctx.String(http.StatusBadRequest, "invalid winner numbers")
		return
	}
	if lottery.LotteryID == "" {
		ctx.String(http.StatusBadRequest, "invalid lottery id")
		return
	}
	if !lottery.RewardType.IsValid() {
		ctx.String(http.StatusBadRequest, "invalid reward type")
		return
	}
	if lottery.RewardAmount <= 0 {
		ctx.String(http.StatusBadRequest, "invalid reward amount")
		return
	}
	if lottery.TotalQuizNum <= 0 {
		ctx.String(http.StatusBadRequest, "invalid total quiz num")
		return
	}
	if lottery.WinnerRequiredCorrectQuizNum <= 0 {
		ctx.String(http.StatusBadRequest, "invalid correct quiz num")
		return
	}
	if lottery.WinnerRequiredCorrectQuizNum > lottery.TotalQuizNum {
		ctx.String(http.StatusBadRequest, "correct quiz num must not greater than total quiz num")
		return
	}
	gameLottery := &database.DiscordQuizGameLottery{
		LotteryID:                    lottery.LotteryID,
		RewardType:                   lottery.RewardType,
		RewardAmount:                 lottery.RewardAmount,
		Status:                       database.DiscordQuizGameLotteryStatusNotStarted,
		AllowedWinnerNum:             lottery.WinnerNum,
		TotalQuizNum:                 lottery.TotalQuizNum,
		WinnerRequiredCorrectQuizNum: lottery.WinnerRequiredCorrectQuizNum,
		CreatedAt:                    time.Now(),
	}
	if err := gameLottery.Save(); err != nil {
		log.Error(err)
		ctx.String(http.StatusInternalServerError, "internal error")
		return
	}
	// 添加lottery
	NewQuizGameManager().AddLottery(gameLottery)
	ctx.String(http.StatusOK, "success")
}

type saveQuizGameRequest struct {
	database.DiscordQuizGame
	SendQuizAt int64 `json:"send_quiz_at"`
}

// curl -H "Content-Type:application/json" -X POST -d "" http://127.0.0.1:8080/discord/quiz_game

// curl -H "Content-Type:application/json" -X POST -d "" http://127.0.0.1:8080/discord/quiz_game_lottery

func SaveQuizGame(ctx *gin.Context) {
	ok, req := validateSaveQuizGameRequest(ctx)
	if !ok {
		return
	}

	// 添加游戏缓存(保证一定会持久化数据库)
	bts, err := json.Marshal(req.DiscordQuizGame)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "marshal game"))
		ctx.String(http.StatusInternalServerError, "internal error")
		return
	}
	err = cache.Redis.Set(context.TODO(), fmt.Sprintf("%v%v", quizGameInfoCacheKeyPrefix, req.GameID), string(bts), 0).Err()
	if err != nil {
		log.Error(errors.WrapAndReport(err, "cache game"))
		ctx.String(http.StatusInternalServerError, "internal error")
		return
	}

	// 数据库持久化
	if err := req.DiscordQuizGame.Save(); err != nil {
		log.Error(err)
		ctx.String(http.StatusInternalServerError, "internal error")
		return
	}
	if err := NewQuizGameManager().AddGame(&req.DiscordQuizGame); err != nil {
		log.Error(err)
		ctx.String(http.StatusInternalServerError, err.Error())
		return
	}

	ctx.String(http.StatusOK, "success")
}

func validateSaveQuizGameRequest(ctx *gin.Context) (bool, *saveQuizGameRequest) {
	var game saveQuizGameRequest
	if err := ctx.ShouldBindJSON(&game); err != nil {
		log.Errorf("bind quiz game json:%v", err)
		ctx.String(http.StatusBadRequest, "invalid request")
		return false, nil
	}
	// 检查开奖
	if game.LotteryID == "" {
		ctx.String(http.StatusBadRequest, "lottery id not present")
		return false, nil
	}
	lottery, err := database.DiscordQuizGameLottery{}.SelectOne(game.LotteryID)
	if err != nil {
		log.Error(err)
		ctx.String(http.StatusInternalServerError, "internal error")
		return false, nil
	}
	if lottery == nil {
		ctx.String(http.StatusBadRequest, "unknown lottery")
		return false, nil
	}
	if lottery.Status == database.DiscordQuizGameLotteryStatusFinished {
		ctx.String(http.StatusBadRequest, "lottery already finished")
		return false, nil
	}

	if game.GuildID == "" {
		ctx.String(http.StatusBadRequest, "guild id not present")
		return false, nil
	}
	_, err = session.Guild(game.GuildID)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query guild when save quiz game"))
		ctx.String(http.StatusBadRequest, "unknown guild")
		return false, nil
	}
	if game.ChannelID == "" {
		ctx.String(http.StatusBadRequest, "channel id not present")
		return false, nil
	}
	_, err = session.Channel(game.ChannelID)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query channel when save quiz game"))
		ctx.String(http.StatusBadRequest, "unknown channel")
		return false, nil
	}
	if game.TimeLimitSec <= 0 {
		ctx.String(http.StatusBadRequest, "invalid game time limit")
		return false, nil
	}
	if game.SendQuizAt < 0 {
		ctx.String(http.StatusBadRequest, "invalid send quiz time")
		return false, nil
	}
	game.DiscordQuizGame.SendQuizAt = time.Unix(0, game.SendQuizAt*int64(time.Millisecond))
	if game.QuestionDescription == "" {
		ctx.String(http.StatusBadRequest, "question description not present")
		return false, nil
	}
	if len(game.AnswerOptions) <= 1 || len(game.AnswerOptions) > 9 {
		ctx.String(http.StatusBadRequest, "answer option size should be [2,9]")
		return false, nil
	}
	if game.CorrectAnswerOption == "" {
		ctx.String(http.StatusBadRequest, "correct answer not present")
		return false, nil
	}
	var (
		foundAnswer  bool
		dedupAnswers = make(map[string]struct{})
	)
	for _, ans := range game.AnswerOptions {
		if ans == game.CorrectAnswerOption {
			foundAnswer = true
		}
		dedupAnswers[ans.(string)] = struct{}{}
	}
	if !foundAnswer {
		ctx.String(http.StatusBadRequest, "correct answer not found in answer options")
		return false, nil
	}
	if len(dedupAnswers) != len(game.AnswerOptions) {
		ctx.String(http.StatusBadRequest, "duplicate answer option found")
		return false, nil
	}
	if game.GameID != "" {
		one, err := database.DiscordQuizGame{}.SelectOne(game.GameID)
		if err != nil {
			log.Error(err)
			ctx.String(http.StatusInternalServerError, "internal error")
			return false, nil
		}
		if one == nil {
			ctx.String(http.StatusBadRequest, "game not found")
			return false, nil
		}
		if !one.Status.Is(database.DiscordQuizGameStatusNotStarted) {
			ctx.String(http.StatusBadRequest, "game already started")
			return false, nil
		}
	} else {
		// 检查游戏数是否超限
		games, err := database.DiscordQuizGame{}.SelectByLotteryIds([]string{game.LotteryID})
		if err != nil {
			log.Error(err)
			ctx.String(http.StatusInternalServerError, "internal error")
			return false, nil
		}
		if len(games) >= lottery.TotalQuizNum {
			ctx.String(http.StatusBadRequest, "too many games in lottery")
			return false, nil
		}
		// 可以添加游戏
		game.GameID = common.NewCutUUIDString()
	}
	game.Status = database.DiscordQuizGameStatusNotStarted
	return true, &game
}

// curl -H "Content-Type:application/json" -X DELETE -d "" http://127.0.0.1:8080/discord/quiz_game?game_id=

func DeleteQuizGame(ctx *gin.Context) {
	gameID := ctx.Query("game_id")
	if gameID == "" {
		ctx.String(http.StatusBadRequest, "game id not present")
		return
	}
	game, err := database.DiscordQuizGame{}.SelectOne(gameID)
	if err != nil {
		log.Error(err)
		ctx.String(http.StatusInternalServerError, "internal error")
		return
	}
	if game == nil {
		ctx.String(http.StatusBadRequest, "game not found")
		return
	}
	if !game.Status.Is(database.DiscordQuizGameStatusNotStarted) {
		ctx.String(http.StatusBadRequest, "game already started")
		return
	}
	now := time.Now()
	game.DeletedAt = &now
	if err := game.Save(); err != nil {
		log.Error(err)
		ctx.String(http.StatusInternalServerError, "internal error")
		return
	}
	err = NewQuizGameManager().RemoveGame(game)
	if err != nil {
		log.Error(err)
		ctx.String(http.StatusInternalServerError, ErrorUnableToTerminateGame.Error())
		return
	}
	ctx.String(http.StatusOK, "success")
}
