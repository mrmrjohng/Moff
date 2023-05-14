package discord

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/anthonynsimon/bild/blur"
	"github.com/bwmarrin/discordgo"
	"github.com/go-redis/redis/v8"
	"github.com/heyuanyou/captcha"
	"image/color"
	"image/jpeg"
	"io"
	"math/rand"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/internal/fonts"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strings"
	"time"
)

const (
	casinoBotVerificationCacheKey = "casino_bot_verification:"
	casinoCoreAccessCacheKey      = "casino_core_access:"
	solveTempAccessCustomIDPrefix = "solve_temp_access:"
	unlockTempAccessPrefix        = "unlock_temp_access:"
	guildCasinoCachePrefix        = "guild_casinos:"
)

var (
	tempRoles []*database.DiscordTempRole
)

func removeCasinoAccessScheduler(ctx context.Context) {
	log.Info("Casino access scheduler running...")
	defer log.Infof("Casino access scheduler stopped...")
	ticker := time.NewTicker(time.Minute)
	roles, err := database.DiscordTempRole{}.SelectAll()
	if err != nil {
		log.Fatal(err)
	}
	tempRoles = roles
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, role := range tempRoles {
				expiredCreatedAt := time.Now().Add(-time.Duration(role.ExpirationMins) * time.Minute).UnixMilli()
				expiredAccesses, err := database.DiscordTempRoleAccess{}.SelectAccessExpired(role.TempRoleID, expiredCreatedAt)
				if err != nil {
					log.Error(err)
					continue
				}
				var (
					expiredKeys []string
				)
				for _, access := range expiredAccesses {
					removeUserRoleFromDiscord(access)
					expiredKeys = append(expiredKeys, fmt.Sprintf("%v%v", casinoCoreAccessCacheKey, access.DiscordID))
				}
				if len(expiredKeys) > 0 {
					if err := cache.Redis.Del(ctx, expiredKeys...).Err(); err != nil {
						log.Error(errors.WrapAndReport(err, "remove cached casino access"))
					}
				}
				if len(expiredAccesses) > 0 {
					log.Infof("Casino access scheduler expired %v member role", len(expiredAccesses))
					if err := expiredAccesses.Delete(); err != nil {
						log.Error(errors.WrapAndReport(err, "remove cached casino access"))
					}
				}
			}
		}
	}
}

func removeUserRoleFromDiscord(access *database.DiscordTempRoleAccess) {
	err := session.GuildMemberRoleRemove(access.GuildID, access.DiscordID, access.TempRoleID)
	if err != nil {
		if strings.Contains(err.Error(), "Unknown Member") {
			log.Debugf("User %v not discord guild %v member any more", access.DiscordID, access.GuildID)
			return
		}
		log.Error(errors.WrapAndReport(err, "remove casino role from discord"))
	}
}

