package discord

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strconv"
	"strings"
	"time"
)

func interactionEventHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer func() {
		if i := recover(); i != nil {
			log.Errorf("interaction handler:%v", i)
		}
	}()
	err := database.DiscordMember{}.UpdateActive(i.GuildID, i.Member.User.ID)
	if err != nil {
		log.Error(err)
	}
	switch i.Type {
	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID
		for prefix, h := range messageReactionHandlers {
			if strings.HasPrefix(customID, prefix) {
				h(s, i)
				break
			}
		}
	case discordgo.InteractionApplicationCommand, discordgo.InteractionApplicationCommandAutocomplete:
		if h, ok := commandsHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	case discordgo.InteractionModalSubmit:
		if strings.HasPrefix(i.ModalSubmitData().CustomID, "create_invite_code") {
			submitInviteCodeCampaign(s, i)
			return
		}
		if h, ok := modalSubmitHandlers[i.ModalSubmitData().CustomID]; ok {
			h(s, i)
		}
	}
}

const (
	stopSnapshot = "stop_snapshot:"
)

var (
	commandsHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"faq":                          frequentAskQuestionCommandHandler,
		"events":                       listmoffEventsCommandHandler,
		"invites":                      invitesCommandHandler,
		"levels":                       levelsCommandHandler,
		"list-channel-snapshot":        listChannelSnapshots,
		"start-snapshot":               startChannelSnapshot,
		"stop-snapshot":                stopChannelSnapshotFromCommand,
		"start-twitter-space-snapshot": startTwitterSpaceSnapshot,
		"list-twitter-space-snapshot":  listTwitterSpaceSnapshot,
		"snapshot-check":               checkUserSnapshot,
		"notification":                 notificationSwitchCommandHandler,
		"temp-role-gateway":            manageTempRole,
		"send-connect":                 sendAppConnectionGateway,
		"create-invites":               createInviteCode,
		"check-invites":                listInviteCodes,
		"dashboard":                    listDashboard,
	}
	messageReactionHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		customRemoveTwitterSpace:                        removeTwitterSpaceSnapshot,
		discordQuizGameCustomIDPrefix:                   quizGameInteractionChooseAnswer,
		discordQuizGameCheckResultCustomIDPrefix:        quizGameInteractionCheckResult,
		discordQuizGameLotteryCheckResultCustomIDPrefix: quizGameLotteryInteractionCheckResult,
		solveTempAccessCustomIDPrefix:                   solveCasinoCaptcha,
		unlockTempAccessPrefix:                          sendCasinoCaptchaVerification,
		"confirm-temp-access:":                          confirmTempRoleManagement,
		listInvitePage:                                  listInviteCodesPagination,
		"verify_user_assets":                            verifyUserAssetsHandler,
		"disable_notifications":                         disableNotifications,
		"unlock_access_to_casino":                       sendCasinoCaptchaVerification,
		"confirm-app-connection":                        confirmAppConnection,
		"connect_app_user":                              connectAppUser,
		stopSnapshot:                                    stopChannelSnapshotFromInteraction,
	}

	modalSubmitHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"snapshot_minimum_duration":   calculateVoiceChannelSnapshot,
		"snapshot_minimum_words":      calculateTextChannelSnapshot,
		customAddTwitterSpaceSnapshot: submitTwitterSnapshot,
	}

	moffCommands = []*discordgo.ApplicationCommand{
		{
			Name:        "faq",
			Description: "Reply for frequent asked questions",
			Type:        discordgo.ChatApplicationCommand,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:         "autocomplete-option",
					Description:  "Type key word to search faq",
					Type:         discordgo.ApplicationCommandOptionString,
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Name: "events",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "ongoing",
					Description: "moff ongoing event list",
				},
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "upcoming",
					Description: "moff upcoming event list",
				},
			},
			Description: "List ongoing or upcoming moff events",
		},
		{
			Name: "invites",
			Type: discordgo.ChatApplicationCommand,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "leaderboard",
					Description: "List top 20 invites leaderboard",
				},
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "me",
					Description: "Get your current invites amount",
				},
			},
			Description: "Show invites information",
		},
		{
			Name:        "create-invites",
			Description: "Create permanent invite link",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "channel",
					Description: "Invite Users to Specific Channels",
					Type:        discordgo.ApplicationCommandOptionChannel,
					Required:    true,
				},
			},
		},
		{
			Name:        "check-invites",
			Description: "List latest permanent invite links",
		},
		{
			Name:        "dashboard",
			Description: "Get a link to the dashboard",
		},
		{
			Name: "notification",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "enable",
					Description: "Enable direct message notification",
				},
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "disable",
					Description: "Disable direct message notification",
				},
			},
			Description: "Direct message notification switch",
		},
		{
			Name:        "list-channel-snapshot",
			Description: "list history snapshots for channels",
			Type:        discordgo.ChatApplicationCommand,
		},
		{
			Name:        "start-snapshot",
			Description: "Start snapshot for given channel",
			Type:        discordgo.ChatApplicationCommand,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "channel",
					Description: "The channel to start snapshot",
					Type:        discordgo.ApplicationCommandOptionChannel,
					Required:    true,
				},
			},
		},
		{
			Name:        "stop-snapshot",
			Description: "Stop snapshot for given channel",
			Type:        discordgo.ChatApplicationCommand,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "channel",
					Description: "The channel to stop snapshot",
					Type:        discordgo.ApplicationCommandOptionChannel,
					Required:    true,
				},
			},
		},
		{
			Name:        "start-twitter-space-snapshot",
			Description: "Start snapshot for twitter space",
			Type:        discordgo.ChatApplicationCommand,
		},
		{
			Name:        "temp-role-gateway",
			Description: "Manage temp role",
			Type:        discordgo.ChatApplicationCommand,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "role",
					Description: "The role to manage",
					Type:        discordgo.ApplicationCommandOptionRole,
					Required:    true,
				},
				{
					Name:        "expiration-min",
					Description: "The minutes to expire the role",
					Type:        discordgo.ApplicationCommandOptionInteger,
					Required:    true,
				},
			},
		},
		{
			Name:        "send-connect",
			Description: "Send a connect interactive message",
			Type:        discordgo.ChatApplicationCommand,
		},
		{
			Name: "list-twitter-space-snapshot",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "ongoing",
					Description: "List ongoing snapshots for twitter space",
				},
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "finished",
					Description: "List finished snapshots for twitter space",
				},
			},
			Description: "List snapshots for twitter space",
		},
		{
			Name:        "snapshot-check",
			Description: "Check if you are being snapshotted!",
			Type:        discordgo.ChatApplicationCommand,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "channel",
					Description: "The voice channel under snapshot",
					Type:        discordgo.ApplicationCommandOptionChannel,
					Required:    true,
				},
			},
		},
	}

	authorizedCommands = []*discordgo.ApplicationCommand{
		{
			Name:        "create-invites",
			Description: "Create permanent invite link",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "channel",
					Description: "Invite Users to Specific Channels",
					Type:        discordgo.ApplicationCommandOptionChannel,
					Required:    true,
				},
			},
		},
		{
			Name:        "check-invites",
			Description: "List latest permanent invite links",
		},
		{
			Name:        "send-connect",
			Description: "Send a connect interactive message",
			Type:        discordgo.ChatApplicationCommand,
		},
		{
			Name:        "start-snapshot",
			Description: "Start snapshot for given channel",
			Type:        discordgo.ChatApplicationCommand,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "channel",
					Description: "The channel to start snapshot",
					Type:        discordgo.ApplicationCommandOptionChannel,
					Required:    true,
				},
			},
		},
		{
			Name:        "list-snapshot",
			Description: "list history snapshots for channels",
			Type:        discordgo.ChatApplicationCommand,
		},
		{
			Name:        "stop-snapshot",
			Description: "Stop snapshot for given channel",
			Type:        discordgo.ChatApplicationCommand,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "channel",
					Description: "The channel to stop snapshot",
					Type:        discordgo.ApplicationCommandOptionChannel,
					Required:    true,
				},
			},
		},
		{
			Name:        "snapshot-check",
			Description: "Check if you are being snapshotted!",
			Type:        discordgo.ChatApplicationCommand,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "channel",
					Description: "The voice channel under snapshot",
					Type:        discordgo.ApplicationCommandOptionChannel,
					Required:    true,
				},
			},
		},
		{
			Name:        "start-twitter-space-snapshot",
			Description: "Start snapshot for twitter space",
			Type:        discordgo.ChatApplicationCommand,
		},
		{
			Name: "list-twitter-space-snapshot",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "ongoing",
					Description: "List ongoing snapshots for twitter space",
				},
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "finished",
					Description: "List finished snapshots for twitter space",
				},
			},
			Description: "List snapshots for twitter space",
		},
		{
			Name:        "dashboard",
			Description: "Get a link to the dashboard",
		},
	}
)

