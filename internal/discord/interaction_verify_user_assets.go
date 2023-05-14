package discord

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"moff.io/moff-social/internal/aws"
	"moff.io/moff-social/internal/chains/moralis"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/internal/walletconnect"
	"moff.io/moff-social/pkg/concurrent"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strconv"
	"strings"
	"time"
)

func verifyUserAssetsHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer logHandlerDuration("veriy user assets", time.Now())
	// 快速响应，等待后续响应用户
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "quick response to verify user assets"))
		return
	}
	verifyUserAssetsPipes <- &verifyUserAssetsPipe{
		session:     s,
		interaction: i,
	}
}

type verifyUserAssetsPipe struct {
	session     *discordgo.Session
	interaction *discordgo.InteractionCreate
}

var (
	verifyUserAssetsPipes chan *verifyUserAssetsPipe
)

func blockingVerifyUserAssets() {
	log.Info("Blocking verify user assets running...")
	defer func() {
		if i := recover(); i != nil {
			log.Errorf("Blocking verify user assets stopped:%v", i)
			return
		}
		log.Warn("Blocking verify user assets stopped...")
	}()
	var (
		concurrency = concurrent.NewLimiter(100)
	)
	for pipe := range verifyUserAssetsPipes {
		verifyUserAssets(concurrency, pipe)
	}
}

func verifyUserAssets(concurrency concurrent.Limiter, pipe *verifyUserAssetsPipe) {
	concurrency.Add()
	defer concurrency.Done()

	client := walletconnect.NewClient()
	qrCodeUrl, err := uploadWalletConnectQRCodeToS3(client)
	if err != nil {
		log.Errorf("upload qrcode:%v", err)
		interactionResponseEditOnError(pipe.session, pipe.interaction)
		return
	}
	guild := botGuild(pipe.interaction.GuildID)
	if guild == nil {
		interactionResponseEditOnError(pipe.session, pipe.interaction)
		return
	}
	signMsg, displayQRCodeFn := getDisplaySignMessageAndQRCodeFn(pipe, guild, qrCodeUrl)
	// 连接用户钱包
	ctx, cancelFunc := context.WithTimeout(context.TODO(), time.Minute*5)
	defer cancelFunc()
	wallet, err := client.ConnectWallet(ctx, signMsg, displayQRCodeFn)
	if err != nil {
		log.Errorf("connect wallet:%v", err)
		return
	}
	// TODO test only
	//wallet.ChainID = 137
	//wallet.Accounts[0] = "0xFA7e751F6802437553c9880438F1E99A3Ad8240f"

	if wallet.Confirmed() {
		log.Debug("wallet confirmed")
		// 校验用户的地址，检查所在的链是否存在tpr
		roles, err := database.DiscordTokenPermissionedRole{}.SelectByGuildIDAndChainID(pipe.interaction.GuildID,
			strconv.Itoa(wallet.ChainID))
		if err != nil {
			log.Errorf("query tprs:%v", err)
			return
		}
		if len(roles) == 0 {
			// 绑定的钱包地址，没有设置角色组
			_, err := pipe.session.FollowupMessageCreate(pipe.interaction.Interaction, true, &discordgo.WebhookParams{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "no roles found for corresponding blockchain",
			})
			if err != nil {
				log.Errorf("wallet connect result respond:%v", err)
			}
			return
		}
		// 检查可授予的角色
		discordRoles, err := validateUserTprForSingleChain(wallet.Accounts[0], roles)
		if err != nil {
			_, err := pipe.session.FollowupMessageCreate(pipe.interaction.Interaction, true, &discordgo.WebhookParams{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "Unknown error",
			})
			if err != nil {
				log.Errorf("wallet connect result respond:%v", err)
			}
			return
		}
		// 没有有效角色可以授予，TODO 这里其实和通过moralis查的，moralis有可能未同步数据，所以这里没有的情况下，可以直接不提示？
		if len(discordRoles) == 0 {
			_, err := pipe.session.FollowupMessageCreate(pipe.interaction.Interaction, true, &discordgo.WebhookParams{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "Sorry, we can not assign you roles as you did not own NFT...",
			})
			if err != nil {
				log.Errorf("wallet connect result respond:%v", err)
			}
			return
		}
		// 授予角色
		var roleDescription = "You have been granted the following roles:"
		for _, dr := range discordRoles {
			roleDescription = fmt.Sprintf("%v\n-**%v**", roleDescription, dr.RoleName)
			err := pipe.session.GuildMemberRoleAdd(dr.GuildID, pipe.interaction.Member.User.ID, dr.RoleID)
			if err != nil {
				log.Errorf("add member role:%v", err)
				return
			}
		}
		// 通知用户，角色授予成功
		_, err = pipe.session.FollowupMessageCreate(pipe.interaction.Interaction, true, &discordgo.WebhookParams{
			Flags: discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{
				{
					Type:        discordgo.EmbedTypeImage,
					Title:       "Roles granted by the bot",
					Description: roleDescription,
					// 嵌入的左边栏的颜色，最左方的竖条
					Color: 6095103,
					// 在嵌入消息的顶部，icon在前，名字在后
					Author: moffAuthor,
				},
			},
		})
		if err != nil {
			log.Errorf("wallet connect result respond:%v", err)
		}
		return
	}
	if !wallet.Approved() {
		log.Debug("wallet connect denied")
		_, err := pipe.session.FollowupMessageCreate(pipe.interaction.Interaction, true, &discordgo.WebhookParams{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "Sorry, seems you denied wallet connect...",
		})
		if err != nil {
			log.Errorf("wallet connect result respond:%v", err)
		}
	}
	if !wallet.Signed() {
		log.Debug("wallet sign denied")
		_, err := pipe.session.FollowupMessageCreate(pipe.interaction.Interaction, true, &discordgo.WebhookParams{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "Sorry, seems you denied wallet sign...",
		})
		if err != nil {
			log.Errorf("wallet connect result respond:%v", err)
		}
	}
}

