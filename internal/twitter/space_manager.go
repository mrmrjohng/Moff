package twitter

import (
	"context"
	"fmt"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultManagerSpaceID      = "twitter_space_manager"
	twitterSpaceManagerLockKey = "twitter_space_manager_lock"
	spaceManagerAutomation     = "twitter_space_manager_automation"
)

var (
	initSpaceManagerOnce sync.Once
	internalSpaceManager *SpaceManager
)

type SpaceManager struct {
	authorization *database.TwitterWebAuthorization
}

func NewSpaceManager() *SpaceManager {
	initSpaceManagerOnce.Do(func() {
		internalSpaceManager = &SpaceManager{}
		authorization, err := internalSpaceManager.nextTwitterAuthorization(defaultManagerSpaceID)
		if err != nil {
			log.Fatal(err)
		}
		internalSpaceManager.authorization = authorization
	})
	return internalSpaceManager
}

func (in *SpaceManager) Start(ctx context.Context) {
	notStarted, waitStarted, monitoring, ended, err := in.filterSnapshots(true)
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("Twitter space manager filter:%v not started, %v to be monitoring, %v being monitoring, %v ended.",
		len(notStarted), len(waitStarted), len(monitoring), len(ended))
	autoAdded, err := in.authAddCampaignTwitterSpaceSnapshots()
	if err != nil {
		log.Fatal(err)
	}
	if autoAdded > 0 {
		log.Infof("Twitter space manager auto added %v snapshots", autoAdded)
	}
	go in.start(ctx)
	go in.cleanupBackups(ctx)
}

