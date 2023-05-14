package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"io/ioutil"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/config"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"net/http"
	"time"
)

func sendAppConnectionGateway(s *discordgo.Session, i *discordgo.InteractionCreate) {
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
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       "Confirmation info",
					Description: "You're creating an interactive connection message.\n\n** We will send a public interactive connection message into this channel after confirmation?**",
				},
			},
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						&discordgo.Button{
							Style:    discordgo.SuccessButton,
							Label:    "Confirm",
							CustomID: "confirm-app-connection",
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
		log.Error(errors.WrapAndReport(err, "send app connection confirmation message"))
	}
}

func confirmAppConnection(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 响应discord OK
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick respond to check quiz game result"))
		return
	}

	msg := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{
			{
				Title: "Sync with your social and gaming accounts",
				Description: "✅ To get higher chance winning the raffle, please sync with your social and gaming accounts.\n" +
					"🔴 Do not share your private keys. We will never ask for your seed phrase. We will never DM you.",
			},
		},
		Components: []discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.Button{
						Style:    discordgo.PrimaryButton,
						Label:    "Let's Connect",
						CustomID: "connect_app_user",
					},
				},
			},
		},
	}
	for j := 0; j < 3; j++ {
		if _, err := s.ChannelMessageSendComplex(i.ChannelID, msg); err != nil {
			log.Error(errors.WrapAndReport(err, "send app connection message"))
			continue
		}
		return
	}
}

func connectAppUser(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "response interaction"))
		return
	}

	guild := botGuild(i.GuildID)
	if guild == nil {
		return
	}
	app := newWhiteLabelingApp(guild)
	if err := app.Cache(); err != nil {
		log.Error(err)
		return
	}
	conn := newAppUserConnection(app.AppID, i.Member.User)
	if err := conn.GenerateAuthorization(); err != nil {
		log.Error(err)
		return
	}

	// 生成对应的token
	if err := conn.Post(); err != nil {
		log.Error(err)
		return
	}

	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title:       "There you are",
				Description: "Click the `Connect` button below to start connecting your accounts",
			},
		},
		Components: &[]discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.Button{
						Style: discordgo.LinkButton,
						Label: "Connect",
						URL: fmt.Sprintf("%v?authorization=%v&app_id=%v", config.Global.DiscordBot.AppConnectionURL,
							conn.authorization, app.AppID),
					},
				},
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "response user connection url"))
		return
	}
}

type whiteLabelingApp struct {
	AppID          string   `json:"app_id"`
	AppName        string   `json:"app_name"`
	AppLogo        string   `json:"app_logo"`
	AppLogoURL     string   `json:"app_logo_url"`
	Permissions    int64    `json:"permissions"`
	AllowedDomains []string `json:"allowed_domains"`
	IsAdmin        bool     `json:"is_admin"`
}

func newWhiteLabelingApp(guild *discordgo.UserGuild) *whiteLabelingApp {
	app := &whiteLabelingApp{
		AppID:   fmt.Sprintf("discord_server_%v", guild.ID),
		AppName: guild.Name,
		AppLogo: guild.Icon,
		AllowedDomains: []string{
			"https://moff.io",
			"https://dev.moff.io",
			"https://test.moff.io",
			"http://localhost:3300",
			"http://localhost:3333",
		},
		IsAdmin: true,
	}
	if guild.Icon != "" {
		app.AppLogoURL = fmt.Sprintf("https://cdn.discordapp.com/icons/%v/%v.png", guild.ID, guild.Icon)
	}
	return app
}

func (a *whiteLabelingApp) Cache() error {
	appKey := fmt.Sprintf("white_labeling_app:%v", a.AppID)
	err := cache.Redis.Set(context.TODO(), appKey, common.MustGetJSONString(a), 0).Err()
	return errors.WrapAndReport(err, "cache white labeling app")
}

type appUserConnection struct {
	UserID   string `json:"user_id"`
	Avatar   string `json:"avatar"`
	Username string `json:"unique_nickname"`
	AppID    string `json:"app_id"`

	authorization string
	authKey       string
}

func newAppUserConnection(appID string, user *discordgo.User) *appUserConnection {
	conn := &appUserConnection{
		UserID:   fmt.Sprintf("discord_user_%v", user.ID),
		Username: fmt.Sprintf("%v#%v", user.Username, user.Discriminator),
		AppID:    appID,
	}
	if user.Avatar != "" {
		conn.Avatar = fmt.Sprintf("https://cdn.discordapp.com/avatars/%v/%v.png", user.ID, user.Avatar)
	}
	return conn
}

func (c *appUserConnection) GenerateAuthorization() error {
	c.authorization = fmt.Sprintf("%v:%v", c.UserID, uuid.NewString())
	c.authKey = fmt.Sprintf("user_token:%v", c.authorization)
	err := cache.Redis.Set(context.TODO(), c.authKey, common.MustGetJSONString(c), time.Hour*24).Err()
	return errors.WrapAndReport(err, "generate authorization")
}

func (c *appUserConnection) Post() error {
	data := map[string]string{
		"key":   c.authKey,
		"value": common.MustGetJSONString(c),
	}
	req, err := http.NewRequest("POST", "https://api-dev.moff.io/auth/user/authorize",
		bytes.NewBuffer([]byte(common.MustGetJSONString(data))))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "ad488e24-0073-4cac-9d75-d9c5d87e4af8")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	var rc ResponseCode
	if err := json.Unmarshal(resp, &rc); err != nil {
		return err
	}
	if rc.Code != 0 {
		return errors.Errorf("request error:%v", rc.Message)
	}
	return nil
}

type ResponseCode struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
}
