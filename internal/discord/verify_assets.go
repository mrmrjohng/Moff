package discord

import (
	"github.com/bwmarrin/discordgo"
	"moff.io/moff-social/pkg/log"
)

func sendVerifyUserAssetsMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	_, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{
			{
				Type:  discordgo.EmbedTypeRich,
				Title: "Verify your NFT assets on moff",
				Description: "moffers! Verify your NFTs assets on [moff.io](https://moff.io/) here to unlock identity-gated roles in this server! 😜\n" +
					"If you are reading this on your mobile devices, we highly recommend click ‘moff official website' to connect! 😉" +
					"\n```\nNote: \nThis connection only can verify evm compatible blockchain assets. \n" +
					"For non-evm compatible blockchain assets,please go to the moff official website to login with wallet, and then connect discord.\n```" +
					"\n**This is a read-only connection. DO NOT share your private keys. We will NEVER ask for your seed phrase. We will NEVER DM you..**",
				// 嵌入的左边栏的颜色，最左方的竖条
				Color: 15158332,
				//Color: "#7289da",
				// 在嵌入消息的顶部，icon在前，名字在后
				Author: moffAuthor,
			},
		},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "Let's verify!",
						Style:    discordgo.PrimaryButton,
						CustomID: "verify_user_assets",
						// component交互时，custom id必须设置，并且同一个message内custom id必须唯一, 最大100个字符
						// 非链接按钮必须拥有custom id，并且不能有url属性
						// 链接按钮必须拥有url属性，并且不能有custom id, 链接按钮点击时不会生成交互事件
					},
					discordgo.Button{
						Label: "moff official website",
						Style: discordgo.LinkButton,
						URL:   "https://moff.io",
						// component交互时，custom id必须设置，并且同一个message内custom id必须唯一, 最大100个字符
						// 非链接按钮必须拥有custom id，并且不能有url属性
						// 链接按钮必须拥有url属性，并且不能有custom id, 链接按钮点击时不会生成交互事件
					},
				},
			},
		},
	})
	if err != nil {
		log.Errorf("send embed:%v", err.Error())
	}
}
