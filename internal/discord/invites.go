package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/fatih/structs"
	"gorm.io/gorm"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strconv"
	"strings"
	"time"
)

func createInviteCode(s *discordgo.Session, i *discordgo.InteractionCreate) {
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
		return
	}

	// 让用户设置快照筛选时长
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			Flags:    discordgo.MessageFlagsEphemeral,
			CustomID: "create_invite_code:" + options[0].Value.(string),
			Title:    "Create Permanent Invites Link",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "campaign_name",
							Label:       "Campaign Name",
							Placeholder: "Based on product,promotion,target audience",
							Style:       discordgo.TextInputShort,
							Required:    true,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "campaign_source",
							Label:       "Campaign Source",
							Placeholder: "Tracking where does the traffic comes from",
							Style:       discordgo.TextInputShort,
							Required:    true,
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "response user snapshot input modal"))
		return
	}
}

const (
	defaultListInviteCodeCount = 5
	listInvitePage             = "list_invites_page:"
)

func listInviteCodes(s *discordgo.Session, i *discordgo.InteractionCreate) {
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

	// 快速响应，等待后续响应用户
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

	// 获取app
	app, err := database.WhiteLabelingApps{}.SelectOne(i.GuildID)
	if err != nil {
		log.Error(err)
		return
	}
	if app == nil || app.CommunityDashboardURL == "" {
		app = &database.WhiteLabelingApps{
			CommunityDashboardURL: "https://t.me/Darthclaire5",
		}
	}
	hasNextPage, desc, err := getInviteCodesPagination(i.GuildID, 1)
	if err != nil {
		log.Error(err)
		return
	}
	// 查询服务器最近创建的邀请链接
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title:       "Latest invites",
				Description: desc,
			},
		},
		Components: &[]discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.Button{
						Style:    discordgo.PrimaryButton,
						Label:    "Prev page",
						CustomID: fmt.Sprintf("%v%v", listInvitePage, 1),
						Disabled: true,
					},
					&discordgo.Button{
						Style:    discordgo.PrimaryButton,
						Label:    "Next page",
						CustomID: fmt.Sprintf("%v%v", listInvitePage, 2),
						Disabled: !hasNextPage,
					},
				},
			},
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.Button{
						Style: discordgo.LinkButton,
						Label: "Check out invites and their performance",
						Emoji: discordgo.ComponentEmoji{
							Name: "🔍",
						},
						URL: app.CommunityDashboardURL,
					},
				},
			},
		},
	})
	if err != nil {
		log.Error(err)
	}
}

func getInviteCodesPagination(guildID string, pageNum int) (bool, string, error) {
	count, err := database.DiscordCampaignInvite{}.SelectServerCount(guildID)
	if err != nil {
		return false, "", err
	}

	offset := (pageNum - 1) * defaultListInviteCodeCount
	invites, err := database.DiscordCampaignInvite{}.SelectPagination(guildID, defaultListInviteCodeCount, offset)
	if err != nil {
		return false, "", err
	}
	var (
		desc string
	)
	if len(invites) > 0 {
		for i, inv := range invites {
			num := offset + i + 1
			desc = fmt.Sprintf("%v\n%v.https://discord.gg/%v\nCampaign Name:`%v`,Campaign Source:`%v`\n", desc, num,
				inv.InviteCode, inv.CampaignName, inv.CampaignSource)
		}
	} else {
		desc = "No more invites."
	}
	// 返回是否有下一页
	return len(invites) == defaultListInviteCodeCount && (pageNum*defaultListInviteCodeCount) < int(count), desc, nil
}