func logHandlerDuration(handler string, start time.Time) {
	log.Debugf("duration - Handler %v cost %v", handler, time.Since(start))
}

func manageTempRole(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !IsAdminPermission(i.Member.Permissions) {
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "Not allowed:thinking: ",
			},
		})
		if err != nil {
			log.Error(errors.WrapAndReport(err, "response interaction"))
		}
		return
	}

	options := i.ApplicationCommandData().Options
	if len(options) < 2 {
		return
	}
	// TODO 尝试添加角色一次，检查是否有权限 (如果同为admin，需要bot在对应角色的权限前面)
	roleId := options[0].Value.(string)
	expiration := options[1].Value.(float64)
	role := i.ApplicationCommandData().Resolved.Roles[roleId]
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{
				{
					Title: "Confirmation info",
					Description: fmt.Sprintf("You're creating temp access for role `%v`\n"+
						"Role expiration: `%v` minutes.\n\n"+
						"**Send a public gateway message into this channel after confirmation?**", role.Name, expiration),
				},
			},
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						&discordgo.Button{
							Style:    discordgo.SuccessButton,
							Label:    "Confirm",
							CustomID: fmt.Sprintf("confirm-temp-access:%v:%v", role.ID, expiration),
						},
						&discordgo.Button{
							Style:    discordgo.DangerButton,
							Label:    "NeverMind",
							CustomID: "nevermind",
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "send temp access confirmation message"))
	}
}

