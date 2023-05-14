package discord

import (
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/fatih/structs"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/internal/google"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strconv"
	"strings"
	"time"
)

func IsAdminPermission(perm int64) bool {
	return perm&discordgo.PermissionAdministrator == discordgo.PermissionAdministrator
}

func checkUserSnapshot(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	channelID := options[0].Value.(string)
	ctx := context.TODO()
	// 检查频道是否开启快照
	_, err := cache.Redis.HGet(ctx, fmt.Sprintf("%v:%v",
		discordChannelSnapshotSwitchKeyPrefix, i.GuildID), channelID).Result()
	if errors.Is(err, redis.Nil) {
		_ = respondEphemeralEmbedDesc(s, i, "`No snapshot enabled for given channel`")
		return
	}
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query snapshot switch"))
		_ = respondBotGoesWrong(s, i)
		return
	}

	if err != nil {
		log.Error(errors.WrapAndReport(err, "parse snapshot start millis"))
		_ = respondBotGoesWrong(s, i)
		return
	}

	// 检查是否存在用户出席
	joinedAt, err := cache.Redis.HGet(ctx, fmt.Sprintf("%v:%v:%v", discordVoiceChannelPresencesKeyPrefix,
		i.GuildID, channelID), i.Member.User.ID).Int64()
	if err != nil && !errors.Is(err, redis.Nil) {
		log.Error(errors.WrapAndReport(err, "query voice channel presence"))
		_ = respondBotGoesWrong(s, i)
		return
	}

	var (
		description string
	)
	if joinedAt == 0 {
		description = "You are NOT getting snapshotted. \n\n**Please quit and REJOIN the right channel.**"
	} else {
		description = "You are getting snapshotted!\n\n**PLEASE STAY THROUGH THE SNAPSHOT PERIOD, OR YOU WILL NOT GET WHITELISTED.**"
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       i.Member.User.Username,
					Description: description,
					Author:      moffAuthor,
				},
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "respond to snapshot check"))
	}
}

func calculateTextChannelSnapshot(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer logHandlerDuration("calculate text channel snapshot", time.Now())
	snapshotID := i.ModalSubmitData().Components[0].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).CustomID
	inputStr := i.ModalSubmitData().Components[0].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
	campaignName := i.ModalSubmitData().Components[1].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
	minimumWords, err := strconv.ParseInt(inputStr, 10, 64)
	if err != nil || minimumWords <= 0 {
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "No valid minimum words length input",
			},
		})
		if err != nil {
			log.Error(errors.WrapAndReport(err, "response interaction"))
		}
		return
	}
	// 快速响应，等待后续响应用户
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "response interaction"))
		return
	}

	ctx := context.TODO()
	// 设置快照锁
	snapshotLockCacheKey := fmt.Sprintf("%v:%v", discordChannelSnapshotStopLockKeyPrefix, snapshotID)
	locked, err := cache.Redis.SetNX(ctx, snapshotLockCacheKey, 1, time.Minute).Result()
	if err != nil {
		log.Error(errors.WrapAndReport(err, "set calculate lock"))
		interactionResponseEditOnError(s, i)
		return
	}
	if !locked {
		log.Warnf("discord snapshot %v lock not hold", snapshotID)
		_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{
				{
					Description: "Too many requests. Try again later:japanese_goblin: ",
					Author:      moffAuthor,
				},
			},
		})
		if err != nil {
			log.Error(errors.WrapAndReport(err, "response unlocked when set snapshot minimum words"))
		}
		return
	}
	defer func() {
		if err := cache.Redis.Del(ctx, snapshotLockCacheKey).Err(); err != nil {
			log.Error(errors.WrapAndReport(err, "delete snapshot lock"))
		}
	}()
	// 获取数据库的快照
	snapshot, err := database.DiscordSnapshot{}.SelectOne(snapshotID)
	if err != nil {
		log.Error(err)
		interactionResponseEditOnError(s, i)
		return
	}
	// 获取快照的频道
	channel, err := s.Channel(snapshot.ChannelID)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query snapshot channel"))
		interactionResponseEditOnError(s, i)
		return
	}
	snapshot.FinishedAt = database.PointerInt64(time.Now().UnixMilli())
	snapshot.FinishedBy = database.PointerString(i.Member.User.ID)
	snapshot.CampaignName = pointStr(campaignName)
	snapshot.MinimumWords = database.PointerInt64(minimumWords)
	participant, err := database.DiscordTextChannelPresence{}.CountSnapshotParticipant(snapshot.SnapshotID)
	if err != nil {
		log.Error(err)
		return
	}
	// 获取参与并分享至谷歌表单
	presences, err := database.DiscordTextChannelPresence{}.SelectSnapshotPresences(snapshot.SnapshotID)
	if err != nil {
		log.Error(err)
	}
	sheetURL, err := createGoogleSheetShareForTextChannelSnapshot(channel, presences)
	if err != nil {
		log.Error(err)
	}
	// 更新快照结束
	snapshot.TotalParticipantsNum = database.PointerInt(participant.TotalMember)
	snapshot.TotalMessageNum = database.PointerInt(participant.TotalMessage)
	snapshot.SheetURL = database.PointerString(sheetURL)
	if err := snapshot.UpdateFinished(); err != nil {
		log.Error(err)
		interactionResponseEditOnError(s, i)
		return
	}
	// 移除快照开关
	err = cache.Redis.HDel(ctx, fmt.Sprintf("%v:%v", discordChannelSnapshotSwitchKeyPrefix, i.GuildID),
		snapshot.ChannelID).Err()
	if err != nil {
		log.Error(errors.WrapAndReport(err, "delete text channel snapshot switch"))
		interactionResponseEditOnError(s, i)
		return
	}
	// 响应
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title: "`⛔`Snapshot is off!",
				Description: fmt.Sprintf("**Channel**:<#%v>\n**Started**:<t:%v:T>(<t:%v:R>)\n**Creater**:<@%v>\n**Terminated**:<t:%v:T>(<t:%v:R>)\n**Terminator**:<@%v>\n**Participants**:`%v`\n**Messages**:`%v`",
					channel.ID, *snapshot.CreatedAt/1000, *snapshot.CreatedAt/1000, *snapshot.CreatedBy,
					*snapshot.FinishedAt/1000, *snapshot.FinishedAt/1000, *snapshot.FinishedBy,
					participant.TotalMember, participant.TotalMessage),
			},
		},
		Components: snapshot.GoogleSheetComponent(),
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "response text channel snapshot information"))
		return
	}
	log.Debugf("Snapshot for text channel %v %v succeeded", channel.Name, channel.ID)
}