func listInviteCodesPagination(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 快速响应，等待后续响应用户
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
	// 获取当前的页面
	p := strings.TrimPrefix(i.MessageComponentData().CustomID, listInvitePage)
	pageNum, err := strconv.ParseInt(p, 10, 64)
	if err != nil {
		log.Error(err)
		return
	}

	// 解析当前查询的页码
	hasNextPage, desc, err := getInviteCodesPagination(i.GuildID, int(pageNum))
	if err != nil {
		log.Error(err)
		return
	}
	offset := int((pageNum - 1) * defaultListInviteCodeCount)
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title:       "Latest invites",
				Description: desc,
			},
		},
		Components: &[]discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.Button{
						Style:    discordgo.PrimaryButton,
						Label:    "Prev page",
						CustomID: fmt.Sprintf("%v%v", listInvitePage, pageNum-1),
						Disabled: offset == 0,
					},
					&discordgo.Button{
						Style:    discordgo.PrimaryButton,
						Label:    "Next page",
						CustomID: fmt.Sprintf("%v%v", listInvitePage, pageNum+1),
						Disabled: !hasNextPage,
					},
				},
			},
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.Button{
						Style: discordgo.LinkButton,
						Label: "Check out invites and their performance",
						Emoji: discordgo.ComponentEmoji{
							Name: "🔍",
						},
						URL: "https://baidu.com",
					},
				},
			},
		},
	})
	if err != nil {
		log.Error(err)
	}
}

func submitInviteCodeCampaign(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 快速响应，等待后续响应用户
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

	channelID := strings.TrimPrefix(i.ModalSubmitData().CustomID, "create_invite_code:")
	campaignName := i.ModalSubmitData().Components[0].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
	campaignSource := i.ModalSubmitData().Components[1].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
	// 创建邀请码
	inv, err := s.ChannelInviteCreate(channelID, discordgo.Invite{
		MaxUses: 0,
		MaxAge:  0,
		Unique:  true,
	})
	if err != nil {
		log.Error(err)
		return
	}

	dgi := &database.DiscordGuildInvites{
		GuildID:     inv.Guild.ID,
		ChannelID:   inv.Channel.ID,
		InviterID:   inv.Inviter.ID,
		InviteCode:  inv.Code,
		CreatedAt:   inv.CreatedAt,
		MaxAge:      inv.MaxAge,
		UsedCount:   inv.Uses,
		MaxUseCount: inv.MaxUses,
		Revoked:     inv.Revoked,
		Temporary:   inv.Temporary,
		Unique:      inv.Unique,
		TargetType:  inv.TargetType,
	}
	if inv.TargetUser != nil {
		dgi.TargetUser = structs.Map(inv.TargetUser)
	}
	if inv.TargetApplication != nil {
		dgi.TargetApplication = structs.Map(inv.TargetApplication)
	}
	campaign := database.DiscordCampaignInvite{
		InviteCode:     inv.Code,
		CampaignName:   campaignName,
		CampaignSource: campaignSource,
		CreatedTime:    time.Now(),
	}
	err = database.CommunityPostgres.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&dgi).Error; err != nil {
			return err
		}
		return tx.Create(&campaign).Error
	})
	if err != nil {
		log.Error(err)
		return
	}
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title: "You just created a new permanent invite",
				Description: fmt.Sprintf("🆕https://discord.gg/%v\nCampaign Name:`%v`,Campaign Source:`%v`",
					campaign.InviteCode, campaign.CampaignName, campaign.CampaignSource),
			},
		},
	})
	if err != nil {
		log.Error(err)
	}
}

func resetInvitesCache(guilds []*discordgo.UserGuild) {
	ctx := context.TODO()
	for _, guild := range guilds {
		// 清空缓存的邀请数
		guildCacheKey := fmt.Sprintf("%v%v", guildInvitesCacheKeyPrefix, guild.ID)
		// 清空缓存的邀请者队列
		potentialInvitersCacheKey := fmt.Sprintf("%v%v", discordPotentialInviterCacheKeyPrefix, guild.ID)
		if err := cache.Redis.Del(ctx, guildCacheKey, potentialInvitersCacheKey).Err(); err != nil {
			log.Error(errors.WrapAndReport(err, "reset invites cache"))
		}
	}
	if err := initializeGuildInvites(session, guilds); err != nil {
		log.Fatal(err)
	}
}

