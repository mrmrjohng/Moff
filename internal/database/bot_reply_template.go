package database

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
	"gorm.io/gorm"
	"moff.io/moff-social/pkg/errors"
)

type DiscordBotReplyTemplate struct {
	ID           int64    `gorm:"primaryKey"`
	InteractID   string   `gorm:"type:varchar(100);uniqueIndex"`
	Faq          string   `gorm:"type:varchar(500)"`
	ReplyMessage JSONBMap `gorm:"type:jsonb"`
}

func (DiscordBotReplyTemplate) SelectInteractID(interactID string) (*Message, error) {
	var entity DiscordBotReplyTemplate
	err := CommunityPostgres.Where("interact_id = ?", interactID).First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.WrapAndReport(err, "query reply template")
	}
	return NewMessageFromJsonb(entity.ReplyMessage), nil
}

func (DiscordBotReplyTemplate) SelectDefaultChoices() ([]*discordgo.ApplicationCommandOptionChoice, error) {
	var temps []*DiscordBotReplyTemplate
	err := CommunityPostgres.Select("faq,interact_id").Limit(25).Find(&temps).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query template choices")
	}
	return toAppCommandOptionsChoice(temps)
}

func (DiscordBotReplyTemplate) SelectFaqLike(content string) ([]*discordgo.ApplicationCommandOptionChoice, error) {
	var temps []*DiscordBotReplyTemplate
	err := CommunityPostgres.Select("faq,interact_id").Where("faq like ?", fmt.Sprintf("%%%v%%", content)).
		Limit(25).Find(&temps).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query template choices")
	}
	return toAppCommandOptionsChoice(temps)
}

func toAppCommandOptionsChoice(results []*DiscordBotReplyTemplate) ([]*discordgo.ApplicationCommandOptionChoice, error) {
	var choices []*discordgo.ApplicationCommandOptionChoice
	for _, res := range results {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  res.Faq,
			Value: res.InteractID,
		})
	}
	return choices, nil
}

type BotReplyChannelType string

const (
	BotReplyChannelTypeCurrentChannel = BotReplyChannelType("current_message_channel")
	BotReplyChannelTypeDirectMessage  = BotReplyChannelType("direct_message_channel")
)

// message content <= 2000

type DiscordMessage struct {
	Content    string                     // 消息文本
	Embeds     []*DiscordEmbedMessage     // 嵌套消息
	Components []*DiscordMessageComponent // 组件消息
}

type DiscordMessageEmbedType string

const (
	DiscordMessageEmbedTypeRich    = DiscordMessageEmbedType("rich")
	DiscordMessageEmbedTypeImage   = DiscordMessageEmbedType("image")
	DiscordMessageEmbedTypeVideo   = DiscordMessageEmbedType("video")
	DiscordMessageEmbedTypeGifv    = DiscordMessageEmbedType("gifv")
	DiscordMessageEmbedTypeArticle = DiscordMessageEmbedType("article")
	DiscordMessageEmbedTypeLink    = DiscordMessageEmbedType("link")
)

func (in DiscordMessageEmbedType) String() string {
	return string(in)
}

type DiscordEmbedMessage struct {
	URL         string                  `json:"url,omitempty"`
	Type        DiscordMessageEmbedType `json:"type,omitempty"`
	Title       string                  `json:"title,omitempty"`
	Description string                  `json:"description,omitempty"`
}

type DiscordMessageComponentType string

const (
	DiscordMessageComponentTypeButton           = DiscordMessageComponentType("button")
	DiscordMessageComponentTypeSingleSelectMenu = DiscordMessageComponentType("single_select_menu")
	DiscordMessageComponentTypeMultiSelectMenu  = DiscordMessageComponentType("multi_select_menu")
)

type DiscordMessageComponent struct {
	Type    DiscordMessageComponentType
	Content string
}

type ButtonStyle string

// Button styles.
const (
	// PrimaryButton is a button with blurple color.
	PrimaryButton ButtonStyle = "primary"
	// SecondaryButton is a button with grey color.
	SecondaryButton ButtonStyle = "secondary"
	// SuccessButton is a button with green color.
	SuccessButton ButtonStyle = "success"
	// DangerButton is a button with red color.
	DangerButton ButtonStyle = "danger"
	// LinkButton is a special type of button which navigates to a URL. Has grey color.
	LinkButton ButtonStyle = "link"
)

func (in ButtonStyle) Uint() uint {
	switch in {
	case PrimaryButton:
		return 1
	case SecondaryButton:
		return 2
	case SuccessButton:
		return 3
	case DangerButton:
		return 4
	case LinkButton:
		return 5
	default:
		return 0
	}
}

type DiscordButton struct {
	Label    string
	Style    ButtonStyle
	Disabled bool
	Emoji    string

	// NOTE: Only button with LinkButton style can have link. Also, URL is mutually exclusive with CustomID.
	URL      string
	CustomID string
}
