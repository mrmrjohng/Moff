package discord

import (
	"context"
	"github.com/bwmarrin/discordgo"
	"moff.io/moff-social/internal/aws"
	"moff.io/moff-social/internal/config"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"os"
	"os/signal"
	"sync"
	"time"
)

var (
	session *discordgo.Session
)

func SetupBot(ctx context.Context, bot *config.DiscordBot) {
	err := initBotSessionAndHandlers(bot)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()
	if err := initOps(ctx, session); err != nil {
		log.Fatalf("Discord initialization: %v", err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Infof("Gracefully shutting down")
}

func initBotSessionAndHandlers(bot *config.DiscordBot) error {
	ses, err := discordgo.New("Bot " + bot.AuthToken)
	if err != nil {
		return errors.ErrorfAndReport("create new discord session:%v", err)
	}
	session = ses
	ses.Identify.Intents = discordgo.IntentsAll
	ses.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) { log.Info("Bot is running!") })
	ses.AddHandler(messageReceiverHandler)
	ses.AddHandler(messageReactionAddEventHandler)
	ses.AddHandler(messageReactionRemoveEventHandler)
	ses.AddHandler(interactionEventHandler)
	ses.AddHandler(guildMemberAddEventHandler)
	ses.AddHandler(guildMemberRemovedEventHandler)
	ses.AddHandler(guildMemberUpdateEventHandler)
	ses.AddHandler(voiceChannelMemberUpdate)

	ses.AddHandler(GuildMembersChunkEventHandler)
	ses.AddHandler(MessageUpdateEventHandler)
	ses.AddHandler(MessageDeleteEventHandler)
	ses.AddHandler(MessageDeleteBulkEventHandler)
	ses.AddHandler(MessageReactionRemoveAllEventHandler)
	ses.AddHandler(TypingStartEventHandler)
	// 只有切换在线 闲置等才会发消息 无用
	//ses.AddHandler(PresenceUpdateEventHandler)
	ses.AddHandler(PresencesReplaceEventHandler)
	ses.AddHandler(UserUpdateEventHandler)
	ses.AddHandler(ChannelCreateEventHandler)
	ses.AddHandler(ThreadCreateEventHandler)
	if err := ses.Open(); err != nil {
		return errors.ErrorfAndReport("Cannot open the session: %v", err)
	}
	return nil
}

func initOps(ctx context.Context, s *discordgo.Session) error {
	guilds, err := initializeBotGuilds(s)
	if err != nil {
		return err
	}
	go overwriteGuildInvitesScheduler(time.Minute * 30)
	go overwriteGuildRolesScheduler(time.Hour)
	go overwriteGuildChannelsScheduler(time.Hour)
	go syncGuildsMembers()

	// 重置邀请缓存
	resetInvitesCache(guilds)
	// 邀请相关初始化
	inviterMatchPipes = make(chan *inviterMatchPipe, 500)
	go blockingTrackGuildInviter(ctx, s)
	// 校验资产相关初始化
	verifyUserAssetsPipes = make(chan *verifyUserAssetsPipe, 500)
	go blockingVerifyUserAssets()
	// 处理队列消息
	if config.Global.DiscordBot.MessageQueues.NotificationQueueURL == "" {
		log.Fatal("Notification queue url not present")
	}
	aws.Client.NewSQSWorker(ctx, config.Global.DiscordBot.MessageQueues.NotificationQueueURL, sendDiscordNotification)
	aws.Client.NewSQSWorker(ctx, config.Global.DiscordBot.MessageQueues.MemberExpQueueURL, calculateDiscordMemberExp)
	go removeCasinoAccessScheduler(ctx)
	return nil
}

var (
	moffAuthor = &discordgo.MessageEmbedAuthor{
		Name:    "moff",
		IconURL: "https://moff-social.s3.ap-southeast-1.amazonaws.com/logo/white/300.png",
	}
)

func isAuthorizedGuild(guildID string) bool {
	return config.Global.moffGuild.AuthorizedGuildsMapping[guildID]
}

func ismoffGuild(guildID string) bool {
	return guildID == config.Global.moffGuild.ID
}

var (
	initializedGuildCommandsLock sync.Mutex
	initializedGuildCommands     = make(map[string]*discordgo.MessageCreate)
)

func initializeGuildCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
	initializedGuildCommandsLock.Lock()
	defer initializedGuildCommandsLock.Unlock()

	if initializedGuildCommands[m.GuildID] != nil {
		return
	}
	initializedGuildCommands[m.GuildID] = m
	overwriteAppCommands(s, m)
	saveUserGuild(s, m.GuildID)
}

func saveUserGuild(s *discordgo.Session, guildID string) {
	guild, err := s.Guild(guildID)
	if err != nil {
		log.Error(err)
		return
	}
	ug := database.UserGuild{
		UserID:     config.Global.DiscordBot.AppID,
		GuildID:    guildID,
		GuildName:  guild.Name,
		Permission: guild.Permissions,
	}
	if err := ug.Create(); err != nil {
		log.Error(err)
		return
	}
	log.Infof("Bot joined guild %v", ug.GuildName)
}
