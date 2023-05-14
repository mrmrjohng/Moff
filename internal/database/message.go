package database

import (
	"github.com/bwmarrin/discordgo"
	"gorm.io/gorm/clause"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
)

type DiscordMessages struct {
	ID          int64      `gorm:"primaryKey"`
	GuildID     string     `gorm:"type:varchar(255);index"`
	ChannelID   string     `gorm:"type:varchar(255);index"`
	MessageID   string     `gorm:"type:varchar(255);index"`
	AuthorID    string     `gorm:"type:varchar(255);index"`
	Content     string     `gorm:"type:text"`
	ContentLen  int        `gorm:"type:int"`
	Images      JSONBArray `gorm:"type:jsonb"`
	CreatedTime int64      `gorm:"type:int8"`
	UpdatedTime int64      `gorm:"type:int8"`
	DeletedTime *int64     `gorm:"type:int8"`
}

func (in DiscordMessages) Create() error {
	err := CommunityPostgres.Create(&in).Error
	return errors.WrapAndReport(err, "create discord message")
}

func (in DiscordMessages) UpdateMessage() error {
	err := CommunityPostgres.Where("guild_id = ? AND channel_id = ? AND message_id = ? AND deleted_time IS NULL",
		in.GuildID, in.ChannelID, in.MessageID).Updates(DiscordMessages{
		Content:     in.Content,
		ContentLen:  in.ContentLen,
		Images:      in.Images,
		UpdatedTime: in.UpdatedTime,
	}).Error
	return errors.WrapAndReport(err, "update discord message")
}

func (in DiscordMessages) Delete() error {
	err := CommunityPostgres.Where("guild_id = ? AND channel_id = ? AND message_id = ? AND deleted_time IS NULL",
		in.GuildID, in.ChannelID, in.MessageID).Updates(DiscordMessages{
		UpdatedTime: in.UpdatedTime,
		DeletedTime: in.DeletedTime,
	}).Error
	return errors.WrapAndReport(err, "delete discord message")
}

type DiscordForumAction string

const (
	DiscordForumActionPubPost   = DiscordForumAction("pub_post")
	DiscordForumActionReplyPost = DiscordForumAction("reply_post")
)

type DiscordForums struct {
	ID          int64              `gorm:"primaryKey"`
	GuildID     string             `gorm:"type:varchar(255);index"`
	ForumID     string             `gorm:"type:varchar(255);index"`
	PostID      string             `gorm:"type:varchar(255);index"`
	AuthorID    string             `gorm:"type:varchar(255);index"`
	Action      DiscordForumAction `gorm:"type:varchar(255)"`
	MessageID   string             `gorm:"type:varchar(255);index"`
	Content     string             `gorm:"type:text"`
	ContentLen  int                `gorm:"type:int"`
	Images      JSONBArray         `gorm:"type:jsonb"`
	CreatedTime int64              `gorm:"type:int8"`
	UpdatedTime int64              `gorm:"type:int8"`
	DeletedTime *int64             `gorm:"type:int8"`
}

func (in DiscordForums) Create() error {
	err := CommunityPostgres.Create(&in).Error
	return errors.WrapAndReport(err, "create discord forums")
}

func (in DiscordForums) UpdateMessage() error {
	err := CommunityPostgres.Where("guild_id = ? AND forum_id = ? AND post_id = ? AND message_id = ? AND deleted_time IS NULL",
		in.GuildID, in.ForumID, in.PostID, in.MessageID).Updates(DiscordForums{
		Content:     in.Content,
		ContentLen:  in.ContentLen,
		Images:      in.Images,
		UpdatedTime: in.UpdatedTime,
	}).Error
	return errors.WrapAndReport(err, "update discord forum message")
}

func (in DiscordForums) DeleteMessage() error {
	err := CommunityPostgres.Where("guild_id = ? AND forum_id = ? AND post_id = ? AND message_id = ? AND deleted_time IS NULL",
		in.GuildID, in.ForumID, in.PostID, in.MessageID).Updates(DiscordForums{
		UpdatedTime: in.UpdatedTime,
		DeletedTime: in.DeletedTime,
	}).Error
	return errors.WrapAndReport(err, "delete discord forum message")
}

type Message struct {
	Content    string                   `bson:"content"`
	Embeds     []map[string]interface{} `bson:"embeds"`
	Components []map[string]interface{} `bson:"components"`
}

func NewMessageFromJsonb(msg JSONBMap) *Message {
	return &Message{
		Content:    msg["content"].(string),
		Embeds:     convertMessageInterface(msg["embeds"]),
		Components: convertMessageInterface(msg["components"]),
	}
}

func convertMessageInterface(itf interface{}) []map[string]interface{} {
	array, ok := itf.([]interface{})
	if !ok {
		log.Errorf("Array not converted from %v", itf)
		return nil
	}
	var results []map[string]interface{}
	for _, val := range array {
		item, ok := val.(map[string]interface{})
		if !ok {
			log.Errorf("Map not converted from %v", val)
			continue
		}
		results = append(results, item)
	}
	return results
}