func createGoogleSheetShareForTextChannelSnapshot(channel *discordgo.Channel, presences []*database.SnapshotPresence) (string, error) {
	if len(presences) == 0 {
		return "", nil
	}
	sheetTitle := fmt.Sprintf("%v %v Snapshots", time.Now().Format("2006-01-02"), channel.Name)
	client := google.NewClients()
	spreadsheet, err := client.CreateSpreadsheet(sheetTitle)
	if err != nil {
		return "", err
	}
	if err := client.ShareFileToAnyReader(spreadsheet.SpreadsheetId); err != nil {
		return "", err
	}
	var (
		imgTitleSize         = 1
		appendUserEnteredReq = &google.SpreadsheetPushRequest{
			SpreadsheetId: spreadsheet.SpreadsheetId,
			Range:         "Sheet1",
			Values: [][]interface{}{
				{"Discord ID", "Time", "Text", "Image1"},
			},
		}
		updateRawDiscordIDReq = &google.SpreadsheetPushRequest{
			SpreadsheetId: spreadsheet.SpreadsheetId,
			Range:         "Sheet1",
			Values: [][]interface{}{
				{"Discord ID"},
			},
		}
	)

	for r, presence := range presences {
		if r != 0 {
			// 空一行
			appendUserEnteredReq.Values = append(appendUserEnteredReq.Values, []interface{}{})
			updateRawDiscordIDReq.Values = append(updateRawDiscordIDReq.Values, []interface{}{})
		}

		for i, msg := range presence.Messages {
			var row []interface{}
			if i == 0 {
				row = append(row, presence.DiscordID, time.UnixMilli(presence.Messages[0].CreatedAt).
					Format("2006.01.02 15:04:05"), msg.Text)
				updateRawDiscordIDReq.Values = append(updateRawDiscordIDReq.Values, []interface{}{presence.DiscordID})
			} else {
				row = append(row, "", time.UnixMilli(msg.CreatedAt).Format("2006.01.02 15:04:05"),
					msg.Text)
				updateRawDiscordIDReq.Values = append(updateRawDiscordIDReq.Values, []interface{}{""})
			}
			if msg.Images != nil {
				// 设置图片
				for i, img := range *msg.Images {
					// 扩展标题行
					if i+1 > imgTitleSize {
						imgTitleSize++
						appendUserEnteredReq.Values[0] = append(appendUserEnteredReq.Values[0], fmt.Sprintf("Image%v", imgTitleSize))
					}
					// 设置消息图片
					row = append(row, fmt.Sprintf("=IMAGE(\"%v\")", img))
				}
			}
			// 设置表单行数据
			appendUserEnteredReq.Values = append(appendUserEnteredReq.Values, row)
		}
	}
	if err := client.AppendUserEnterToSpreadsheet(appendUserEnteredReq); err != nil {
		return "", err
	}
	updateRawDiscordIDReq.Range = fmt.Sprintf("A1:A%v", len(updateRawDiscordIDReq.Values))
	if err := client.UpdateRawToSpreadsheet(updateRawDiscordIDReq); err != nil {
		return "", err
	}
	return spreadsheet.SpreadsheetUrl, nil
}

