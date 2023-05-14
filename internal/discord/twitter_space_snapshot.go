package discord

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/internal/twitter"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strconv"
	"strings"
)

const (
	customAddTwitterSpaceSnapshot   = "add_twitter_space_snapshot"
	customTwitterSpaceURL           = "twitter_space_url"
	customTwitterSnapshotMinSeconds = "twitter_snapshot_min_seconds"
	customSnapshotCampaignID        = "snapshot_campaign_id"
	customSnapshotCampaignName      = "snapshot_campaign_name"
	customRemoveTwitterSpace        = "terminate_twitter_space:"
)

func removeTwitterSpaceSnapshot(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !IsAdminPermission(i.Member.Permissions) {
		respondSnapshotError(s, i, "Not allowed:thinking: ")
		return
	}

	customID := i.MessageComponentData().CustomID
	arr := strings.Split(customID, customRemoveTwitterSpace)
	snapshot, err := database.TwitterSpaceSnapshotOwns{}.SelectOne(i.GuildID, arr[1])
	if err != nil {
		log.Error(err)
		return
	}
	if snapshot == nil {
		log.Warnf("Snapshot %v not found for guild %v", arr[1], i.GuildID)
		return
	}
	if snapshot.EndedAt != nil {
		respondSnapshotError(s, i, "Twitter snapshot already finished.")
		return
	}
	snapshot.TerminatorDiscordID = i.Member.User.ID
	if err := snapshot.Delete(); err != nil {
		log.Error(err)
		return
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{
				{
					Title: "`⛔`Snapshot is off!",
					Description: fmt.Sprintf("**Space Name**:%v\n**Space Start Time**:<t:%v:T><t:%v:R>\n**Creater**:<@%v>",
						snapshot.SpaceTitle, snapshot.StartTime().Unix(), snapshot.StartTime().Unix(),
						snapshot.StarterDiscordID),
				},
			},
		},
	})
}

func listTwitterSpaceSnapshot(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !IsAdminPermission(i.Member.Permissions) {
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "Not allowed:thinking: ",
			},
		})
		if err != nil {
			log.Error(err)
		}
		return
	}
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "Please choose one option to checkout twitter spaces.",
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
	switch options[0].Name {
	case "ongoing":
		listOngoingTwitterSpaceSnapshot(s, i)
	case "finished":
		listFinishedTwitterSpaceSnapshot(s, i)
	}
}

func listOngoingTwitterSpaceSnapshot(s *discordgo.Session, i *discordgo.InteractionCreate) {
	snapshots, err := database.TwitterSpaceSnapshotOwns{}.SelectOngoing(i.GuildID)
	if err != nil {
		log.Error(err)
		return
	}
	var (
		title, desc string
		components  []discordgo.MessageComponent
		row         discordgo.ActionsRow
	)
	if len(snapshots) > 0 {
		title = "Latest ongoing twitter space"
	} else {
		title = "There are no ongoing twitter spaces for now.."
	}
	for _, snapshot := range snapshots {
		row.Components = append(row.Components, &discordgo.Button{
			Style: discordgo.LinkButton,
			Label: ellipsis(snapshot.SpaceTitle, 50),
			URL:   snapshot.SpaceURL,
		})
		row.Components = append(row.Components, &discordgo.Button{
			Style:    discordgo.DangerButton,
			Label:    "terminate",
			CustomID: fmt.Sprintf("%v%v", customRemoveTwitterSpace, snapshot.SpaceID),
		})
		components = append(components, row)
		row = discordgo.ActionsRow{}
	}
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title:       title,
				Description: desc,
			},
		},
		Components: &components,
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick respond to list twitter spaces"))
	}
}

func listFinishedTwitterSpaceSnapshot(s *discordgo.Session, i *discordgo.InteractionCreate) {
	snapshots, err := database.TwitterSpaceSnapshotOwns{}.SelectFinished(i.GuildID)
	if err != nil {
		log.Error(err)
		return
	}
	var (
		title, desc string
	)
	if len(snapshots) == 0 {
		title = "There are no finished twitter spaces for now.."
	} else {
		title = "Latest finished twitter space"
	}
	for i, snapshot := range snapshots {
		var content string
		if snapshot.ParticipantLink != "" {
			content = fmt.Sprintf("\n\n**%v. Space**:[%v](%v)\n　[Participants link](%v)", i+1,
				snapshot.SpaceTitle, snapshot.SpaceURL, snapshot.ParticipantLink)
		} else {
			content = fmt.Sprintf("\n\n**%v. Space**:[%v](%v)", i+1,
				snapshot.SpaceTitle, snapshot.SpaceURL)
		}
		if snapshot.StartedAt != nil {
			content += fmt.Sprintf("\n　**Start Time**:<t:%v>", snapshot.StartedAt.Unix())
		}
		content += fmt.Sprintf("\n　**Finished Time**:<t:%v>", snapshot.EndedAt.Unix())
		// 检查是否字符超限
		if len(desc+content) > 4096 {
			break
		}
		desc += content
	}
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title:       title,
				Description: desc,
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick respond to list twitter spaces"))
	}
}

func ellipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return fmt.Sprintf("%v...", s[:maxLen])
}

