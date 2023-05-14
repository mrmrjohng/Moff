package discord

import (
	"context"
	"encoding/json"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/bwmarrin/discordgo"
	"github.com/tidwall/gjson"
	"moff.io/moff-social/internal/aws"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strings"
)

func notificationSwitchCommandHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "Please choose one option to checkout event list.",
			},
		})
		if err != nil {
			log.Error(err)
		}
		return
	}
	// 快速响应，等待后续响应用户
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick respond to notification switch"))
		return
	}

	var discordID string
	if i.Member != nil {
		discordID = i.Member.User.ID
	}
	if i.User != nil {
		discordID = i.User.ID
	}

	var (
		content string
	)
	switch options[0].Name {
	case "enable":
		content = "Huarry, dm notifications **enabled** 😉"
		err = database.DiscordMember{}.EnableNotification(i.GuildID, discordID)
	case "disable":
		content = "Sorry, dm notifications **disabled**...\n\nYou can enable this notification from guild again 😉"
		err = database.DiscordMember{}.DisableNotification(i.GuildID, discordID)
	}
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Description: content,
				Author:      moffAuthor,
			},
		},
	})
	if err != nil {
		log.Errorf("switch notification interaction:%v", err)
	}
	log.Debugf("discord user %v switched notifications from command", discordID)
}

func disableNotifications(s *discordgo.Session, i *discordgo.InteractionCreate) {
	log.Debug("disable notifications")
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick response to disable notification"))
		return
	}

	var discordID string
	if i.Member != nil {
		discordID = i.Member.User.ID
	}
	if i.User != nil {
		discordID = i.User.ID
	}

	err = database.DiscordMember{}.DisableNotification(i.GuildID, discordID)
	if err != nil {
		log.Error(err)
		interactionResponseEditOnError(s, i)
		return
	}
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Description: "Sorry, dm notifications **disabled**...\nYou can enable this notification from moff guild again 😉",
				Author:      moffAuthor,
			},
		},
	})
	if err != nil {
		log.Errorf("disable notification interaction:%v", err)
	}
	log.Debugf("discord user %v disabled notifications", discordID)
}

func sendDiscordNotification(msg *types.Message) (deleteMsg bool, err error) {
	// 目前大量dm会导致被discord判断spam
	notificationType := gjson.Get(*msg.Body, "type").String()
	switch notificationType {
	case "discord":
		break
	default:
		log.Errorf("unknown notification type %v", notificationType)
		return true, nil
	}
	queueMsg := *msg.Body
	discord := gjson.Get(queueMsg, "discord").String()
	notified := gjson.Get(queueMsg, "on_notified").String()
	var ntfn discordNotification
	if err := json.Unmarshal([]byte(discord), &ntfn); err != nil {
		return true, errors.WrapAndReport(err, "Unmarshal discord notification")
	}
	if ntfn.IsMentionUser {
		if ntfn.MentionUserID == "" {
			log.Warnf("message %v mention user but got empty user id", queueMsg)
			return true, nil
		}
		// 检查用户是否在工会
		_, err := session.GuildMember(ntfn.GuildID, ntfn.MentionUserID)
		if err != nil {
			if strings.Contains(err.Error(), "Unknown Member") {
				log.Debugf("User %v not in discord guild %v", ntfn.MentionUserID, ntfn.GuildID)
				return true, nil
			}
			if strings.Contains(err.Error(), "Unknown User") {
				log.Errorf("inviter %v not found from guild", ntfn.MentionUserID)
				return true, nil
			}
			return false, errors.WrapfAndReport(err, "check user %v in guild %v", ntfn.MentionUserID, ntfn.GuildID)
		}
	}
	// 发送通知
	embeds, err := ntfn.Message.GetMessageEmbeds()
	if err != nil {
		return false, err
	}
	components, err := ntfn.Message.GetMessageComponents()
	if err != nil {
		return false, err
	}
	var (
		channelID string
	)
	// 根据消息配置设置推送的channel
	switch ntfn.MsgType {
	case NotificationTypeDM:
		channel, err := session.UserChannelCreate(ntfn.MentionUserID)
		if err != nil {
			return false, errors.WrapAndReport(err, "create discord user channel")
		}
		channelID = channel.ID
	default:
		channelID = ntfn.ChannelID
	}
	log.Debugf("Sending message to channel %v", channelID)
	_, err = session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:    ntfn.Message.Content,
		Embeds:     embeds,
		Components: components,
	})
	if err != nil {
		return false, errors.WrapfAndReport(err, "send notification message to user %v in guild %v",
			ntfn.MentionUserID, ntfn.GuildID)
	}
	notificationPostHandle(notified)
	return true, nil
}

func notificationPostHandle(callback string) {
	if callback == "" {
		return
	}
	callbackQueueURL := gjson.Get(callback, "callback_queue_url").String()
	if callbackQueueURL == "" {
		log.Error(errors.ErrorfAndReport("Calling back on community notification but callback queue not found"))
		return
	}
	callbackData := gjson.Get(callback, "callback_data").String()
	err := aws.Client.MultiTrySendMessageToSQS(context.TODO(), callbackQueueURL, callbackData, 3)
	if err != nil {
		log.Error(err)
	}
}

type NotificationType string

const (
	NotificationTypeDM      = "direct_message"
	NotificationTypeChannel = "channel_message"
)

type discordNotification struct {
	GuildID       string            `json:"guild_id"`
	ChannelID     string            `json:"channel_id"`
	IsMentionUser bool              `json:"is_mention_user"`
	MentionUserID string            `json:"mention_user_id"`
	MsgType       NotificationType  `json:"msg_type"`
	Message       *database.Message `json:"message"`
}

type onNotified struct {
	CallbackQueueURL string      `json:"callback_queue_url"`
	CallbackData     interface{} `json:"callback_data"`
}