// validateUserTprForSingleChain 校验用户在单个链上的代币授予角色
func validateUserTprForSingleChain(userAddr string, roles []*database.DiscordTokenPermissionedRole) ([]*database.DiscordTokenPermissionedRole, error) {
	tokenContracts, tokenRolesMapping, contractRolesMapping := classifyTPRsMapping(roles)
	// 构建moralis请求，拉取用户nft数据
	req := &moralis.GetAddressNFTRequest{
		ChainName:      roles[0].ChainName,
		Limit:          10,
		Format:         "decimal",
		TokenAddresses: tokenContracts,
		OwnerAddress:   userAddr,
	}
	// 如果只检查单个nft拥有，不校验token id时，moralis只返回一条有效数据即可
	if len(contractRolesMapping) == 1 && len(tokenRolesMapping) == 0 {
		req.Limit = 1
	}
	// 获取用户在该链所有的nft, todo 实际这样一锅端在以后是可能存在问题的，需根据实际情况修改
	var nfts []*moralis.AddressNFT
	for {
		response, err := moralis.NewClient().GetAddressNfts(req)
		if err != nil {
			return nil, err
		}
		if len(response.Result) == 0 {
			break
		}
		nfts = append(nfts, response.Result...)
		// 当前仅在校验拥有nft
		if req.Limit == 1 {
			break
		}
		// moralis没有更多数据
		if response.Cursor == "" {
			break
		}
		req.Cursor = response.Cursor
	}
	return calculateTPRs(nfts, contractRolesMapping, tokenRolesMapping)
}

// classifyTPRMapping 分类代币授予角色的映射
// 返回值：
//	tokenContracts：需要检查的所有合约地址
//  tokenRolesMapping：根据合约地址、token id、持有数量，进行tpr授予的映射，key为token id
//  contractRolesMapping：根据合约地址、持有数量，进行tpr授予的映射，key为合约地址
func classifyTPRsMapping(roles []*database.DiscordTokenPermissionedRole) (
	tokenContracts []string, tokenRolesMapping, contractRolesMapping map[string][]*database.DiscordTokenPermissionedRole) {
	tokenRolesMapping = make(map[string][]*database.DiscordTokenPermissionedRole)
	contractRolesMapping = make(map[string][]*database.DiscordTokenPermissionedRole)
	// 注意：目前均是evm兼容链，大小写不敏感
	for _, role := range roles {
		// 基于合约的某个token id进行tpr
		if role.TokenID != "" {
			tokenID := strings.ToLower(role.TokenID)
			permissionRoles := tokenRolesMapping[tokenID]
			if permissionRoles == nil {
				permissionRoles = []*database.DiscordTokenPermissionedRole{}
			}
			permissionRoles = append(permissionRoles, role)
			tokenRolesMapping[tokenID] = permissionRoles
		} else {
			// 只要该合约中持有token即可进行tpr
			contractAddr := strings.ToLower(role.ContractAddress)
			permissionRoles := contractRolesMapping[contractAddr]
			if permissionRoles == nil {
				permissionRoles = []*database.DiscordTokenPermissionedRole{}
			}
			permissionRoles = append(permissionRoles, role)
			contractRolesMapping[contractAddr] = permissionRoles
		}
		tokenContracts = append(tokenContracts, role.ContractAddress)
	}
	return tokenContracts, tokenRolesMapping, contractRolesMapping
}