func calculateVoiceChannelSnapshot(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer logHandlerDuration("calculate voice channel snapshot", time.Now())
	data := i.ModalSubmitData()
	snapshotID := data.Components[0].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).CustomID
	inputStr := data.Components[0].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
	campaignName := data.Components[1].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
	campaignID := data.Components[2].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
	snapshotSeconds, err := strconv.ParseInt(inputStr, 10, 64)
	if err != nil || snapshotSeconds <= 0 {
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "No valid seconds input",
			},
		})
		if err != nil {
			log.Error(errors.WrapAndReport(err, "response interaction"))
		}
		return
	}

	// 快速响应，等待后续响应用户
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "response interaction"))
		return
	}
	// 检查campaign
	var (
		campaign *database.Campaigns
	)

	if campaignID != "" {
		// 检查campaign是否存在与campaign的归属
		campaign, err = database.Campaigns{}.SelectOne(campaignID)
		if err != nil {
			log.Error(err)
			interactionResponseEditOnError(s, i)
			return
		}
		if campaign == nil || campaign.Status != "reviewed" {
			interactionResponseEditOnMsg(s, i, fmt.Sprintf("Campaign from %v not found", campaignID))
			return
		}
		app, err := database.WhiteLabelingApps{}.SelectOne(i.GuildID)
		if err != nil {
			log.Error(err)
			interactionResponseEditOnError(s, i)
			return
		}
		if app == nil {
			interactionResponseEditOnMsg(s, i, "We don't know who are you...")
			return
		}
		if app.AppID != campaign.AppID {
			interactionResponseEditOnMsg(s, i, "Cannot link this campaign")
			return
		}
	}

	// 设置快照锁
	ctx := context.TODO()
	snapshotLockCacheKey := fmt.Sprintf("%v:%v", discordChannelSnapshotStopLockKeyPrefix, snapshotID)
	locked, err := cache.Redis.SetNX(ctx, snapshotLockCacheKey, 1, time.Minute).Result()
	if err != nil {
		log.Error(errors.WrapAndReport(err, "set calculate lock"))
		interactionResponseEditOnError(s, i)
		return
	}
	if !locked {
		log.Warnf("discord snapshot %v lock not hold", snapshotID)
		_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{
				{
					Description: "Too many requests. Try again later:japanese_goblin: ",
					Author:      moffAuthor,
				},
			},
		})
		if err != nil {
			log.Error(errors.WrapAndReport(err, "response unlocked when set snapshot minimum words"))
		}
		return
	}
	defer func() {
		if err := cache.Redis.Del(ctx, snapshotLockCacheKey).Err(); err != nil {
			log.Error(errors.WrapAndReport(err, "delete snapshot lock"))
		}
	}()

	// 获取数据库的快照
	startCalcSnapshots := time.Now()
	channelSnapshot, err := database.DiscordSnapshot{}.SelectOne(snapshotID)
	if err != nil {
		log.Error(err)
		interactionResponseEditOnError(s, i)
		return
	}
	channelSnapshot.SnapshotSeconds = database.PointerInt64(snapshotSeconds)
	// 获取快照频道
	channel, err := s.Channel(channelSnapshot.ChannelID)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query snapshot channel"))
		interactionResponseEditOnError(s, i)
		return
	}
	// 计算所有的出席
	snapshots, err := clearSnapshotParticipantsPresence(ctx, channelSnapshot)
	if err != nil {
		log.Error(err)
		interactionResponseEditOnError(s, i)
		return
	}
	var (
		snapshotMillis = snapshotSeconds * 1000
		whitelist      []interface{}
		whitelisted    = make(map[string]int64)
	)
	for member, presenceMillis := range snapshots {
		if presenceMillis > snapshotMillis {
			whitelist = append(whitelist, member)
			whitelisted[member] = presenceMillis / 1000
		}
	}
	log.Debugf("Calc voice channel snapshots:%v", time.Since(startCalcSnapshots))
	// 保存谷歌表单
	startSaveGoogleSheet := time.Now()
	sheetURL, err := createGoogleSheetShareForVoiceChannelSnapshot(channel, channelSnapshot, whitelisted)
	if err != nil {
		log.Error(err)
	}
	log.Debugf("Save voice channel snapshots to google sheet:%v", time.Since(startSaveGoogleSheet))
	channelSnapshot.Whitelist = whitelist
	channelSnapshot.FinishedAt = database.PointerInt64(time.Now().UnixMilli())
	channelSnapshot.FinishedBy = database.PointerString(i.Member.User.ID)
	channelSnapshot.TotalParticipantsNum = database.PointerInt(len(snapshots))
	channelSnapshot.ValidParticipantsNum = database.PointerInt(len(whitelist))
	channelSnapshot.SheetURL = database.PointerString(sheetURL)
	channelSnapshot.SnapshotSeconds = database.PointerInt64(snapshotSeconds)
	channelSnapshot.CampaignID = pointStr(campaignID)
	channelSnapshot.CampaignName = pointStr(campaignName)
	if err := channelSnapshot.UpdateFinished(); err != nil {
		log.Error(err)
		interactionResponseEditOnError(s, i)
		return
	}
	writeCampaignWhitelists(campaign, whitelist)
	// 移除快照相关缓存
	snapshotsCacheKey := fmt.Sprintf("%v:%v:%v", discordVoiceChannelSnapshotsKeyPrefix,
		channelSnapshot.GuildID, channelSnapshot.ChannelID)
	snapshotSwitchKey := fmt.Sprintf("%v:%v", discordChannelSnapshotSwitchKeyPrefix, i.GuildID)
	_, err = cache.Redis.TxPipelined(ctx, func(pipeliner redis.Pipeliner) error {
		// 删除频道快照开关
		if err := pipeliner.HDel(ctx, snapshotSwitchKey, channelSnapshot.ChannelID).Err(); err != nil {
			return errors.WrapAndReport(err, "delete snapshot lock")
		}
		// 删除频道中的快照列表
		if err := pipeliner.Del(ctx, snapshotsCacheKey).Err(); err != nil {
			return errors.WrapAndReport(err, "delete snapshot lock")
		}
		if err := pipeliner.Del(ctx, snapshotLockCacheKey).Err(); err != nil {
			return errors.WrapAndReport(err, "delete snapshot lock")
		}
		return nil
	})
	if err != nil {
		log.Errorf("delete snapshot cache when stop:%v", err)
		interactionResponseEditOnError(s, i)
		return
	}
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title: "`⛔`Snapshot is off!",
				Description: fmt.Sprintf("**Channel**:<#%v>\n**Started**:<t:%v:T>(<t:%v:R>)\n**Creater**:<@%v>\n**Terminated**:<t:%v:T>(<t:%v:R>)\n**Terminator**:<@%v>\n**Participants**:`%v`\n**Filtered participants**: `%v` (not less than `%v` seconds)",
					channel.ID, *channelSnapshot.CreatedAt/1000, *channelSnapshot.CreatedAt/1000, *channelSnapshot.CreatedBy,
					*channelSnapshot.FinishedAt/1000, *channelSnapshot.FinishedAt/1000, *channelSnapshot.FinishedBy,
					len(snapshots), len(whitelist), snapshotSeconds),
			},
		},
		Components: channelSnapshot.GoogleSheetComponent(),
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "response snapshot information"))
		return
	}
	log.Debugf("Snapshot for voice channel %v %v succeeded", channel.Name, channel.ID)
}