func (in *Message) GetMessageEmbeds() ([]*discordgo.MessageEmbed, error) {
	if len(in.Embeds) == 0 {
		return nil, nil
	}
	var embeds []*discordgo.MessageEmbed
	for _, val := range in.Embeds {
		//typ := stringValueFromMap(val, "type")
		embed := &discordgo.MessageEmbed{
			URL:         stringValueFromMap(val, "url"),
			Type:        discordgo.EmbedTypeRich,
			Title:       stringValueFromMap(val, "title"),
			Description: stringValueFromMap(val, "description"),
			Color:       intValueFromMap(val, "color"),
		}
		// embed图片
		img, ok := val["image"].(map[string]interface{})
		if ok {
			embed.Image = &discordgo.MessageEmbedImage{
				URL:    stringValueFromMap(img, "url"),
				Width:  intValueFromMap(img, "width"),
				Height: intValueFromMap(img, "height"),
			}
		}
		// embed缩略图
		thumbnail, ok := val["thumbnail"].(map[string]interface{})
		if ok {
			embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
				URL:    stringValueFromMap(thumbnail, "url"),
				Width:  intValueFromMap(thumbnail, "width"),
				Height: intValueFromMap(thumbnail, "height"),
			}
		}
		// embed作者
		author, ok := val["author"].(map[string]interface{})
		if ok {
			embed.Author = &discordgo.MessageEmbedAuthor{
				URL:     stringValueFromMap(author, "url"),
				Name:    stringValueFromMap(author, "name"),
				IconURL: stringValueFromMap(author, "icon_url"),
			}
		}
		// embed字段
		fields, ok := val["fields"].([]interface{})
		if ok {
			var fieldArr []*discordgo.MessageEmbedField
			for _, field := range fields {
				fm, _ := field.(map[string]interface{})
				fieldArr = append(fieldArr, &discordgo.MessageEmbedField{
					Name:   stringValueFromMap(fm, "name"),
					Value:  stringValueFromMap(fm, "value"),
					Inline: boolValueFromMap(fm, "inline"),
				})
			}
			embed.Fields = fieldArr
		}
		embeds = append(embeds, embed)
	}
	return embeds, nil
}

func (in *Message) GetMessageComponents() ([]discordgo.MessageComponent, error) {
	if len(in.Components) == 0 {
		return nil, nil
	}
	var (
		components []discordgo.MessageComponent
		// discord row 至多存放5个元素
		row discordgo.ActionsRow
	)
	for i, val := range in.Components {
		typ := stringValueFromMap(val, "type")
		switch DiscordMessageComponentType(typ) {
		case DiscordMessageComponentTypeButton:
			btn := &discordgo.Button{
				Label:    stringValueFromMap(val, "label"),
				Style:    discordgo.ButtonStyle(ButtonStyle(stringValueFromMap(val, "style")).Uint()),
				CustomID: stringValueFromMap(val, "custom_id"),
				URL:      stringValueFromMap(val, "url"),
			}
			// 检查button是否有emoji
			emoji := stringValueFromMap(val, "emoji")
			if emoji != "" {
				btn.Emoji = discordgo.ComponentEmoji{
					Name: emoji,
				}
			}
			row.Components = append(row.Components, btn)
			// 当元素装满一个row或最后一个元素时，把row加至components
			if len(row.Components) == 5 || i == len(in.Components)-1 {
				components = append(components, row)
				row = discordgo.ActionsRow{}
			}
		case DiscordMessageComponentTypeSingleSelectMenu, DiscordMessageComponentTypeMultiSelectMenu:
			return nil, errors.ErrorfAndReport("message component type %v not implemented", typ)
		default:
			return nil, errors.ErrorfAndReport("message component type %v not implemented", typ)
		}
	}
	return components, nil
}

func stringValueFromMap(m map[string]interface{}, key string) string {
	s, _ := m[key].(string)
	return s
}

func intValueFromMap(m map[string]interface{}, key string) int {
	i, _ := m[key].(int)
	return i
}

func boolValueFromMap(m map[string]interface{}, key string) bool {
	b, _ := m[key].(bool)
	return b
}

type DiscordMessageReaction struct {
	ID          int64  `gorm:"primaryKey"`
	GuildID     string `gorm:"type:varchar(100);uniqueIndex:uni_react"`
	ChannelID   string `gorm:"type:varchar(100);uniqueIndex:uni_react"`
	MessageID   string `gorm:"type:varchar(100);index;uniqueIndex:uni_react"`
	DiscordID   string `gorm:"type:varchar(100);index;uniqueIndex:uni_react"`
	EmojiName   string `gorm:"type:varchar(200);uniqueIndex:uni_react"`
	CreatedTime int64  `gorm:"type:int8"`
}

func (in *DiscordMessageReaction) Save() error {
	err := PublicPostgres.Clauses(clause.OnConflict{DoNothing: true}).Create(in).Error
	return errors.WrapAndReport(err, "save discord message reaction")
}