func sendCasinoCaptchaVerification(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer logHandlerDuration("send casino captcha verification", time.Now())
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick respond to check quiz game result"))
		return
	}
	var roleID string
	if strings.HasPrefix(i.MessageComponentData().CustomID, unlockTempAccessPrefix) {
		array := strings.Split(i.MessageComponentData().CustomID, ":")
		roleID = array[1]
	} else {
		// TODO 硬编码兼容以前的老消息，casino频道组移除时停止兼容
		if i.GuildID == "915445727600205844" {
			roleID = "997064381475078255"
		} else {
			roleID = "996769243414671409"
		}
	}

	gotTime, err := cache.Redis.Get(context.TODO(), fmt.Sprintf("%v%v", casinoCoreAccessCacheKey, i.Member.User.ID)).Int64()
	if err == nil && time.Since(time.UnixMilli(gotTime)) < time.Hour*24 {
		_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{
				{
					Description: "You already have access to the casino",
					Author:      moffAuthor,
				},
			},
		})
		if err != nil {
			log.Error(errors.WrapAndReport(err, "response to already have access to casino"))
		}
		return
	}
	if err != nil && !errors.Is(err, redis.Nil) {
		log.Error(errors.WrapAndReport(err, "query casino access"))
		interactionResponseEditOnError(s, i)
		return
	}

	fontdata, err := hex.DecodeString(fonts.DotTricksHex)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "decode font"))
		return
	}
	cap := captcha.New()
	cap.SetSize(320, 100)
	cap.SetDisturbance(captcha.HIGH)
	cap.SetBkgColor(color.RGBA{209, 222, 179, 0})
	err = cap.AddFontFromBytes(fontdata)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "add font to captcha"))
		return
	}
	captchaImg, code := cap.Create(6, captcha.ALL)
	// 保存验证码
	captchaID := common.NewCutUUIDString()
	ctx := context.TODO()
	err = cache.Redis.Set(ctx, fmt.Sprintf("%v%v", casinoBotVerificationCacheKey, captchaID), code, time.Minute).Err()
	if err != nil {
		log.Error(errors.WrapAndReport(err, "cache casino bot verification"))
		return
	}

	pr, pw := io.Pipe()
	done := make(chan bool, 1)
	go func() {
		captchaMsg, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{
				{
					Title: "Select the correct answer",
					Image: &discordgo.MessageEmbedImage{
						URL:    "attachment://captcha.jpeg",
						Width:  400,
						Height: 200,
					},
					Footer: &discordgo.MessageEmbedFooter{
						Text: "Your have 60 seconds to solve the CAPTCHA",
					},
					Author: moffAuthor,
				},
			},
			Files: []*discordgo.File{
				{
					Name:        "captcha.jpeg",
					ContentType: "image/jpeg",
					Reader:      pr,
				},
			},
			Components: &[]discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							// Select menu, must have a customID, so we set it to this value.
							CustomID:    fmt.Sprintf("%v%v:%v", solveTempAccessCustomIDPrefix, captchaID, roleID),
							Placeholder: "Select the correct CAPTCHA answer 👇",
							Options:     generateCaptchaCodeOptions(code),
						},
					},
				},
			},
		})
		if err != nil {
			log.Error(errors.WrapAndReport(err, "send casino bot verification"))
		} else {
			if err := cache.Redis.Set(ctx, fmt.Sprintf("msg_2_casino_captcha:%v", captchaID),
				captchaMsg.ID, time.Minute*20).Err(); err != nil {
				log.Error(errors.WrapAndReport(err, "cache message to casino captcha"))
			}
			// todo 60秒后移除当前的消息选项
		}
		close(done)
	}()
	result := blur.Box(captchaImg, 0.5)
	err = jpeg.Encode(pw, result, nil)
	if err != nil {
		panic(err)
	}
	if err := pw.Close(); err != nil {
		log.Error(errors.WrapAndReport(err, "close pipe writer"))
		return
	}
	<-done
}

func solveCasinoCaptcha(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer logHandlerDuration("solve casino captcha", time.Now())
	customID := i.MessageComponentData().CustomID
	array := strings.Split(customID, ":")
	captchaID := array[1]
	roleID := array[2]
	ctx := context.TODO()

	code, err := cache.Redis.Get(ctx, fmt.Sprintf("%v%v", casinoBotVerificationCacheKey, captchaID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			disableCaptchaSelection(s, i)
			return
		}
		log.Error(errors.WrapAndReport(err, "query casino captcha"))
		interactionResponseEditOnError(s, i)
		return
	}
	if code != i.MessageComponentData().Values[0] {
		log.Debugf("want captcha %v but got %v", code, i.MessageComponentData().Values[0])
		disableCaptchaSelection(s, i)
		return
	}

	// 检查角色
	role, err := database.DiscordTempRole{}.SelectOne(i.GuildID, i.ChannelID, roleID)
	if err != nil {
		log.Error(err)
		return
	}
	if role == nil {
		log.Warnf("should got temp role %v but got nil", roleID)
		return
	}

	err = cache.Redis.Set(ctx, fmt.Sprintf("%v%v", casinoCoreAccessCacheKey, i.Member.User.ID), time.Now().UnixMilli(), time.Hour*24).Err()
	if err != nil {
		log.Error(errors.WrapAndReport(err, "cache user access"))
		disableCaptchaSelection(s, i)
		interactionResponseEditOnError(s, i)
		return
	}

	// 验证成功，下放角色
	if err := s.GuildMemberRoleAdd(i.GuildID, i.Member.User.ID, roleID); err != nil {
		log.Error(errors.WrapAndReport(err, "add member casino access"))
		return
	}
	// 发送通知消息
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       fmt.Sprintf("Welcome %v", i.Member.User.Username),
					Description: ":ballot_box_with_check: | Temp Access granted",
					Image:       &discordgo.MessageEmbedImage{},
				},
			},
			Files: []*discordgo.File{},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick respond to solve casino captcha"))
	}
}

