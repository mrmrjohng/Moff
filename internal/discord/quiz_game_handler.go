package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/go-redis/redis/v8"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strings"
	"time"
)

func quizGameInteractionChooseAnswer(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer logHandlerDuration("choose quiz game answer", time.Now())
	customID := i.MessageComponentData().CustomID
	optionid := i.MessageComponentData().Values[0]
	gameID := strings.TrimPrefix(customID, discordQuizGameCustomIDPrefix)
	game := NewQuizGameManager().GetGame(gameID)
	if game == nil {
		log.Error(errors.ErrorfAndReport("Game not found from game id %v when participate", gameID))
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "`Sorry you missed the bullseye this time! Come next time!` \U0001FAE0",
			},
		})
		if err != nil {
			log.Error(errors.WrapfAndReport(err, "Send game not found from game id %v", gameID))
		}
		return
	}

	participated, err := game.participate(i.Member.User.ID, optionid)
	if err != nil {
		log.Error(err)
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags: discordgo.MessageFlagsEphemeral,
				Embeds: []*discordgo.MessageEmbed{
					{
						Description: "Unknown error",
						Author:      moffAuthor,
					},
				},
			},
		})
		if err != nil {
			log.Error(errors.WrapfAndReport(err, "Send bot goes wrong when participate game %v", gameID))
		}
		return
	}
	// TODO 此处参与失败，存在问题，需要定位
	var content string
	if participated {
		content = "Your answer is: `" + game.answerOptionFromOptionID(optionid) + "`."
	} else {
		content = "`Sorry you missed the bullseye this time! Come next time!` \U0001FAE0"
	}
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: content,
		},
	})
	if err != nil {
		log.Error(errors.WrapfAndReport(err, "Send silent interaction when participate game %v", gameID))
	}
}

func quizGameInteractionCheckResult(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer logHandlerDuration("check quiz game result", time.Now())
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick respond to check quiz game result"))
		return
	}

	customID := i.MessageComponentData().CustomID
	gameID := strings.TrimPrefix(customID, discordQuizGameCheckResultCustomIDPrefix)

	// 查找游戏
	result, err := cache.Redis.Get(context.TODO(), fmt.Sprintf("%v%v", quizGameInfoCacheKeyPrefix, gameID)).Result()
	var game quizGame
	if result != "" {
		if err := json.Unmarshal([]byte(result), &game); err != nil {
			log.Error(errors.WrapAndReport(err, "unmarshal quiz game"))
			if err := respondBotGoesWrong(s, i); err != nil {
				log.Error(errors.WrapfAndReport(err, "Send bot goes wrong when check game %v participation", gameID))
			}
			return
		}
	}
	if errors.Is(err, redis.Nil) {
		// db查找
		game.DiscordQuizGame, err = database.DiscordQuizGame{}.SelectOne(gameID)
		if game.DiscordQuizGame != nil {
			bts, _ := json.Marshal(game)
			err = cache.Redis.Set(context.TODO(), fmt.Sprintf("%v%v", quizGameInfoCacheKeyPrefix, gameID), string(bts), 0).Err()
		}
	}
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query quiz game from cache"))
		if err := respondBotGoesWrong(s, i); err != nil {
			log.Error(errors.WrapfAndReport(err, "Send bot goes wrong when check game %v participation", gameID))
		}
		return
	}

	participationInfo, err := game.participationInfo(context.TODO(), i.Member.User.ID)
	if err != nil {
		log.Error(err)
		if err := respondBotGoesWrong(s, i); err != nil {
			log.Error(errors.WrapfAndReport(err, "Send bot goes wrong when check game %v participation", gameID))
		}
		return
	}
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: participationInfo.ReplyContent(),
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "check quiz game result response"))
	}
}

func quizGameLotteryInteractionCheckResult(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer logHandlerDuration("check lottery result", time.Now())
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick respond to check quiz game result"))
		return
	}
	customID := i.MessageComponentData().CustomID
	lotteryID := strings.TrimPrefix(customID, discordQuizGameLotteryCheckResultCustomIDPrefix)

	_, err = cache.Redis.HGet(context.TODO(), fmt.Sprintf("%v%v", quizGameLotteryWinnersCacheKeyPrefix, lotteryID), i.Member.User.ID).Result()
	if errors.Is(err, redis.Nil) {
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: pointStr("Sorry but it seems you didn't know moff that well (for now :cry: ). Better luck next time!")})
	} else if err == nil {
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: pointStr("You know moff well. You can collect Dragonball fragments @ https://moff.io/events/dragonball soon :heart_eyes_cat: \n\n" +
				":no_entry: You need to collect fragments within **15** days, or dragon :dragon_face:  will confiscate your Dragonball fragments."),
		})
	}
	if err != nil {
		log.Error(errors.WithMessageAndReport(err, "Check lottery winner result"))
		respondBotGoesWrong(s, i)
	}

}

func respondBotGoesWrong(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	return respondEphemeralEmbedDesc(s, i, "Unknown error")
}

func respondEphemeralEmbedDesc(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{
				{
					Description: content,
				},
			},
		},
	})
}