// guildMemberAddEventHandler sent when a new user joins a guild.
func guildMemberAddEventHandler(s *discordgo.Session, a *discordgo.GuildMemberAdd) {
	log.Infof("guild member add event triggered")
	log.Debug("guild member add event triggered")
	if a.Member.User.Bot {
		return
	}
	dumpEvent(&database.DiscordEvents{
		GuildID:   a.GuildID,
		EventType: database.DiscordEventTypeGuildMemberAdd,
		Event:     structs.Map(a),
		EventTime: time.Now(),
	})

	pipe := newInviterMatchPipe(a.GuildID, a.User.ID)
	inviterMatchPipes <- pipe
	notification := <-pipe.inviterNotification
	if notification.Invite == nil {
		log.Warnf("guild %v member %v inviter not found", a.GuildID, a.User.ID)
	} else {
		millis := time.Since(notification.BufferTime).Milliseconds()
		if int64(millis) > 3000 {
			log.Warnf("Got inviter buffered %v seconds", millis)
		} else {
			log.Debugf("Got inviter buffered %v seconds", millis)
		}
	}
	// 尝试加入成员
	member := database.DiscordMember{
		GuildID:       a.GuildID,
		DiscordID:     a.Member.User.ID,
		Avatar:        a.Member.User.Avatar,
		Discriminator: a.Member.User.Discriminator,
		Username:      a.Member.User.Username,
		JoinedAt:      a.Member.JoinedAt,
		UpdatedAt:     time.Now(),
	}
	err := member.NewJoined()
	if err != nil {
		log.Error(err)
	}
	// 添加邀请关系
	invites := database.NewDiscordGuildMemberInvites(notification.Invite, a.Member)
	if err := invites.Create(); err != nil {
		log.Errorf("member add event:%v", err)
		return
	}

	inviterId := ""
	inviterName := ""
	inviteCode := ""
	if notification.Invite != nil {
		inviteCode = notification.Invite.Code
		inviterId = notification.Invite.Inviter.ID
		inviterName = notification.Invite.Inviter.Username
	}
	rawEvent, err := json.Marshal(a)
	if err == nil {
		pubDiscordEvent(&database.DiscordInviteEvent{
			GuildID:     a.GuildID,
			EventType:   database.DiscordEventTypeGuildMemberAdd,
			InviterId:   inviterId,
			InviterName: inviterName,
			InviteeId:   member.DiscordID,
			InviteeName: member.Username,
			InviteCode:  inviteCode,
			RawEvent:    string(rawEvent),
			EventTime:   time.Now().UTC().Format("2006-01-02 15:04:05.000 UTC"),
			TotalMember: cache.GetOrUpdateGuildInfo(s, inviteCode, a.GuildID),
		})
	} else {
		log.Errorf("failed to dump invite event: %v", err)
	}
	log.Debugf("created new discord invites: inviter %v, invitee %v", invites.InviterID, invites.InviteeID)
}

// guildMemberRemovedEventHandler sent when a user is removed from a guild (leave/kick/ban)
func guildMemberRemovedEventHandler(s *discordgo.Session, a *discordgo.GuildMemberRemove) {
	checkRemoveAppCommands(s, a)
	dumpEvent(&database.DiscordEvents{
		GuildID:   a.GuildID,
		EventType: database.DiscordEventTypeGuildMemberRemove,
		Event:     structs.Map(a),
		EventTime: time.Now(),
	})
	pubDiscordEvent(&database.DiscordMemberRemoveEvent{
		GuildID:   a.GuildID,
		EventType: database.DiscordEventTypeGuildMemberRemove,
		UserId:    a.Member.User.ID,
		UserName:  a.Member.User.Username,
		RawEvent:  common.MustGetJSONString(a),
		EventTime: time.Now().UTC().Format("2006-01-02 15:04:05.000 UTC"),
	})

	err := database.DiscordMember{}.UpdateLeave(a.GuildID, a.Member.User.ID)
	if err != nil {
		log.Error(err)
	}
	invites := database.NewDiscordGuildMemberInvites(nil, a.Member)
	err = invites.UpdateInviteeLeave()
	if err != nil {
		log.Errorf("update invites left:%v", err)
		return
	}
	log.Debugf("update guild member %v left", invites.InviteeID)
}