func disableCaptchaSelection(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{
				{
					Title: "Select the correct answer",
					Image: &discordgo.MessageEmbedImage{
						URL:    "attachment://captcha.jpeg",
						Width:  400,
						Height: 200,
					},
					Footer: &discordgo.MessageEmbedFooter{
						Text: "Your have 60 seconds to solve the CAPTCHA",
					},
				},
			},
			Components: []discordgo.MessageComponent{},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "disable captcha selection"))
	}
}

func generateCaptchaCodeOptions(correctCode string) []discordgo.SelectMenuOption {
	var codes []string
	codes = append(codes, correctCode)
	for i := 0; i < 4; i++ {
		codes = append(codes, string(captcha.GenerateRandCode(6, captcha.ALL)))
		rand.Shuffle(len(codes), func(i, j int) {
			codes[i], codes[j] = codes[j], codes[i]
		})
	}

	var options []discordgo.SelectMenuOption
	for _, code := range codes {
		option := discordgo.SelectMenuOption{
			Label: code,
			Value: code,
		}
		options = append(options, option)
	}
	return options
}

func swapCharOnce(s string) string {
	a := []rune(s)
	if len(a) < 2 {
		return s
	}
	swapIdx := rand.Intn(len(a)-2) + 1
	a[swapIdx], a[swapIdx+1] = a[swapIdx+1], a[swapIdx]
	return string(a)
}

func permissionsOverwriteForRole(roleID string, permissions int64) *discordgo.PermissionOverwrite {
	var (
		allow, deny int64
	)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionViewChannel)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionManageChannels)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionManageRoles)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionManageWebhooks)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionCreateInstantInvite)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionSendMessages)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionSendMessagesInThreads)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionCreatePublicThreads)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionCreatePrivateThreads)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionEmbedLinks)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionAttachFiles)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionAddReactions)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionUseExternalEmojis)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionUseExternalStickers)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionMentionEveryone)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionManageMessages)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionManageThreads)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionReadMessageHistory)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionSendTTSMessages)
	allowOrDenyPermission(&allow, &deny, permissions, discordgo.PermissionUseSlashCommands)
	return &discordgo.PermissionOverwrite{
		ID:    roleID,
		Type:  discordgo.PermissionOverwriteTypeRole,
		Allow: allow,
		Deny:  deny,
	}
}

func allowOrDenyPermission(allow, deny *int64, permissions, permission int64) {
	if permissions&permission == permission {
		*allow = *allow | permission
		return
	}
	*deny = *deny | permission
}

func onlyViewOverwriteForRole(roleID string) *discordgo.PermissionOverwrite {
	return permissionsOverwriteForRole(roleID, discordgo.PermissionViewChannel|
		discordgo.PermissionReadMessageHistory)
}

func onlyViewOverwritesForRoles(roleIds ...string) []*discordgo.PermissionOverwrite {
	var overwrites []*discordgo.PermissionOverwrite
	for _, roleid := range roleIds {
		overwrites = append(overwrites, permissionsOverwriteForRole(roleid, discordgo.PermissionViewChannel|
			discordgo.PermissionReadMessageHistory))
	}
	return overwrites
}

func messageAndViewOverwriteForRole(roleID string) *discordgo.PermissionOverwrite {
	return permissionsOverwriteForRole(roleID, discordgo.PermissionViewChannel|discordgo.PermissionSendMessages|
		discordgo.PermissionAddReactions|discordgo.PermissionReadMessageHistory)
}

func messageAndViewOverwritesForRoles(roleIds ...string) []*discordgo.PermissionOverwrite {
	var overwrites []*discordgo.PermissionOverwrite
	for _, roleid := range roleIds {
		overwrites = append(overwrites, permissionsOverwriteForRole(roleid, discordgo.PermissionViewChannel|
			discordgo.PermissionSendMessages|discordgo.PermissionAddReactions|discordgo.PermissionReadMessageHistory))
	}
	return overwrites
}