func writeCampaignWhitelists(campaign *database.Campaigns, whitelists []interface{}) {
	if campaign == nil {
		return
	}
	whitelistID := database.FindCampaignWhitelistID(campaign.Required)
	if whitelistID == "" {
		log.Warnf("Campaign %v whitelist not found", campaign.CampaignID)
		return
	}
	err := database.PublicPostgres.Transaction(func(tx *gorm.DB) error {
		var (
			batch     = make([]string, 0)
			batchSize = 2000
		)
		for i, did := range whitelists {
			batch = append(batch, did.(string))
			if len(batch) < batchSize && i < len(whitelists)-1 {
				continue
			}
			// 按批写入
			err := database.Whitelist{}.WriteDiscordIds(tx, whitelistID, batch)
			if err != nil {
				return err
			}
			batch = make([]string, 0)
		}
		return nil
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "write twitter space whitelists"))
	}
}

func createGoogleSheetShareForVoiceChannelSnapshot(channel *discordgo.Channel,
	snapshot *database.DiscordSnapshot, whitelist map[string]int64) (string, error) {
	if len(whitelist) == 0 {
		return "", nil
	}
	sheetTitle := fmt.Sprintf("%v %v %vS Snapshots", time.Now().Format("2006-01-02"),
		channel.Name, snapshot.SnapshotSeconds)
	client := google.NewClients()
	spreadsheet, err := client.CreateSpreadsheet(sheetTitle)
	if err != nil {
		return "", err
	}
	if err := client.ShareFileToAnyReader(spreadsheet.SpreadsheetId); err != nil {
		return "", err
	}
	appendReq := &google.SpreadsheetPushRequest{
		SpreadsheetId: spreadsheet.SpreadsheetId,
		Range:         "Sheet1",
		Values: [][]interface{}{
			{"Discord ID", "Snapshot Seconds"},
		},
	}
	for member, sec := range whitelist {
		appendReq.Values = append(appendReq.Values, []interface{}{member, sec})
	}
	if err := client.AppendRawToSpreadsheet(appendReq); err != nil {
		return "", err
	}
	return spreadsheet.SpreadsheetUrl, nil
}

func clearSnapshotParticipantsPresence(ctx context.Context, snapshot *database.DiscordSnapshot) (map[string]int64, error) {
	// 检查出席中的房间用户
	presencesCache, err := cache.Redis.HGetAll(ctx, fmt.Sprintf("%v:%v:%v", discordVoiceChannelPresencesKeyPrefix,
		snapshot.GuildID, snapshot.ChannelID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, errors.WrapAndReport(err, "query snapshot presences")
	}
	// 检查历史用户快照时间
	snapshotsCacheKey := fmt.Sprintf("%v:%v:%v", discordVoiceChannelSnapshotsKeyPrefix,
		snapshot.GuildID, snapshot.ChannelID)
	snapshotsCache, err := cache.Redis.HGetAll(ctx, snapshotsCacheKey).Result()
	if err != nil {
		return nil, errors.WrapAndReport(err, "query all presence snapshots")
	}

	// 合并出席时长
	var (
		snapshots = make(map[string]int64)
	)
	for member, presenceTimeStr := range presencesCache {
		presenceTime, err := strconv.ParseInt(presenceTimeStr, 10, 64)
		if err != nil {
			log.Error(errors.WrapAndReport(err, "parse presence participant time"))
			continue
		}
		if presenceTime > *snapshot.CreatedAt {
			snapshots[member] = time.Now().UnixMilli() - presenceTime
		} else {
			snapshots[member] = time.Now().UnixMilli() - *snapshot.CreatedAt
		}
	}
	for member, millis := range snapshotsCache {
		presenceMillis, err := strconv.ParseInt(millis, 10, 64)
		if err != nil {
			log.Error(errors.WrapAndReport(err, "parse presence duration"))
			continue
		}
		snapshots[member] = snapshots[member] + presenceMillis
	}
	return snapshots, nil
}

func stopChannelSnapshotFromInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 快速响应，等待后续响应用户
	//err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
	//	Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	//	Data: &discordgo.InteractionResponseData{
	//		Flags: discordgo.MessageFlagsEphemeral,
	//	},
	//})
	//if err != nil {
	//	log.Error(errors.WrapAndReport(err, "response interaction"))
	//	return
	//}
	// 获取当前的页面
	channelID := strings.TrimPrefix(i.MessageComponentData().CustomID, stopSnapshot)
	stopDiscordChannelSnapshot(s, i, channelID)
}

func stopChannelSnapshotFromCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
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
	defer logHandlerDuration("stop channel snapshot", time.Now())
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	channelID := options[0].Value.(string)
	stopDiscordChannelSnapshot(s, i, channelID)
}

func stopDiscordChannelSnapshot(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	channel, err := s.Channel(channelID)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query channel"))
		return
	}
	// 查找当前快照开关
	ctx := context.TODO()
	snapshotPointStr, err := cache.Redis.HGet(ctx, fmt.Sprintf("%v:%v",
		discordChannelSnapshotSwitchKeyPrefix, i.GuildID), channelID).Result()
	if errors.Is(err, redis.Nil) {
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: fmt.Sprintf("No snapshot started for channel `%v`", channel.Name),
			},
		})
		if err != nil {
			log.Error(errors.WrapAndReport(err, "response interaction"))
		}
		return
	}
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query snapshot switch"))
		return
	}
	snapshotPoints := strings.Split(snapshotPointStr, "&")
	var (
		customID, title string
		components      []discordgo.MessageComponent
	)
	if channel.Type == discordgo.ChannelTypeGuildVoice || channel.Type == discordgo.ChannelTypeGuildStageVoice {
		customID = "snapshot_minimum_duration"
		title = "Snapshot minimum duration"
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    snapshotPoints[1],
						Label:       "Minimum Seconds",
						Style:       discordgo.TextInputShort,
						Placeholder: "Minimum seconds to consider a valid entry",
						Required:    true,
						MaxLength:   10,
						MinLength:   1,
					},
				},
			},
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    customSnapshotCampaignName,
						Label:       "Event name",
						Style:       discordgo.TextInputShort,
						Placeholder: "e.g. Townhall AMA",
						Required:    true,
						MaxLength:   200,
					},
				},
			},
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    customSnapshotCampaignID,
						Label:       "Event ID",
						Style:       discordgo.TextInputShort,
						Placeholder: "Optional,automatically write whitelist to",
						Required:    false,
						MaxLength:   100,
					},
				},
			},
		}

	} else {
		customID = "snapshot_minimum_words"
		title = "Snapshot minimum words"
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    snapshotPoints[1],
						Label:       "Minimum words",
						Style:       discordgo.TextInputShort,
						Placeholder: "Minimum words to consider a valid entry",
						Required:    true,
						MaxLength:   10,
						MinLength:   1,
					},
				},
			},
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    customSnapshotCampaignName,
						Label:       "Event name",
						Style:       discordgo.TextInputShort,
						Placeholder: "e.g. Townhall AMA",
						Required:    true,
						MaxLength:   200,
					},
				},
			},
		}
	}
	// 让用户设置快照筛选时长
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			Flags:      discordgo.MessageFlagsEphemeral,
			CustomID:   customID,
			Title:      title,
			Components: components,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "response user snapshot input modal"))
		return
	}
	log.Debugf("Snapshot input modal responded for channel %v %v", channel.Name, channel.ID)
}

func listChannelSnapshots(s *discordgo.Session, i *discordgo.InteractionCreate) {
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
	snapshots, err := database.DiscordSnapshot{}.SelectLatest(20, i.GuildID)
	if err != nil {
		log.Error(err)
		return
	}
	var (
		title, desc string
	)
	if len(snapshots) == 0 {
		title = "There are no snapshots for now.."
	} else {
		title = fmt.Sprintf("Latest %v discord snapshots", len(snapshots))
	}
	for i, snapshot := range snapshots {
		var content string
		if snapshot.FinishedAt != nil {
			if snapshot.SheetURL != nil {
				content = fmt.Sprintf("\n\n**%v.Channel**:<#%v>　**Status:**`🟥Finished`　**[Participants](%v)**\n　**Start Time**: <t:%v>\n　**End Time**:<t:%v>",
					i+1, snapshot.ChannelID, snapshot.SheetURL, *snapshot.CreatedAt/1000, *snapshot.FinishedAt/1000)
			} else {
				content = fmt.Sprintf("\n\n**%v.Channel**:<#%v>　**Status:**`🟥Finished`\n　**Start Time**: <t:%v>\n　**End Time**:<t:%v>",
					i+1, snapshot.ChannelID, *snapshot.CreatedAt/1000, *snapshot.FinishedAt/1000)
			}
		} else {
			content = fmt.Sprintf("\n\n**%v.Channel**:<#%v>　**Status:**`✅Ongoing`\n　**Start Time**: <t:%v>", i+1,
				snapshot.ChannelID, *snapshot.CreatedAt/1000)
		}

		// 检查是否字符超限
		if len(desc+content) > 4096 {
			break
		}
		desc += content
	}
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       title,
					Description: desc,
				},
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick respond to list channel snapshots"))
	}
}