func calculateTPRs(nfts []*moralis.AddressNFT, contractRolesMapping, tokenRolesMapping map[string][]*database.DiscordTokenPermissionedRole) ([]*database.DiscordTokenPermissionedRole, error) {
	var roleSet = make(map[string]*database.DiscordTokenPermissionedRole)
	// 计算可授予用户的角色
	for _, nft := range nfts {
		// 基于nft持有授予角色
		cpRoles := contractRolesMapping[strings.ToLower(nft.TokenAddress)]
		if len(cpRoles) != 0 {
			for _, pr := range cpRoles {
				// 检查持有数量是否满足要求
				ownedAmt, err := strconv.ParseInt(nft.Amount, 10, 64)
				if err != nil {
					return nil, errors.WrapAndReport(err, "docode moralis nft amount")
				}
				if ownedAmt < pr.MinOwnAmount {
					continue
				}
				// 添加授予角色
				roleSet[pr.RoleID] = pr
			}
		}
		// 基于token id授予角色
		tpRoles := tokenRolesMapping[strings.ToLower(nft.TokenID)]
		if len(tpRoles) != 0 {
			for _, pr := range tpRoles {
				// 检查持有数量是否满足要求
				ownedAmt, err := strconv.ParseInt(nft.Amount, 10, 64)
				if err != nil {
					return nil, errors.WrapAndReport(err, "docode moralis nft amount")
				}
				if ownedAmt < pr.MinOwnAmount {
					continue
				}
				// 添加授予角色
				roleSet[pr.RoleID] = pr
			}
		}
	}

	var result []*database.DiscordTokenPermissionedRole
	for _, role := range roleSet {
		result = append(result, role)
	}
	return result, nil
}

func getDisplaySignMessageAndQRCodeFn(pipe *verifyUserAssetsPipe, guild *discordgo.UserGuild, qrCodeUrl string) (
	signMsg string, displayQRCodeFn walletconnect.DisplayQRCodeFn) {
	signingUser := fmt.Sprintf("%v#%v", pipe.interaction.Member.User.Username, pipe.interaction.Member.User.Discriminator)
	signMsg = fmt.Sprintf("moff (moff.io) asks you to sign this message for the purpose of verifying your account ownership. This is READ-ONLY access and will NOT trigger any blockchain transactions or incur any fees.\n\n- Community: %v\n- User: %v\n- Timestamp: %v",
		guild.Name, signingUser, time.Now().UTC())
	displayQRCodeFn = func() error {
		content := "Use following qrcode to connect (valid for 5 minutes)\nGuild: moff Member: " + pipe.interaction.Member.User.ID
		_, err := pipe.session.InteractionResponseEdit(pipe.interaction.Interaction, &discordgo.WebhookEdit{
			Content: &content,
			Embeds: &[]*discordgo.MessageEmbed{
				{
					Type:        discordgo.EmbedTypeImage,
					Title:       "Please read instructions carefully before connecting",
					Description: "You should expect to sign the following message with a wallet-connect compatible wallet such as TokenPocket:\n```" + signMsg + "```\n**Scan following QR code to connect wallet:**",
					// 嵌入的左边栏的颜色，最左方的竖条
					Color: 6095103,
					// 在嵌入消息的顶部，icon在前，名字在后
					Author: moffAuthor,
					Image: &discordgo.MessageEmbedImage{
						URL:    qrCodeUrl,
						Width:  250,
						Height: 250,
					},
				},
			},
		})
		return errors.WrapAndReport(err, "display qrcode to discord")
	}

	return signMsg, displayQRCodeFn
}

func uploadWalletConnectQRCodeToS3(client walletconnect.ClientV1) (url string, err error) {
	// 上传二维码至s3
	code, err := client.GetQRCode()
	if err != nil {
		return "", err
	}
	s3ObjectKey := fmt.Sprintf("expires/%v.png", uuid.NewString())
	err = aws.Client.PutFileToS3(context.TODO(), s3ObjectKey, bytes.NewReader(code))
	if err != nil {
		return "", err
	}
	// 预签名二维码url
	url, err = aws.Client.GetS3PresignedAccessURL(context.TODO(), s3ObjectKey, time.Minute*10)
	return url, err
}

func interactionResponseEditOnMsg(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Description: msg,
				Author:      moffAuthor,
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "edit response"))
	}
}

func interactionResponseEditOnError(s *discordgo.Session, i *discordgo.InteractionCreate) {
	interactionResponseEditOnMsg(s, i, "Unknown error")
}