func (in *SpaceManager) cleanupBackups(ctx context.Context) {
	log.Infof("Twitter space manager cleanup backups...")
	defer log.Infof("Twitter space manager cleanup backups stopped...")
	var (
		checkBackupTicker = time.NewTicker(time.Minute * 10)
	)
	for {
		select {
		case <-checkBackupTicker.C:
			history := time.Now().Add(-15 * 24 * time.Hour).UTC()
			err := database.TwitterSpaceBackups{}.DeleteBefore(history)
			if err != nil {
				log.Error(err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (in *SpaceManager) start(ctx context.Context) {
	var (
		autoAddSnapshotTicker = time.NewTicker(time.Minute * 2)
		checkSnapshotTicker   = time.NewTicker(time.Minute * 10)
	)
	log.Infof("Twitter space manager running...")
	defer log.Infof("Twitter space manager stopped...")
	for {
		select {
		case <-checkSnapshotTicker.C:
			notStarted, waitStarted, monitoring, ended, err := in.filterSnapshots(false)
			if err != nil {
				log.Error(err)
				continue
			}
			log.Infof("Twitter space manager filter:%v not started, %v to be monitoring, %v being monitoring, %v ended.",
				len(notStarted), len(waitStarted), len(monitoring), len(ended))
		case <-autoAddSnapshotTicker.C:
			autoAdded, err := in.authAddCampaignTwitterSpaceSnapshots()
			if err != nil {
				log.Error(err)
				continue
			}
			if autoAdded > 0 {
				log.Infof("Twitter space manager auto added %v snapshots", autoAdded)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (in *SpaceManager) authAddCampaignTwitterSpaceSnapshots() (autoAdded int64, err error) {
	campaigns, err := database.Campaigns{}.QueryUpcomingTwitterSpace()
	if err != nil {
		return 0, err
	}
	if len(campaigns) == 0 {
		return 0, err
	}
	apps, err := queryCampaignApps(campaigns)
	if err != nil {
		return 0, err
	}
	var (
		initLog = make(map[string]bool)
	)
	for _, campaign := range campaigns {
		app := apps[campaign.AppID]
		if app == nil && !initLog[campaign.AppID] {
			log.Warnf("Campaign %v app %v not found", campaign.CampaignID, campaign.AppID)
			initLog[campaign.AppID] = true
			continue
		}
		if !app.AutoSnapshotTwitterSpaceEnabled && !initLog[campaign.AppID] {
			log.Infof("App %v auto twitter space snapshot not enabled", app.AppID)
			initLog[campaign.AppID] = true
			continue
		}
		// 检查是否已添加至数据库
		snapshot, err := database.TwitterSpaceOwnerships{}.SelectOne(app.DiscordGuildId, campaign.SpaceID())
		if err != nil {
			log.Error(err)
			continue
		}
		if snapshot != nil {
			continue
		}
		autoAdded++
		_, tips := in.CreateTwitterSpaceSnapshot(app.DiscordGuildId, spaceManagerAutomation, campaign.ParticipateLink, campaign)
		if tips != "" {
			log.Warnf("Twitter space manager create snapshot got tips %v", tips)
		}
	}
	return autoAdded, nil
}

func (in *SpaceManager) filterSnapshots(ignoreMonitoring bool) (notStarted, waitStarted, monitoring, ended []*database.TwitterSpaceSnapshots, err error) {
	ongoing, err := database.TwitterSpaceSnapshots{}.SelectOngoing()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if len(ongoing) == 0 {
		return nil, nil, nil, nil, nil
	}

	// 检查快照状态
	for _, snapshot := range ongoing {
		monitor := NewSpaceMonitor(in.authorization, snapshot)
		isMonitoring, err := monitor.IsMonitoring()
		if err != nil {
			log.Error(err)
			continue
		}
		if isMonitoring && !ignoreMonitoring {
			monitoring = append(monitoring, snapshot)
			continue
		}

		// 检查space最新状态
		space, err := monitor.QueryTwitterSpace()
		if err != nil {
			in.handleSpaceQueryError(err)
			continue
		}
		if space == nil {
			// 不应当出现这种情况，出现则说明space id有问题
			log.Error(errors.ErrorfAndReport("twitter space %v should not found", snapshot.SpaceID))
			ended = append(ended, snapshot)
			in.endSnapshot(snapshot)
			continue
		}
		switch space.State {
		case SpaceNotStarted:
			if space.IsGoingRunning() {
				// 在预开场半个小时前提前开始
				waitStarted = append(waitStarted, snapshot)
				in.runMonitor(snapshot)
			} else {
				notStarted = append(notStarted, snapshot)
			}
		case SpaceEnded, SpaceCanceled, SpaceTimeout:
			ended = append(ended, snapshot)
			in.runMonitor(snapshot)
		case SpaceRunning:
			waitStarted = append(waitStarted, snapshot)
			in.runMonitor(snapshot)
		default:
			log.Warnf("Twitter space unhandled status %v", space.State)
		}
	}
	return
}

func (in *SpaceManager) CreateTwitterSpaceSnapshot(discordGuildId, starterId, spaceURL string, campaign *database.Campaigns) (
	owns *database.TwitterSpaceSnapshotOwns, tips string) {
	spaceURL = strings.ReplaceAll(spaceURL, " ", "")
	spaceId := SpaceIDFromURL(spaceURL)
	if spaceId == "" {
		return nil, "Unrecognized twitter space URL"
	}
	snapshot := &database.TwitterSpaceSnapshotOwns{
		TwitterSpaceSnapshots: database.TwitterSpaceSnapshots{
			SpaceID:  spaceId,
			SpaceURL: spaceURL,
		},
		TwitterSpaceOwnerships: database.TwitterSpaceOwnerships{
			DiscordGuildID:     discordGuildId,
			StarterDiscordID:   starterId,
			TwitterSpaceID:     spaceId,
			SnapshotMinSeconds: 0,
			CreatedTime:        time.Now().UnixMilli(),
		},
	}
	if campaign != nil {
		snapshot.LinkedCampaignID = campaign.CampaignID
		snapshot.CampaignWhitelistID = database.FindCampaignWhitelistID(campaign.Required)
	}

	space, err := NewSpaceMonitor(in.authorization, &snapshot.TwitterSpaceSnapshots).QueryTwitterSpace()
	if err != nil {
		in.handleSpaceQueryError(err)
	}
	if err != nil {
		return nil, "Unknown error"
	}
	if space == nil {
		return nil, fmt.Sprintf("Space not found from %s", spaceURL)
	}
	if !space.IsNotEnded() {
		snapshot.EndedAt = space.EndTime()
	}
	snapshot.SpaceTitle = space.Title
	snapshot.ScheduledStartedAt = space.ScheduleStartTime()
	snapshot.StartedAt = space.StartTime()
	if err := snapshot.Save(); err != nil {
		log.Error(err)
		return nil, "Unknown error"
	}
	if space.IsGoingRunning() {
		in.runMonitor(&snapshot.TwitterSpaceSnapshots)
	}
	return snapshot, ""
}

func (in *SpaceManager) runMonitor(snapshot *database.TwitterSpaceSnapshots) {
	authorization, err := in.nextTwitterAuthorization(snapshot.SpaceID)
	if err != nil {
		log.Error(err)
		return
	}
	err = NewSpaceMonitor(authorization, snapshot).Run()
	if err != nil {
		log.Error(err)
		return
	}
}

func (in *SpaceManager) handleSpaceQueryError(spaceErr error) {
	if spaceErr == nil {
		return
	}
	if errors.Is(spaceErr, ErrorTwitterUnauthorized) {
		if err := in.authorization.Expire(); err != nil {
			log.Error(err)
		}
		authorization, err := in.nextTwitterAuthorization(defaultManagerSpaceID)
		if err != nil {
			log.Error(err)
			return
		}
		in.authorization = authorization
	}
	log.Error(spaceErr)
}

func (in *SpaceManager) endSnapshot(snapshot *database.TwitterSpaceSnapshots) {
	now := time.Now()
	snapshot.EndedAt = &now
	if err := snapshot.Update(); err != nil {
		log.Error(errors.WrapAndReport(err, "end snapshot"))
		return
	}
	log.Infof("Twitter snapshot %v ended", snapshot.SpaceURL)
}

func (in *SpaceManager) holdAuthorization(spaceID string, auth *database.TwitterWebAuthorization) error {
	heartbeats := database.TwitterWebAuthorizationHeartbeats{
		TwitterSpaceID:  spaceID,
		AuthorizationID: auth.ID,
	}
	return heartbeats.Beat()
}

func (in *SpaceManager) nextTwitterAuthorization(spaceID string) (*database.TwitterWebAuthorization, error) {
	var (
		ctx = context.TODO()
	)
	defer cache.Redis.Del(ctx, twitterSpaceManagerLockKey)
	for {
		locked, err := cache.Redis.SetNX(ctx, twitterSpaceManagerLockKey, 1, time.Minute).Result()
		if err != nil {
			return nil, errors.WrapAndReport(err, "lock twitter space manager")
		}
		if !locked {
			// 等待获取锁
			time.Sleep(time.Second)
			continue
		}

		authorizations, err := database.TwitterWebAuthorization{}.FindHeartbeatableAndNotExpired()
		if err != nil {
			return nil, err
		}
		if len(authorizations) == 0 {
			return nil, errors.NewWithReport("insufficient twitter web authorizations")
		}

		// 取第一个有效token
		authorization := authorizations[0]
		err = in.holdAuthorization(spaceID, authorization)
		if err != nil {
			return nil, err
		}
		return authorization, nil
	}
}

func SpaceIDFromURL(twitterURL string) string {
	spaceURL, err := url.Parse(twitterURL)
	if err != nil {
		log.Error(errors.WrapAndReport(err, fmt.Sprintf("parse url %v", twitterURL)))
		return ""
	}
	spacePath := strings.TrimSuffix(spaceURL.Path, "/")
	spacePath = strings.ReplaceAll(spacePath, " ", "")
	lastIdx := strings.LastIndex(spacePath, "/")
	return spacePath[lastIdx+1:]
}

func queryCampaignApps(campaigns []*database.Campaigns) (map[string]*database.WhiteLabelingApps, error) {
	var (
		appIdMapping = make(map[string]bool)
		appIds       []string
	)
	for _, campaign := range campaigns {
		appIdMapping[campaign.AppID] = true
	}
	for appId, _ := range appIdMapping {
		appIds = append(appIds, appId)
	}
	apps, err := database.WhiteLabelingApps{}.SelectApps(appIds)
	if err != nil {
		return nil, err
	}
	appMapping := make(map[string]*database.WhiteLabelingApps, 0)
	for _, app := range apps {
		appMapping[app.AppID] = app
	}
	return appMapping, nil
}