func startChannelSnapshot(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !IsAdminPermission(i.Member.Permissions) {
		respondSnapshotError(s, i, "Not allowed:thinking: ")
		return
	}
	defer logHandlerDuration("stop channel snapshot", time.Now())
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
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

	// 获取快照的频道
	channel, err := s.Channel(options[0].Value.(string))
	if err != nil {
		log.Error(errors.WrapAndReport(err, "query snapshot channel"))
		return
	}
	ctx := context.TODO()
	// 检查频道快照，是否已开启
	snapshotSwitchCacheKey := fmt.Sprintf("%v:%v", discordChannelSnapshotSwitchKeyPrefix, i.GuildID)
	snapshotPointStr, err := cache.Redis.HGet(ctx, snapshotSwitchCacheKey, channel.ID).Result()
	if !errors.Is(err, redis.Nil) && err != nil {
		log.Error(errors.WrapAndReport(err, "query snapshot channel cache"))
		return
	}
	if snapshotPointStr != "" {
		snapshotPoints := strings.Split(snapshotPointStr, "&")
		startMillis, _ := strconv.ParseInt(snapshotPoints[0], 10, 64)
		snapshot, err := database.DiscordSnapshot{}.SelectOne(snapshotPoints[1])
		if err != nil {
			log.Error(err)
			return
		}
		if snapshot == nil {
			log.Errorf("snapshot %v not found", snapshotPoints[1])
			return
		}
		startSeconds := startMillis / 1000
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{
				{
					Title: "`🔴`Snapshot is on!",
					Description: fmt.Sprintf("**Channel**:<#%v>\n**Started**:<t:%v:T>(<t:%v:R>)\n**Creater**:<@%v>",
						channel.ID, startSeconds, startSeconds, *snapshot.CreatedBy),
					Footer: &discordgo.MessageEmbedFooter{
						Text: "Is this panel stuck?Try using \"/start-snapshot\" again to recover recording panel",
					},
				},
			},
			Components: &[]discordgo.MessageComponent{
				&discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						&discordgo.Button{
							Style: discordgo.DangerButton,
							Label: "Stop Snapshot",
							Emoji: discordgo.ComponentEmoji{
								Name: "◻️",
							},
							CustomID: fmt.Sprintf("%v%v", stopSnapshot, snapshot.ChannelID),
						},
					},
				},
			},
		})
		if err != nil {
			log.Error(errors.WrapAndReport(err, "response snapshot already started"))
		}
		return
	}
	var snapshotType database.DiscordSnapshotType
	switch channel.Type {
	case discordgo.ChannelTypeGuildVoice, discordgo.ChannelTypeGuildStageVoice:
		snapshotType = database.DiscordSnapshotTypeVoice
	default:
		snapshotType = database.DiscordSnapshotTypeText
	}
	// 快照锁
	startSnapshotLockCacheKey := fmt.Sprintf("%v:%v:%v", discordChannelSnapshotStartLockKeyPrefix, i.GuildID, channel.ID)
	locked, err := cache.Redis.SetNX(ctx, startSnapshotLockCacheKey, 1, time.Second*30).Result()
	if err != nil {
		log.Error(errors.WrapAndReport(err, "set snapshot lock"))
		return
	}
	if !locked {
		respondEditSnapshotError(s, i, "Try again later please!")
		return
	}
	defer func() {
		if err := cache.Redis.Del(ctx, startSnapshotLockCacheKey).Err(); err != nil {
			log.Error(errors.WrapAndReport(err, "delete snapshot lock cache"))
		}
	}()
	// 保存快照
	snapshots := database.DiscordSnapshot{
		SnapshotID: common.NewCutUUIDString(),
		GuildID:    i.GuildID,
		ChannelID:  channel.ID,
		Type:       snapshotType,
		CreatedBy:  database.PointerString(i.Member.User.ID),
		CreatedAt:  database.PointerInt64(time.Now().UnixMilli()),
		UpdatedAt:  time.Now(),
	}
	if err := snapshots.Create(); err != nil {
		log.Error(err)
		return
	}
	// 设置快照开关
	err = cache.Redis.HSet(ctx, snapshotSwitchCacheKey, channel.ID, fmt.Sprintf("%v&%v",
		time.Now().UnixMilli(), snapshots.SnapshotID)).Err()
	if err != nil {
		log.Error(errors.WrapAndReport(err, "cache snapshot switch"))
		return
	}
	// 响应成功
	now := time.Now().Unix()
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title: "`🔴`Snapshot is on!",
				Description: fmt.Sprintf("**Channel**:<#%v>\n**Started**:<t:%v:T>(<t:%v:R>)\n**Creater**:<@%v>",
					channel.ID, now, now, *snapshots.CreatedBy),
				Footer: &discordgo.MessageEmbedFooter{
					Text: "Is this panel stuck?Try using \"/start-snapshot\" again to recover recording panel",
				},
			},
		},
		Components: &[]discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.Button{
						Style: discordgo.DangerButton,
						Label: "Stop Snapshot",
						Emoji: discordgo.ComponentEmoji{
							Name: "◻️",
						},
						CustomID: fmt.Sprintf("%v%v", stopSnapshot, snapshots.ChannelID),
					},
				},
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "response channel snapshot enabled"))
		return
	}
	log.Infof("Snapshot for channel %v %v  started", channel.Name, channel.ID)
}

