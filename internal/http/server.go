package http

import (
	"context"
	"encoding/csv"
	"github.com/gin-gonic/gin"
	"moff.io/moff-social/internal/aws"
	"moff.io/moff-social/internal/databus"
	"moff.io/moff-social/internal/discord"
	"moff.io/moff-social/internal/twitter"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"net/http"
	"os"
)

func NewServer() {
	router := gin.Default()
	//gin.SetMode(gin.ReleaseMode)
	router.Use(gin.Recovery())
	router.GET("/hello", func(ctx *gin.Context) {
		defer func() {
			if i := recover(); i != nil {
				log.Error(errors.ErrorfAndReport("%v", i))
			}
		}()
		err := databus.GetDataBus().PublishRaw("discord_topic", []byte("hello world"))
		if err != nil {
			log.Error(err)
		}
		ctx.JSONP(http.StatusOK, map[string]interface{}{
			"hello": "world",
		})
	})
	router.POST("/discord/quiz_game_lottery", discord.SaveQuizGameLottery)
	router.POST("/discord/quiz_game", discord.SaveQuizGame)
	router.DELETE("/discord/quiz_game", discord.DeleteQuizGame)
	router.GET("/twitter/snapshot", func(ctx *gin.Context) {
		// curl http://127.0.0.1:8080/twitter/snapshot?space_id=1dRKZMeWNLgxB
		spaceID := ctx.Query("space_id")
		if spaceID == "" {
			ctx.JSONP(http.StatusOK, map[string]interface{}{
				"error": "space id not present",
			})
			return
		}
		err := twitter.WriteTwitterSnapshot(spaceID)
		if err != nil {
			ctx.JSONP(http.StatusOK, map[string]interface{}{
				"error": err.Error(),
			})
			return
		}
		ctx.JSONP(http.StatusOK, map[string]interface{}{
			"success": true,
		})
	})
	if err := router.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}

func writeTemp(ctx *gin.Context) {
	file, err := os.OpenFile("/cache/test.csv", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		ctx.JSONP(http.StatusOK, map[string]interface{}{
			"error": err.Error(),
		})
		return
	}
	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"title1", "title2"}); err != nil {
		ctx.JSONP(http.StatusOK, map[string]interface{}{
			"error": err.Error(),
		})
		return
	}
	if err := writer.Write([]string{"content1", "content2"}); err != nil {
		ctx.JSONP(http.StatusOK, map[string]interface{}{
			"error": err.Error(),
		})
		return
	}
	writer.Flush()
	if err := file.Close(); err != nil {
		ctx.JSONP(http.StatusOK, map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	// 写入s3
	file, err = os.Open("/cache/test.csv")
	if err != nil {
		ctx.JSONP(http.StatusOK, map[string]interface{}{
			"error": err.Error(),
		})
		return
	}
	err = aws.Client.PutFileToS3WithPublicRead(context.TODO(), "moff-public",
		"community/whitelist/discord/2022/11/25/test.csv", file)
	if err != nil {
		ctx.JSONP(http.StatusOK, map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	ctx.JSONP(http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

func writeToFile(ctx *gin.Context) error {
	file, err := os.OpenFile("test.csv", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"title1", "title2"}); err != nil {
		return err
	}
	if err := writer.Write([]string{"content1", "content2"}); err != nil {
		return err

	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return nil
}