func showUserInvitesInfo(s *discordgo.Session, m *discordgo.MessageCreate) {
	users, err := database.DiscordGuildMemberInvites{}.UserTotalInvites(m.GuildID, m.Author.ID)
	if err != nil {
		log.Error(err)
		return
	}
	var (
		valid, total, fake, leave int64
	)
	if len(users) > 0 {
		valid = users[0].GetValidInvitesCount()
		total = users[0].InviteNum
		fake = users[0].Newbee
		leave = users[0].Leave
	}
	content := fmt.Sprintf("\n✅ **%v** joins\n👶 **%v** fakes (account too young)\n❌ %v leaves\n\nYou have %v invites ! :clap:",
		total, fake, leave, valid)
	_, err = s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{
			{
				Title:       m.Author.Username,
				Description: content,
				Author:      moffAuthor,
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "show user invites message"))
	}
}

func invitesCommandHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer logHandlerDuration("invites command", time.Now())
	// 快速响应，等待后续响应用户
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick response to invites command"))
		return
	}
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		respondMemberInvitesAmount(s, i)
		return
	}
	switch options[0].Name {
	case "me":
		respondMemberInvitesAmount(s, i)
	case "leaderboard":
		respondInvitesLeaderboard(s, i)
	}
}

func respondMemberInvitesAmount(s *discordgo.Session, i *discordgo.InteractionCreate) {
	users, err := database.DiscordGuildMemberInvites{}.UserTotalInvites(i.GuildID, i.Member.User.ID)
	if err != nil {
		log.Error(err)
		interactionResponseEditOnError(s, i)
		return
	}
	var (
		valid, total, fake, leave int64
	)
	if len(users) > 0 {
		valid = users[0].GetValidInvitesCount()
		total = users[0].InviteNum
		fake = users[0].Newbee
		leave = users[0].Leave
	}
	content := fmt.Sprintf("\n✅ **%v** joins\n👶 **%v** fakes (account too young)\n❌ %v leaves\n\nYou have %v invites ! :clap:",
		total, fake, leave, valid)
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title:       i.Member.User.Username,
				Description: content,
				Author:      moffAuthor,
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "invites response edit"))
	}
}

func respondInvitesLeaderboard(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 进行响应
	total, err := database.DiscordGuildMemberInvites{}.QueryTotalLeaderboard(i.GuildID, 0, 20)
	if err != nil {
		log.Error(err)
		interactionResponseEditOnError(s, i)
		return
	}

	content := "moff's invites leaderboard TOP 20.\n\n✅ Valid invites\n♾ All invites\n👶 Fake invites (account too young)\n❌Leave\n"
	for idx, l := range total {
		content += fmt.Sprintf("\n**%v** | <@%v> -> ✅∶**%v**  ♾∶**%v**  👶:**%v** ❌:**%v**\n", idx+1, l.InviterID,
			l.GetValidInvitesCount(), l.InviteNum, l.Newbee, l.Leave)
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
		log.Error(errors.WrapAndReport(err, "invites response edit"))
	}
}

func listDashboard(s *discordgo.Session, i *discordgo.InteractionCreate) {
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

	app, err := database.WhiteLabelingApps{}.SelectOne(i.GuildID)
	if err != nil {
		log.Error(err)
		return
	}
	if app == nil || app.CommunityDashboardURL == "" {
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{
				{
					Title:       "Oops! You don't have the access to the dashboard",
					Description: "The dashboard might be in maintenance.\nPlease get in touch with our support to know more: https://t.me/Darthclaire5",
				},
			},
		})
		if err != nil {
			log.Error(err)
		}
		return
	}
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title:       "👀Better understand your server",
				Description: "Gain valuable insights into your server's performance with our dashboard.\nMonitor key metrics and get a better understanding of how your server is performing.",
			},
		},
		Components: &[]discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.Button{
						Style: discordgo.LinkButton,
						Label: "Checkout Dashboard",
						URL:   app.CommunityDashboardURL,
					},
				},
			},
		},
	})
	if err != nil {
		log.Error(err)
	}
}