const (
	discordChannelSnapshotSwitchKeyPrefix    = "discord_channel_snapshot_switch"
	discordChannelSnapshotStartLockKeyPrefix = "discord_channel_snapshot_start_lock"
	discordChannelSnapshotStopLockKeyPrefix  = "discord_channel_snapshot_stop_lock"
	discordVoiceChannelPresencesKeyPrefix    = "discord_voice_channel_presences"
	discordVoiceChannelSnapshotsKeyPrefix    = "discord_voice_channel_snapshots"
)

type VoiceStateUpdateAddTime struct {
	VoiceEventData *discordgo.VoiceState
	Duration       int64 `json:"duration"`
}

func voiceChannelMemberUpdate(s *discordgo.Session, u *discordgo.VoiceStateUpdate) {
	if u.UserID == "" {
		log.Warnf("Receive empty user id state from guild %v channel %v", u.GuildID, u.ChannelID)
		return
	}
	dumpEvent(&database.DiscordEvents{
		GuildID:   u.GuildID,
		EventType: database.DiscordEventTypeVoiceStateUpdate,
		Event:     structs.Map(u),
		EventTime: time.Now(),
	})
	ctx := context.TODO()
	// move 的时候SessionID 不会变
	var joinVoiceCacheKey string

	// 离开语音房间: leave or move
	if u.BeforeUpdate != nil {
		joinVoiceCacheKey = fmt.Sprintf("voice:%s-%s-%s-%s", u.BeforeUpdate.GuildID, u.BeforeUpdate.ChannelID, u.BeforeUpdate.UserID, u.BeforeUpdate.SessionID)
		log.Debugf("user %v leave guild %v voice channel %v", u.UserID, u.BeforeUpdate.GuildID, u.BeforeUpdate.ChannelID)
		updateUserLeaveVoiceChannel(u)

		if joinUnixSecond, err := cache.Redis.Get(ctx, joinVoiceCacheKey).Result(); err == nil {
			// joinVoiceCacheKey 不存在或者失败都不处理
			if joinSecondInt, err := strconv.ParseInt(joinUnixSecond, 10, 64); err == nil {
				pubDiscordEvent(&database.DiscordActiveEvent{
					GuildID:     u.BeforeUpdate.GuildID,
					EventType:   database.DiscordEventTypeVoiceStateUpdate,
					UserId:      u.BeforeUpdate.UserID,
					UserName:    cache.GetOrUpdateUserInfo(s, u.BeforeUpdate.UserID),
					ChannelId:   u.BeforeUpdate.ChannelID,
					ChannelName: cache.GetOrUpdateChannelInfo(s, u.BeforeUpdate.ChannelID),
					RawEvent:    common.MustGetJSONString(VoiceStateUpdateAddTime{u.BeforeUpdate, time.Now().Unix() - joinSecondInt}),
					EventTime:   time.Now().UTC().Format("2006-01-02 15:04:05.000 UTC"),
				})
				cache.Redis.Del(ctx, joinVoiceCacheKey)
			}
		}
	}
	// 加入语音房间
	if u.ChannelID != "" {
		joinVoiceCacheKey = fmt.Sprintf("voice:%s-%s-%s-%s", u.GuildID, u.ChannelID, u.UserID, u.SessionID)

		log.Debugf("user %v join guild %v voice channel %v", u.UserID, u.GuildID, u.ChannelID)
		updateUserJoinVoiceChannel(u)
		cache.Redis.Set(ctx, joinVoiceCacheKey, time.Now().Unix(), time.Hour*24)
		//joinUnixSecond, err := cache.Redis.Get(ctx, joinVoiceCacheKey).Result()
		//fmt.Println("set seccess", joinUnixSecond, err)
	}
}

func updateUserLeaveVoiceChannel(u *discordgo.VoiceStateUpdate) {
	now := time.Now().UnixMilli()
	presence := &database.DiscordVoiceChannelPresence{
		GuildID:   u.BeforeUpdate.GuildID,
		ChannelID: u.BeforeUpdate.ChannelID,
		DiscordID: u.UserID,
		LeftAt:    &now,
	}
	NewSingleWriteStorageEngine().pipeline <- func() {
		var (
			maxTry = 3
		)
		for i := 0; i < maxTry; i++ {
			if err := presence.Leave(); err != nil {
				log.Error(err)
				time.Sleep(time.Millisecond * 300)
				continue
			}
			return
		}
	}
	try2RemoveUserVoiceChannelPresenceFromCache(presence)
}

