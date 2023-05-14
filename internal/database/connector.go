package database

import (
	"context"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"moff.io/moff-social/internal/config"
	"moff.io/moff-social/pkg/log"
	"time"
)

var (
	PublicPostgres, CommunityPostgres *gorm.DB
)

func Close(ctx context.Context) {

}

func InitCommunityPostgres(conf *config.DBCredential) {
	cli, err := gorm.Open(postgres.Open(conf.Dsn()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Error),
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: "community.",
		},
	})
	if err != nil {
		log.Fatalf("connect to pg:%v", err)
	}
	CommunityPostgres = cli

	db, err := cli.DB()
	if err != nil {
		log.Fatalf("get pg conn:%v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("ping to pg:%v", err)
	}
	log.Info("Connected to community postgres...")

	err = CommunityPostgres.AutoMigrate(
		&DiscordGuildInvites{},
		&DiscordEvents{},
		&DiscordBotReplyTemplate{},
		&DiscordMessages{},
		&DiscordForums{},
		&TwitterSpaceBackups{},
		&DiscordTempRole{},
		&DiscordTempRoleAccess{},
		&DiscordTokenPermissionedRole{},
		&DiscordRole{},
		&DiscordSnapshot{},
		&DiscordTextChannelPresence{},
		&DiscordVoiceChannelPresence{},
		&DiscordQuizGameLottery{},
		&DiscordQuizGame{},
		&DiscordGuildMemberInvites{},
		&DiscordMember{},
		&DiscordChannel{},
		&DiscordUserTrace{},
		&DiscordCampaignInvite{},
		&UserGuild{},
	)
	if err != nil {
		log.Fatalf("autoMigrate tables:%v", err)
	}
	initDiscordTempRole()
}

func InitPublicPostgres(conf *config.DBCredential) {
	cli, err := gorm.Open(postgres.Open(conf.Dsn()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Error),
	})
	if err != nil {
		log.Fatalf("connect to pg:%v", err)
	}
	PublicPostgres = cli

	db, err := cli.DB()
	if err != nil {
		log.Fatalf("get pg conn:%v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("ping to pg:%v", err)
	}
	log.Info("Connected to public postgres...")

	//err = PublicPostgres.AutoMigrate()
	//if err != nil {
	//	log.Fatalf("autoMigrate tables:%v", err)
	//}
}

func initDiscordTempRole() {
	roles := []*DiscordTempRole{
		{
			ID:             1,
			GuildID:        "981117893582389278",
			ChannelID:      "1000047456903508018",
			TempRoleID:     "996769243414671409",
			ExpirationMins: 60 * 24,
			Note:           "Hippo server",
			CreatedAt:      time.Now().UnixMilli(),
		},
		{
			ID:             2,
			GuildID:        "915445727600205844",
			ChannelID:      "997064393579843654",
			TempRoleID:     "997064381475078255",
			ExpirationMins: 60 * 24,
			Note:           "moff server",
			CreatedAt:      time.Now().UnixMilli(),
		},
	}
	for _, role := range roles {
		if err := role.Save(); err != nil {
			log.Fatal(err)
		}
	}
}
