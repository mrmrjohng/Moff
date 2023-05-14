package database

import (
	"gorm.io/gorm"
	"moff.io/moff-social/pkg/errors"
	"strings"
	"time"
)

const (
	TwitterSpaceURLPrefix = "https://twitter.com/i/spaces/"
)

type Campaigns struct {
	CampaignID      string
	GameID          string
	GameName        string
	GameLogo        string
	Name            string
	DescriptionFull string
	DescriptionText string
	ImageURL        string
	OpenForMint     bool
	Hidden          bool
	Status          string
	StartDate       int64
	EndDate         int64
	Required        JSONBMap
	AppID           string
	ParticipateLink string
}

func FindCampaignWhitelistID(requirements map[string]interface{}) string {
	for k, v := range requirements {
		switch k {
		case "and", "or":
			arr, ok := v.([]interface{})
			if !ok {
				return ""
			}
			for _, ele := range arr {
				require, ok := ele.(map[string]interface{})
				if !ok {
					return ""
				}
				if result := FindCampaignWhitelistID(require); result != "" {
					return result
				}
			}
		case "type":
			if v != "whitelist" {
				return ""
			}
			args := requirements["args"]
			m, ok := args.(map[string]interface{})
			if !ok {
				return ""
			}
			return m["whitelist_id"].(string)
		default:
			continue
		}
	}
	return ""
}

func (in Campaigns) SpaceID() string {
	if in.ParticipateLink == "" {
		return ""
	}
	spaceId := strings.TrimPrefix(in.ParticipateLink, TwitterSpaceURLPrefix)
	spaceId = strings.ReplaceAll(spaceId, " ", "")
	results := strings.Split(spaceId, "/")
	return results[0]
}

func (Campaigns) SelectOne(campaignID string) (*Campaigns, error) {
	var camp Campaigns
	err := PublicPostgres.Where("campaign_id = ?", campaignID).First(&camp).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errors.WrapAndReport(err, "query one campaign")
	}
	return &camp, nil
}

func (in Campaigns) QueryOngoing(limit, offset int) ([]*Campaigns, error) {
	var campaigns []*Campaigns
	now := time.Now().UnixMilli()
	err := PublicPostgres.Where("start_date <= ? and end_date > ? AND hidden = false and status = 'reviewed'", now, now).
		Order("end_date asc").Limit(limit).Offset(offset).Find(&campaigns).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query ongoing campaigns")
	}
	return campaigns, nil
}

func (in Campaigns) QueryUpcomingTwitterSpace() ([]*Campaigns, error) {
	var campaigns []*Campaigns
	now := time.Now().UnixMilli()
	err := PublicPostgres.Select("participate_link,required,campaign_id,app_id,name").
		Where("end_date > ? and status = 'reviewed' and participate_link like '%/spaces%'",
			now).Find(&campaigns).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query ongoing campaigns")
	}
	return campaigns, nil
}

func (in Campaigns) QueryUpcoming(limit, offset int) ([]*Campaigns, error) {
	var campaigns []*Campaigns
	now := time.Now().UnixMilli()
	err := PublicPostgres.Where("start_date > ? AND hidden = false and status = 'reviewed'", now).
		Order("end_date asc").Limit(limit).Offset(offset).Find(&campaigns).Error
	if err != nil {
		return nil, errors.WrapAndReport(err, "query upcoming campaigns")
	}
	return campaigns, nil
}