func try2RemoveUserVoiceChannelPresenceFromCache(presence *database.DiscordVoiceChannelPresence) {
	var (
		maxTry = 3
	)
	for i := 0; i < maxTry; i++ {
		if err := removeUserVoiceChannelPresenceFromCache(presence); err != nil {
			log.Error(err)
			continue
		}
		return
	}
	log.Error(errors.ErrorfAndReport("Fail to update cache for member %v leave discord %v voice channel %v",
		presence.DiscordID, presence.GuildID, presence.ChannelID))
}

func removeUserVoiceChannelPresenceFromCache(presence *database.DiscordVoiceChannelPresence) error {
	ctx := context.TODO()
	// 检查当前频道是否存在快照,
	// 快照开关值：开始时间戳&32位数据库记录id
	snapshotSwitchKey := fmt.Sprintf("%v:%v", discordChannelSnapshotSwitchKeyPrefix, presence.GuildID)
	snapshotPointStr, err := cache.Redis.HGet(ctx, snapshotSwitchKey, presence.ChannelID).Result()
	if errors.Is(err, redis.Nil) {
		// 未开启快照，直接移除
		err := cache.Redis.HDel(ctx, fmt.Sprintf("%v:%v:%v", discordVoiceChannelPresencesKeyPrefix,
			presence.GuildID, presence.ChannelID), presence.DiscordID).Err()
		return errors.WrapAndReport(err, "delete voice channel presence cache")
	}
	if err != nil {
		return errors.WrapAndReport(err, "query discord voice channel snapshot switch")
	}

	// 当前存在快照，添加用户的快照时长
	snapshotPoints := strings.Split(snapshotPointStr, "&")
	snapshotStartTime, err := strconv.ParseInt(snapshotPoints[0], 10, 64)
	if err != nil {
		return errors.WrapAndReport(err, "parse snapshot start time")
	}
	// 获取用户进入房间时间
	joinedAt, err := cache.Redis.HGet(ctx, fmt.Sprintf("%v:%v:%v", discordVoiceChannelPresencesKeyPrefix,
		presence.GuildID, presence.ChannelID), presence.DiscordID).Int64()
	if err != nil && !errors.Is(err, redis.Nil) {
		return errors.WrapAndReport(err, "query user presence time")
	}
	// 没有进入记录，直接返回
	if joinedAt == 0 {
		log.Errorf("No cache for voice channel %v member %v joined time", presence.ChannelID, presence.DiscordID)
		return nil
	}

	// 有用户加入房间时间，计算用户快照时长
	var memberSnapshotTime int64
	if joinedAt > snapshotStartTime {
		memberSnapshotTime = time.Now().UnixMilli() - joinedAt
	} else {
		memberSnapshotTime = time.Now().UnixMilli() - snapshotStartTime
	}

	return cacheUserVoiceChannelPresenceSnapshot(ctx, presence, memberSnapshotTime)
}

func cacheUserVoiceChannelPresenceSnapshot(ctx context.Context, presence *database.DiscordVoiceChannelPresence, snapshotTime int64) error {
	pipeline := cache.Redis.Pipeline()
	pipeline.HDel(ctx, fmt.Sprintf("%v:%v:%v", discordVoiceChannelPresencesKeyPrefix, presence.GuildID, presence.ChannelID),
		presence.DiscordID)
	pipeline.HIncrBy(ctx, fmt.Sprintf("%v:%v:%v", discordVoiceChannelSnapshotsKeyPrefix, presence.GuildID, presence.ChannelID),
		presence.DiscordID, snapshotTime)

	var (
		maxTry = 3
	)
	for i := 0; i < maxTry; i++ {
		if _, err := pipeline.Exec(ctx); err != nil {
			log.Error(errors.WrapAndReport(err, "delete cache when voice channel member leave"))
			continue
		}
		return nil
	}
	return errors.ErrorfAndReport("failed to snapshot member %v presence when leave voice channel %v",
		presence.DiscordID, presence.ChannelID)
}

func updateUserJoinVoiceChannel(u *discordgo.VoiceStateUpdate) {
	presence := &database.DiscordVoiceChannelPresence{
		GuildID:   u.GuildID,
		ChannelID: u.ChannelID,
		DiscordID: u.UserID,
		JoinedAt:  time.Now().UnixMilli(),
	}
	NewSingleWriteStorageEngine().pipeline <- func() {
		var (
			maxTry = 3
		)
		for i := 0; i < maxTry; i++ {
			if err := presence.Join(); err != nil {
				log.Error(err)
				time.Sleep(time.Millisecond * 300)
				continue
			}
			return
		}
	}
	cacheUserVoiceChannelPresence(presence)
}

func cacheUserVoiceChannelPresence(presence *database.DiscordVoiceChannelPresence) {
	var (
		maxTry      = 3
		ctx         = context.TODO()
		presenceKey = fmt.Sprintf("%v:%v:%v", discordVoiceChannelPresencesKeyPrefix, presence.GuildID, presence.ChannelID)
	)
	for i := 0; i < maxTry; i++ {
		err := cache.Redis.HSet(ctx, presenceKey, presence.DiscordID, presence.JoinedAt).Err()
		if err != nil {
			log.Error(errors.WrapAndReport(err, "cache voice channel presence"))
			continue
		}
		return
	}
	log.Error(errors.ErrorfAndReport("Fail to update cache for member %v join discord %v voice channel %v",
		presence.DiscordID, presence.GuildID, presence.ChannelID))
}