func confirmTempRoleManagement(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 响应discord OK
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick respond to confirm temp role management"))
		return
	}

	customID := i.MessageComponentData().CustomID
	array := strings.Split(customID, ":")
	if len(array) < 3 {
		log.Warnf("Invalid temp role confirmation custom id %v", customID)
		return
	}
	roleID := array[1]
	expirationMin, err := strconv.ParseInt(array[2], 10, 64)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "parse expiration min"))
		return
	}
	role := database.DiscordTempRole{
		GuildID:        i.GuildID,
		ChannelID:      i.ChannelID,
		TempRoleID:     roleID,
		ExpirationMins: expirationMin,
		CreatedAt:      time.Now().UnixMilli(),
	}
	if err := role.Create(); err != nil {
		log.Error(err)
		return
	}

	roles, err := database.DiscordTempRole{}.SelectAll()
	if err != nil {
		log.Error(err)
		return
	}
	tempRoles = roles
	if err := tryToSendCasinoAccessMessage(s, role.ChannelID, role.TempRoleID); err != nil {
		log.Error(err)
	}
}

func tryToSendCasinoAccessMessage(s *discordgo.Session, channelID, roleID string) error {
	msg := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{
			{
				Title:       "You are entering a place with no laws \U0001F974",
				Description: "Click `I fully aware of what's comin🎰` to start the verification.",
			},
		},
		Components: []discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.Button{
						Style: discordgo.PrimaryButton,
						Label: "I fully aware of what's comin🎰",
						Emoji: discordgo.ComponentEmoji{
							Name: "🔓",
						},
						CustomID: fmt.Sprintf("%v%v", unlockTempAccessPrefix, roleID),
					},
				},
			},
		},
	}
	for i := 0; i < 3; i++ {
		if _, err := s.ChannelMessageSendComplex(channelID, msg); err != nil {
			log.Error(errors.WrapAndReport(err, "send casino captcha verification"))
			continue
		}
		return nil
	}
	return errors.NewWithReport("try to send casino captcha verification max try exceeded")
}
