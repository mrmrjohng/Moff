package main

import (
	"context"
	"moff.io/moff-social/internal/aws"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/chains/moralis"
	"moff.io/moff-social/internal/config"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/internal/databus"
	"moff.io/moff-social/internal/discord"
	"moff.io/moff-social/internal/google"
	"moff.io/moff-social/internal/http"
	"moff.io/moff-social/internal/starter"
	"moff.io/moff-social/internal/twitter"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
)

func main() {
	log.Infof("Starting app")
	startApp()
}

func startApp() {
	defer func() {
		if i := recover(); i != nil {
			log.Fatal(errors.ErrorfAndReport("%v", i))
		}
	}()
	log.SetLevel(0)
	config.Read()
	//errors.NewLarkReporter(config.Global.LarkAlarmWebhook, time.Minute)
	google.NewClients()
	aws.Init(config.Global.AwsS3.Bucket.Name, config.Global.AwsS3.Bucket.Region)
	ctx := context.Background()
	database.InitPublicPostgres(&config.Global.Postgres)
	database.InitCommunityPostgres(&config.Global.Postgres)
	databus.InitDataBus(config.Global.KafkaServer)
	defer database.Close(ctx)
	cache.Init(&config.Global.RedisCredential)
	defer cache.Close()

	starter.Start(ctx,
		//discord.NewUnbelievaboatHandler(),
		discord.NewQuizGameManager(),
		discord.NewSingleWriteStorageEngine(),
		twitter.NewSpaceManager(),
	)

	moralis.Init(config.Global.MoralisAPIKey)
	go http.NewServer()
	discord.SetupBot(ctx, &config.Global.DiscordBot)
}