func startTwitterSpaceSnapshot(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !IsAdminPermission(i.Member.Permissions) {
		respondSnapshotError(s, i, "Not allowed:thinking: ")
		return
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			Flags:    discordgo.MessageFlagsEphemeral,
			CustomID: customAddTwitterSpaceSnapshot,
			Title:    "Twitter Space Snapshot",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:  customTwitterSpaceURL,
							Label:     "Twitter Space URL",
							Style:     discordgo.TextInputShort,
							Required:  true,
							MaxLength: 200,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    customTwitterSnapshotMinSeconds,
							Label:       "Snapshot Seconds",
							Style:       discordgo.TextInputShort,
							Placeholder: "Minimum seconds to consider a valid entry",
							Required:    true,
							MaxLength:   20,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							Label:       "Event ID",
							Style:       discordgo.TextInputShort,
							Placeholder: "Optional,automatically write whitelist to",
							Required:    false,
							MaxLength:   100,
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "respond twitter space modal"))
	}
}

func submitTwitterSnapshot(s *discordgo.Session, i *discordgo.InteractionCreate) {
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

	data := i.ModalSubmitData()
	spaceURL := data.Components[0].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
	inputSecondStr := data.Components[1].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
	campaignID := data.Components[2].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
	snapshotSeconds, err := strconv.ParseInt(inputSecondStr, 10, 64)
	if err != nil || snapshotSeconds < 0 {
		respondEditSnapshotError(s, i, "**No valid twitter space minimum entry seconds present**")
		return
	}

	spaceID := twitter.SpaceIDFromURL(spaceURL)
	if spaceID == "" || spaceID == spaceURL {
		respondEditSnapshotError(s, i, "**No valid twitter space URL present**")
		return
	}

	app, err := database.WhiteLabelingApps{}.SelectOne(i.GuildID)
	if err != nil {
		log.Error(err)
		return
	}
	if app == nil {
		respondEditSnapshotError(s, i, "**The dashboard might be in maintenance.\nPlease get in touch with our support to know more: https://t.me/Darthclaire5")
		return
	}

	var (
		campaign *database.Campaigns
	)
	if campaignID != "" {
		// 检查campaign是否存在与campaign的归属
		campaign, err = database.Campaigns{}.SelectOne(campaignID)
		if err != nil {
			log.Error(err)
			return
		}
		if campaign == nil || campaign.Status != "reviewed" {
			respondEditSnapshotError(s, i, "`We cannot locate specified event`")
			return
		}
		if app.AppID != campaign.AppID {
			respondEditSnapshotError(s, i, "`You cannot write to specified event`")
			return
		}
		if campaign.ParticipateLink != "" && spaceID != campaign.SpaceID() {
			respondEditSnapshotError(s, i, "`Twitter space URL not identical to event participate link`")
			return
		}
	}

	// 检查当前是否已经创建snapshot
	snapshotOwns, err := database.TwitterSpaceSnapshotOwns{}.SelectOne(i.GuildID, spaceID)
	if err != nil {
		log.Error(err)
		return
	}
	if snapshotOwns != nil {
		respondEditTwitterSnapshotCreated(s, i, snapshotOwns)
		return
	}
	// 检查当前同时进行中的snapshot总数
	snapshots, err := database.TwitterSpaceSnapshotOwns{}.SelectOngoing(i.GuildID)
	if err != nil {
		log.Error(err)
		return
	}
	if len(snapshots) > 3 {
		respondEditSnapshotError(s, i, "**Max ongoing twitter space limit reached.**")
		return
	}

	manager := twitter.NewSpaceManager()
	ownerships, tips := manager.CreateTwitterSpaceSnapshot(i.GuildID, i.Member.User.ID, spaceURL, campaign)
	if tips != "" {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: tips,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}
	respondEditTwitterSnapshotCreated(s, i, ownerships)
}

func respondSnapshotError(s *discordgo.Session, i *discordgo.InteractionCreate, tips string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       "`🐞`Snapshot error`🐞`",
					Description: tips,
				},
			},
		},
	})
}

func respondEditSnapshotError(s *discordgo.Session, i *discordgo.InteractionCreate, tips string) {
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title:       "`🐞`Snapshot error`🐞`",
				Description: tips,
			},
		},
	})
}

func respondEditTwitterSnapshotCreated(s *discordgo.Session, i *discordgo.InteractionCreate, ownerships *database.TwitterSpaceSnapshotOwns) {
	var (
		startTime string
		endDesc   string
		cmp       *[]discordgo.MessageComponent
	)
	// 开始时间描述
	if ownerships.StartedAt != nil {
		startTime = fmt.Sprintf("<t:%v:T><t:%v:R>", ownerships.StartedAt.Unix(), ownerships.StartedAt.Unix())
	} else if ownerships.ScheduledStartedAt != nil {
		startTime = fmt.Sprintf("<t:%v:T><t:%v:R>", ownerships.ScheduledStartedAt.Unix(), ownerships.ScheduledStartedAt.Unix())
	} else {
		startTime = "`🕐🕐🕐`"
	}
	// 未结束时需要添加结束按钮
	if ownerships.EndedAt != nil {
		endDesc = fmt.Sprintf("\n**Space End Time**:<t:%v:T><t:%v:R>", ownerships.EndedAt.Unix(), ownerships.EndedAt.Unix())
	} else {
		cmp = &[]discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.Button{
						Style: discordgo.DangerButton,
						Label: "Stop Snapshot",
						Emoji: discordgo.ComponentEmoji{
							Name: "◻️",
						},
						CustomID: fmt.Sprintf("%v%v", customRemoveTwitterSpace, ownerships.SpaceID),
					},
				},
			},
		}
	}
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title: "`🔴`Snapshot is on!",
				Description: fmt.Sprintf("**Space Name**:%v\n**Space Start Time**:%v\n**Creater**:<@%v>%v",
					ownerships.SpaceTitle, startTime, ownerships.StarterDiscordID, endDesc),
			},
		},
		Components: cmp,
	})
}
