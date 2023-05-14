package discord

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
	"math/rand"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"time"
)

func frequentAskQuestionCommandHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		data := i.ApplicationCommandData()
		log.Infof("app command:%v", data.Options[0].StringValue())

		msg, err := database.DiscordBotReplyTemplate{}.SelectInteractID(data.Options[0].StringValue())
		if err != nil {
			log.Errorf("faq handler:%v", err)
			return
		}
		embeds, err := msg.GetMessageEmbeds()
		if err != nil {
			log.Errorf("faq handler:%v", err)
			return
		}
		components, err := msg.GetMessageComponents()
		if err != nil {
			log.Errorf("faq handler:%v", err)
			return
		}
		err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:      discordgo.MessageFlagsEphemeral,
				Content:    msg.Content,
				Embeds:     embeds,
				Components: components,
			},
		})
		if err != nil {
			log.Errorf("faq handler:%v", err)
			return
		}
	// Autocomplete options introduce a new interaction type (8) for returning custom autocomplete results.
	case discordgo.InteractionApplicationCommandAutocomplete:
		data := i.ApplicationCommandData()
		var (
			choices []*discordgo.ApplicationCommandOptionChoice
			err     error
		)
		// not typing anything
		if data.Options[0].StringValue() == "" {
			choices, err = database.DiscordBotReplyTemplate{}.SelectDefaultChoices()
		} else {
			choices, err = database.DiscordBotReplyTemplate{}.SelectFaqLike(data.Options[0].StringValue())
		}
		if err != nil {
			log.Errorf("faq handler:%v", err)
			return
		}
		err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionApplicationCommandAutocompleteResult,
			Data: &discordgo.InteractionResponseData{
				Choices: choices, // This is basically the whole purpose of autocomplete interaction - return custom options to the user.
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if err != nil {
			log.Errorf("respond autocomplete:%v", err)
			return
		}
	}
}

func listmoffEventsCommandHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer logHandlerDuration("list moff events", time.Now())
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
			interactionResponseEditOnError(s, i)
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
		log.Error(errors.WrapAndReport(err, "quick response to list events"))
		return
	}

	var (
		campaigns          []*database.Campaigns
		title, campaignStr string
	)
	switch options[0].Name {
	case "ongoing":
		title = "Ongoing events on moff.io"
		campaignStr = "These are the ongoing events on moff.io! Come check on here :"
		campaigns, err = database.Campaigns{}.QueryOngoing(10, 0)
	case "upcoming":
		title = "Upcoming events on moff.io"
		campaignStr = "These are the upcoming events on moff.io! Come check on here :"
		campaigns, err = database.Campaigns{}.QueryUpcoming(10, 0)
	}
	if err != nil {
		log.Error(err)
		interactionResponseEditOnError(s, i)
	}
	// todo 当前没有活动时的提醒
	if len(campaigns) == 0 {
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{
				{
					Type:        discordgo.EmbedTypeImage,
					Title:       title,
					URL:         "https://moff.io/events",
					Description: "No events found, check moff official website please :hushed:",
					// 嵌入的左边栏的颜色，最左方的竖条
					Color: 6095103,
					// 在嵌入消息的顶部，icon在前，名字在后
					Author: moffAuthor,
				},
			},
		})
		if err != nil {
			log.Error(err)
			interactionResponseEditOnError(s, i)
		}
		return
	}
	for _, campaign := range campaigns {
		end := time.Unix(0, campaign.EndDate*int64(time.Millisecond))
		endTime := end.UTC().Format("2006.01.02 15:04")
		eventLink := fmt.Sprintf("https://moff.io/events?campaign_id=%v", campaign.CampaignID)
		campaignStr += "\n\n**[" + campaign.Name + "](" + eventLink + ")** | " + campaign.DescriptionText + " | End in `" + endTime + "` " + newDiscordEmoji().Random()
	}
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Type:        discordgo.EmbedTypeImage,
				Title:       title,
				URL:         "https://moff.io/events",
				Description: campaignStr,
				// 嵌入的左边栏的颜色，最左方的竖条
				Color: 6095103,
				// 在嵌入消息的顶部，icon在前，名字在后
				Author: moffAuthor,
			},
		},
	})
	if err != nil {
		log.Error(err)
		interactionResponseEditOnError(s, i)
	}
}

type discordEmoji struct {
	list []string
}

func newDiscordEmoji() *discordEmoji {
	return &discordEmoji{
		list: []string{
			"😀", "😃", "😄", "😁", "😆", "😅", "😂", "🤣", "🥲", "☺️", "😊", "😇", "🙂", "🙃", "😉", "😌", "😍", "🥰", "😘", "😗", "😙", "😚", "😋", "😛", "😝", "😜", "🤪", "🤨", "🧐", "🤓",
			"😎", "🥸", "🤩", "🥳", "😏", "😒", "😞", "😔", "😟", "😕", "🙁", "☹️", "😣", "😖", "😫", "😩", "🥺", "😢", "😭", "😤", "😠", "😡", "🤬", "🤯", "😳", "😥", "😓", "🤗", "🤔", "🤭",
			"🤫", "🤥", "😶", "😐", "😑", "😬", "😯", "😦", "😧", "😮", "😲", "🥱", "😴", "🤤", "😪", "😵", "🤐", "🥴", "🤧", "🤑", "🤠", "😈", "👿", "👹", "👺", "🤡", "💩", "👻", "💀", "☠️",
			"👽", "👾", "🤖", "🎃", "😺", "😸", "😹", "😻", "😼", "😽", "🙀", "😿", "😾",
		},
	}
}

func (in *discordEmoji) Random() string {
	size := len(in.list)
	if size == 0 {
		return ""
	}
	rand.Seed(time.Now().UnixNano())
	i := rand.Intn(size)
	return in.list[i]
}
