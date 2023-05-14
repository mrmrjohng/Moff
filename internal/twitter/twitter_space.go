package twitter

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"io/ioutil"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/csv"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Space struct {
	State              string
	Title              string
	ScheduledStartedAt int64
	StartedAt          int64
	UpdatedAt          int64
	Presences          []*SpacePresence
}

func (in *Space) IsNotEnded() bool {
	return in.State == SpaceNotStarted || in.State == SpacePrePublished || in.State == SpaceRunning
}

func (in *Space) IsRunning() bool {
	return in.State == SpaceRunning
}

func (in *Space) IsGoingRunning() bool {
	if in.ScheduledStartedAt == 0 {
		return false
	}
	leftMs := in.ScheduledStartedAt - time.Now().UnixMilli()
	return leftMs < 1000*60*30
}

func (in *Space) ScheduleStartTime() *time.Time {
	if in.ScheduledStartedAt == 0 {
		return nil
	}
	t := time.UnixMilli(in.ScheduledStartedAt)
	return &t
}

func (in *Space) StartTime() *time.Time {
	if in.StartedAt == 0 {
		return nil
	}
	t := time.UnixMilli(in.StartedAt)
	return &t
}

func (in *Space) EndTime() *time.Time {
	if in.UpdatedAt == 0 {
		now := time.Now()
		return &now
	}
	t := time.UnixMilli(in.UpdatedAt)
	return &t
}

type SpaceIdentity string

type SpacePresence struct {
	Identity  SpaceIdentity
	TwitterID string
	Since     int64
}

const (
	SpaceIdentityAdmin    = SpaceIdentity("admin")
	SpaceIdentitySpeaker  = SpaceIdentity("speaker")
	SpaceIdentityListener = SpaceIdentity("listener")
)

type SpaceParticipant struct {
	Presence   *SpacePresence
	PresenceMs int64
}

func (p *SpaceParticipant) Marshal() string {
	b, err := json.Marshal(p)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "marshal TwitterSpacePresence"))
	}
	return string(b)
}

type SpaceMonitor struct {
	authorization          *database.TwitterWebAuthorization
	authorizationHeartbeat *database.TwitterWebAuthorizationHeartbeats
	snapshot               *database.TwitterSpaceSnapshots

	spaceRequest      *http.Request
	spaceParticipants map[string]*SpaceParticipant
}

const (
	SpaceNotStarted     = "NotStarted"
	SpacePrePublished   = "PrePublished"
	SpaceCanceled       = "Canceled"
	SpaceRunning        = "Running"
	SpaceEnded          = "Ended"
	SpaceTimeout        = "TimedOut"
	twitterSpaceLockKey = "twitter_space_monitor:"
)

var (
	ErrorTwitterUnauthorized = errors.New("twitter web authorization expired")
)

func NewSpaceMonitor(authorization *database.TwitterWebAuthorization, snapshot *database.TwitterSpaceSnapshots) *SpaceMonitor {
	monitor := SpaceMonitor{
		authorization: authorization,
		authorizationHeartbeat: &database.TwitterWebAuthorizationHeartbeats{
			AuthorizationID: authorization.ID,
			TwitterSpaceID:  snapshot.SpaceID,
		},
		snapshot:          snapshot,
		spaceParticipants: make(map[string]*SpaceParticipant),
	}
	return &monitor
}

func (in *SpaceMonitor) buildSpaceRequest() (*http.Request, error) {
	url := "https://api.twitter.com/graphql/i3Y4qgl8Xth8VLHTbMC9Hw/AudioSpaceById?variables=%7B%22id%22%3A%22" + in.snapshot.SpaceID + "%22%2C%22isMetatagsQuery%22%3Atrue%2C%22withSuperFollowsUserFields%22%3Atrue%2C%22withDownvotePerspective%22%3Afalse%2C%22withReactionsMetadata%22%3Afalse%2C%22withReactionsPerspective%22%3Afalse%2C%22withSuperFollowsTweetFields%22%3Atrue%2C%22withReplays%22%3Atrue%7D&features=%7B%22spaces_2022_h2_clipping%22%3Atrue%2C%22spaces_2022_h2_spaces_communities%22%3Atrue%2C%22responsive_web_twitter_blue_verified_badge_is_enabled%22%3Atrue%2C%22responsive_web_graphql_exclude_directive_enabled%22%3Afalse%2C%22verified_phone_label_enabled%22%3Afalse%2C%22responsive_web_graphql_skip_user_profile_image_extensions_enabled%22%3Afalse%2C%22longform_notetweets_consumption_enabled%22%3Atrue%2C%22tweetypie_unmention_optimization_enabled%22%3Atrue%2C%22vibe_api_enabled%22%3Atrue%2C%22responsive_web_edit_tweet_api_enabled%22%3Atrue%2C%22graphql_is_translatable_rweb_tweet_is_translatable_enabled%22%3Atrue%2C%22view_counts_everywhere_api_enabled%22%3Atrue%2C%22freedom_of_speech_not_reach_appeal_label_enabled%22%3Afalse%2C%22standardized_nudges_misinfo%22%3Atrue%2C%22tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled%22%3Afalse%2C%22responsive_web_graphql_timeline_navigation_enabled%22%3Atrue%2C%22interactive_text_enabled%22%3Atrue%2C%22responsive_web_text_conversations_enabled%22%3Afalse%2C%22responsive_web_enhance_cards_enabled%22%3Afalse%7D"
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.WrapAndReport(err, "create new request")
	}
	request.Header.Set("referer", "https://twitter.com/i/spaces/"+in.snapshot.SpaceID)
	request.Header.Set("content-type", "application/json")
	request.Header.Set("authorization", in.authorization.Authorization)
	request.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/106.0.0.0 Safari/537.36")
	request.Header.Set("x-twitter-active-user", "yes")
	request.Header.Set("x-twitter-auth-type", "OAuth2Session")
	request.Header.Set("cookie", in.authorization.Cookies)
	request.Header.Set("x-csrf-token", in.authorization.CsrfToken)
	return request, nil
}

func (in *SpaceMonitor) Run() error {
	key := fmt.Sprintf("%v%v", twitterSpaceLockKey, in.snapshot.SpaceID)
	locked, err := cache.Redis.SetNX(context.TODO(), key, time.Now().UnixMilli(), time.Minute).Result()
	if err != nil {
		return errors.WrapAndReport(err, "lock twitter snapshot monitor")
	}
	if !locked {
		log.Warn(errors.ErrorfAndReport("Seems twitter %v snapshot running...", in.snapshot.SpaceID))
		return nil
	}
	participants, err := in.loadSpaceParticipants(in.snapshot.SpaceID)
	if err != nil {
		return err
	}
	in.spaceParticipants = participants
	go in.run()
	return nil
}

func (in *SpaceMonitor) loadSpaceParticipants(spaceID string) (map[string]*SpaceParticipant, error) {
	presenceKey := fmt.Sprintf("twitter_space_presence:%v", spaceID)
	result, err := cache.Redis.HGetAll(context.TODO(), presenceKey).Result()
	if err != nil {
		return nil, errors.WrapAndReport(err, "query cache twitter space participants")
	}
	presences := make(map[string]*SpaceParticipant)
	for twitterID, val := range result {
		var p SpaceParticipant
		if err := json.Unmarshal([]byte(val), &p); err != nil {
			panic(err)
		}
		presences[twitterID] = &p
	}
	return presences, nil
}

func (in *SpaceMonitor) shouldSelfDestruct() bool {
	count, err := database.TwitterSpaceOwnerships{}.SelectOwnerCount(in.snapshot.SpaceID)
	if err != nil {
		log.Error(err)
		return false
	}
	return count == 0
}

func (in *SpaceMonitor) IsMonitoring() (bool, error) {
	key := fmt.Sprintf("%v%v", twitterSpaceLockKey, in.snapshot.SpaceID)
	_, err := cache.Redis.Get(context.TODO(), key).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, errors.WrapAndReport(err, "query space monitoring")
	}
	return true, nil
}

func (in *SpaceMonitor) try2UnlockSpaceMonitor() {
	var (
		maxTry      = 3
		ctx         = context.TODO()
		presenceKey = fmt.Sprintf("twitter_space_presence:%v", in.snapshot.SpaceID)
		key         = fmt.Sprintf("%v%v", twitterSpaceLockKey, in.snapshot.SpaceID)
	)
	for i := 0; i < maxTry; i++ {
		cache.Redis.Expire(ctx, presenceKey, time.Hour*24*3)
		if err := cache.Redis.Del(ctx, key).Err(); err != nil {
			log.Error(errors.WrapAndReport(err, "unlock twitter space monitor"))
			continue
		}
		log.Infof("Unlocked twitter space monitor %v", in.snapshot.SpaceID)
		return
	}
}

func (in *SpaceMonitor) heartbeat(snapshot *database.TwitterSpaceSnapshots) {
	key := fmt.Sprintf("%v%v", twitterSpaceLockKey, snapshot.SpaceID)
	if err := cache.Redis.Set(context.TODO(), key, time.Now().UnixMilli(), time.Minute).Err(); err != nil {
		log.Error(errors.WrapAndReport(err, "monitor lock ttl"))
	}

	if err := in.authorizationHeartbeat.Beat(); err != nil {
		log.Error(err)
	}
}

func (in *SpaceMonitor) run() {
	defer in.try2UnlockSpaceMonitor()
	var (
		ticker                = time.NewTicker(time.Second * 10)
		logScheduledStartedAt bool
		shouldFinalize        bool
		snapshot              = in.snapshot
	)
	log.Infof("Twitter space %v snapshot monitor running...", snapshot.SpaceID)
	defer log.Infof("Twitter space %v snapshot monitor stopped...", snapshot.SpaceID)
	for {
		// 自检
		if in.shouldSelfDestruct() {
			log.Infof("Twitter space monitor self destructing as no owner")
			return
		}
		in.heartbeat(snapshot)
		// 获取space信息
		space, err := in.QueryTwitterSpace()
		if err != nil {
			if errors.Is(err, ErrorTwitterUnauthorized) {
				if err := in.authorization.Expire(); err != nil {
					log.Fatal(err)
				}
				authorization, err := NewSpaceManager().nextTwitterAuthorization(in.snapshot.SpaceID)
				if err != nil {
					log.Error(err)
					return
				}
				if authorization == nil {
					log.Fatal("Insufficient twitter web authorizations")
				}
				in.authorization = authorization
				in.spaceRequest = nil
			}
			log.Error(err)
			<-ticker.C
			continue
		}
		if space == nil {
			// 默认此时space id有问题
			log.Warnf("Terminating twitter space monitor as invalid Twitter space %v", snapshot.SpaceURL)
			return
		}
		switch space.State {
		case SpaceNotStarted:
			if space.ScheduledStartedAt > 0 && !logScheduledStartedAt {
				logScheduledStartedAt = true
				log.Infof("Space %v start at %v", snapshot.SpaceID, time.UnixMilli(space.ScheduledStartedAt))
			}
			<-ticker.C
			continue
		case SpaceEnded, SpaceCanceled, SpaceTimeout:
			if shouldFinalize {
				in.finalize()
				return
			}
			shouldFinalize = true
		case SpaceRunning:
			break
		default:
			log.Warnf("Twitter space unhandled status %v", space.State)
			<-ticker.C
			continue
		}

		in.recordSpaceStarted()
		log.Infof("Current twitter space %v participants %v", snapshot.SpaceID, len(space.Presences))
		var (
			currParticipants  = make(map[string]*SpaceParticipant)
			newParticipantNum int64
		)
		for _, p := range space.Presences {
			participant := &SpaceParticipant{Presence: p}
			currParticipants[p.TwitterID] = participant
			// 添加新增的参与者
			if in.spaceParticipants[p.TwitterID] == nil {
				newParticipantNum++
				in.spaceParticipants[p.TwitterID] = participant
				continue
			}
			// 既有参与者，检查是否退出后加入房间
			// 此处偷懒，未校验他们的start的值
			if in.spaceParticipants[p.TwitterID].Presence == nil {
				in.spaceParticipants[p.TwitterID].Presence = p
			}
		}

		//if newParticipantNum > 0 {
		//	log.Infof("New participants:%v", newParticipantNum)
		//}

		// 计算退出用户的参与时间
		now := time.Now().UnixMilli()
		for _, p := range in.spaceParticipants {
			if p.Presence == nil {
				continue
			}
			if currParticipants[p.Presence.TwitterID] != nil {
				continue
			}
			// 用户已退出，计算其参与时间
			p.PresenceMs += now - p.Presence.Since
			p.Presence = nil
		}
		// 执行缓存
		in.cacheSpaceParticipants(context.TODO(), snapshot.SpaceID, in.spaceParticipants)
		<-ticker.C
	}
}

func (in *SpaceMonitor) recordSpaceStarted() {
	if in.snapshot.StartedAt != nil {
		return
	}
	now := time.Now()
	in.snapshot.StartedAt = &now
	if err := in.snapshot.Update(); err != nil {
		log.Error(err)
		in.snapshot.StartedAt = nil
	}
}

func (in *SpaceMonitor) finalize() {
	in.calcUserPresences()
	in.writeToS3()
	in.writeWhitelists()
	now := time.Now()
	in.snapshot.TotalParticipants = len(in.spaceParticipants)
	in.snapshot.EndedAt = &now

	if err := in.snapshot.Update(); err != nil {
		log.Error(err)
	}
	log.Infof("Finalized twitter space %v", in.snapshot.SpaceID)
}

func (in *SpaceMonitor) calcUserPresences() {
	nowMs := time.Now().UnixMilli()
	for _, p := range in.spaceParticipants {
		if p.Presence != nil {
			p.PresenceMs += nowMs - p.Presence.Since
		}
	}
}

func (in *SpaceMonitor) writeToS3() {
	if len(in.spaceParticipants) == 0 {
		return
	}

	var (
		records = [][]string{
			{"twitter id", "seconds"},
		}
		now = time.Now()
	)
	for twitterID, p := range in.spaceParticipants {
		records = append(records, []string{twitterID, strconv.Itoa(int(p.PresenceMs / 1000))})
	}
	objectKey := fmt.Sprintf("community/whitelist/twitter/%v/%v/%v/%v.csv",
		now.Year(), now.Month(), now.Day(), in.snapshot.SpaceID)
	err := csv.WriteCsvAndUploadToS3(objectKey, records)
	if err != nil {
		log.Error(err)
		return
	}
	in.snapshot.ParticipantLink = fmt.Sprintf("https://public.moff.io/%s", objectKey)
}

func (in *SpaceMonitor) writeWhitelists() {
	if len(in.spaceParticipants) == 0 {
		return
	}

	owners, err := database.TwitterSpaceOwnerships{}.SelectSpaceOwners(in.snapshot.SpaceID)
	if err != nil {
		log.Error(err)
		return
	}
	var (
		whitelists  = make(map[string][]string)
		whitelistMs = make(map[string]int64)
	)
	for _, owner := range owners {
		if owner.CampaignWhitelistID == "" {
			continue
		}
		whitelists[owner.CampaignWhitelistID] = make([]string, 0)
		whitelistMs[owner.CampaignWhitelistID] = owner.SnapshotMinSeconds * 1000
	}
	// 计算白名单
	for twitterID, p := range in.spaceParticipants {
		for wid, _ := range whitelists {
			if p.PresenceMs < whitelistMs[wid] {
				continue
			}
			whitelists[wid] = append(whitelists[wid], twitterID)
		}
	}
	// 写入白名单
	err = database.PublicPostgres.Transaction(func(tx *gorm.DB) error {
		for wid, twitterIds := range whitelists {
			if len(twitterIds) == 0 {
				continue
			}
			var (
				batch     = make([]string, 0)
				batchSize = 2000
			)
			for i, tid := range twitterIds {
				batch = append(batch, tid)
				if len(batch) < batchSize && i < len(whitelists)-1 {
					continue
				}
				// 按批写入
				err := database.Whitelist{}.WriteTwitterIds(tx, wid, batch)
				if err != nil {
					return err
				}
				batch = make([]string, 0)
			}
		}
		return nil
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "write whitelists"))
	}
}

func (in *SpaceMonitor) cacheSpaceParticipants(ctx context.Context, spaceID string, participants map[string]*SpaceParticipant) {
	var (
		presenceKey    = fmt.Sprintf("twitter_space_presence:%v", spaceID)
		presenceValues []interface{}
	)
	for twitterID, p := range participants {
		presenceValues = append(presenceValues, twitterID, p.Marshal())
	}
	for i := 0; i < 3; i++ {
		err := cache.Redis.HMSet(ctx, presenceKey, presenceValues...).Err()
		if err != nil {
			log.Error(errors.WrapAndReport(err, "cache twitter space presence values"))
			continue
		}
		return
	}
	log.Error("Failed to cache twitter space presence values")
}

func (in *SpaceMonitor) QueryTwitterSpace() (*Space, error) {
	if in.spaceRequest == nil {
		request, err := in.buildSpaceRequest()
		if err != nil {
			return nil, err
		}
		in.spaceRequest = request
	}
	// 15分钟，500个请求, 即一个token最多支持5个twitter space的间隔10秒的请求
	response, err := http.DefaultClient.Do(in.spaceRequest)
	if err != nil {
		return nil, errors.WrapAndReport(err, "request to twitter space api")
	}

	defer response.Body.Close()

	// 判断当前是否已限流
	rateLimit := response.Header.Get("x-rate-limit-remaining")
	if rateLimit == "0" {
		resetAtStr := response.Header.Get("x-rate-limit-reset")
		resetAt, err := strconv.ParseInt(resetAtStr, 10, 64)
		if err != nil {
			return nil, errors.WrapAndReport(err, "parse reset at")
		}
		seconds := resetAt - time.Now().Unix()
		time.Sleep(time.Second * time.Duration(seconds))
		return nil, errors.WrapAndReport(err, "rate limited")
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, errors.WrapAndReport(err, "read twitter space response")
	}
	e := database.TwitterSpaceBackups{
		SpaceID:     in.snapshot.SpaceID,
		Response:    string(body),
		CreatedTime: time.Now().UTC(),
	}.Create()
	if e != nil {
		log.Error(err)
	}
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusUnauthorized {
			return nil, ErrorTwitterUnauthorized
		}
		return nil, errors.ErrorfAndReport("twitter space response status %v:%v", response.Status, string(body))
	}
	if strings.Contains(string(body), "BadRequest") {
		log.Warn(string(body))
		return nil, nil
	}
	var space spaceResponse
	if err := json.Unmarshal(body, &space); err != nil {
		return nil, errors.WrapAndReport(err, "unmarshal twitter participants")
	}
	return in.calcTwitterSpaceParticipants(&space), nil
}

func (in *SpaceMonitor) calcTwitterSpaceParticipants(space *spaceResponse) *Space {
	var presences []*SpacePresence
	admins := space.Data.AudioSpace.Participants.Admins
	speakers := space.Data.AudioSpace.Participants.Speakers
	listeners := space.Data.AudioSpace.Participants.Listeners
	now := time.Now().UnixMilli()
	for _, admin := range admins {
		presence := &SpacePresence{
			Identity:  SpaceIdentityAdmin,
			TwitterID: firstNonDefault(admin.User.RestID, admin.UserResults.RestID),
			Since:     now,
		}
		if admin.Start > 0 {
			presence.Since = admin.Start
		}
		presences = append(presences, presence)
	}
	for _, speaker := range speakers {
		presence := &SpacePresence{
			Identity:  SpaceIdentitySpeaker,
			TwitterID: firstNonDefault(speaker.User.RestID, speaker.UserResults.RestID),
			Since:     now,
		}
		if speaker.Start > 0 {
			presence.Since = speaker.Start
		}
		presences = append(presences, presence)
	}
	for _, listener := range listeners {
		presence := &SpacePresence{
			Identity:  SpaceIdentityListener,
			TwitterID: firstNonDefault(listener.User.RestID, listener.UserResults.RestID),
			Since:     now,
		}
		if listener.Start > 0 {
			presence.Since = listener.Start
		}
		presences = append(presences, presence)
	}
	return &Space{
		State:              space.Data.AudioSpace.Metadata.State,
		Title:              space.Data.AudioSpace.Metadata.Title,
		ScheduledStartedAt: space.Data.AudioSpace.Metadata.ScheduledStartedAt,
		StartedAt:          space.Data.AudioSpace.Metadata.StartedAt,
		UpdatedAt:          space.Data.AudioSpace.Metadata.UpdatedAt,
		Presences:          presences,
	}
}

type spaceResponse struct {
	Data struct {
		AudioSpace struct {
			Metadata struct {
				RestID                      string `json:"rest_id"`
				State                       string `json:"state"`
				Title                       string `json:"title"`
				MediaKey                    string `json:"media_key"`
				ScheduledStartedAt          int64  `json:"scheduled_start"`
				CreatedAt                   int64  `json:"created_at"`
				StartedAt                   int64  `json:"started_at"`
				ReplayStartTime             int    `json:"replay_start_time"`
				UpdatedAt                   int64  `json:"updated_at"`
				DisallowJoin                bool   `json:"disallow_join"`
				NarrowCastSpaceType         int    `json:"narrow_cast_space_type"`
				IsEmployeeOnly              bool   `json:"is_employee_only"`
				IsLocked                    bool   `json:"is_locked"`
				IsSpaceAvailableForReplay   bool   `json:"is_space_available_for_replay"`
				IsSpaceAvailableForClipping bool   `json:"is_space_available_for_clipping"`
				ConversationControls        int    `json:"conversation_controls"`
				TotalReplayWatched          int    `json:"total_replay_watched"`
				TotalLiveListeners          int    `json:"total_live_listeners"`
				CreatorResults              struct {
					RestID string `json:"rest_id"`
					Result struct {
						Typename                   string `json:"__typename"`
						ID                         string `json:"id"`
						RestID                     string `json:"rest_id"`
						AffiliatesHighlightedLabel struct {
						} `json:"affiliates_highlighted_label"`
						HasNftAvatar bool `json:"has_nft_avatar"`
						Legacy       struct {
							BlockedBy           bool   `json:"blocked_by"`
							Blocking            bool   `json:"blocking"`
							CanDm               bool   `json:"can_dm"`
							CanMediaTag         bool   `json:"can_media_tag"`
							CreatedAt           string `json:"created_at"`
							DefaultProfile      bool   `json:"default_profile"`
							DefaultProfileImage bool   `json:"default_profile_image"`
							Description         string `json:"description"`
							Entities            struct {
								Description struct {
									Urls []interface{} `json:"urls"`
								} `json:"description"`
								URL struct {
									Urls []struct {
										DisplayURL  string `json:"display_url"`
										ExpandedURL string `json:"expanded_url"`
										URL         string `json:"url"`
										Indices     []int  `json:"indices"`
									} `json:"urls"`
								} `json:"url"`
							} `json:"entities"`
							FastFollowersCount     int           `json:"fast_followers_count"`
							FavouritesCount        int           `json:"favourites_count"`
							FollowRequestSent      bool          `json:"follow_request_sent"`
							FollowedBy             bool          `json:"followed_by"`
							FollowersCount         int           `json:"followers_count"`
							Following              bool          `json:"following"`
							FriendsCount           int           `json:"friends_count"`
							HasCustomTimelines     bool          `json:"has_custom_timelines"`
							IsTranslator           bool          `json:"is_translator"`
							ListedCount            int           `json:"listed_count"`
							Location               string        `json:"location"`
							MediaCount             int           `json:"media_count"`
							Muting                 bool          `json:"muting"`
							Name                   string        `json:"name"`
							NormalFollowersCount   int           `json:"normal_followers_count"`
							Notifications          bool          `json:"notifications"`
							PinnedTweetIdsStr      []interface{} `json:"pinned_tweet_ids_str"`
							PossiblySensitive      bool          `json:"possibly_sensitive"`
							ProfileImageExtensions struct {
								MediaColor struct {
									R struct {
										Ok struct {
											Palette []struct {
												Percentage float64 `json:"percentage"`
												Rgb        struct {
													Blue  int `json:"blue"`
													Green int `json:"green"`
													Red   int `json:"red"`
												} `json:"rgb"`
											} `json:"palette"`
										} `json:"ok"`
									} `json:"r"`
								} `json:"mediaColor"`
							} `json:"profile_image_extensions"`
							ProfileImageURLHTTPS    string        `json:"profile_image_url_https"`
							ProfileInterstitialType string        `json:"profile_interstitial_type"`
							Protected               bool          `json:"protected"`
							ScreenName              string        `json:"screen_name"`
							StatusesCount           int           `json:"statuses_count"`
							TranslatorType          string        `json:"translator_type"`
							URL                     string        `json:"url"`
							Verified                bool          `json:"verified"`
							WantRetweets            bool          `json:"want_retweets"`
							WithheldInCountries     []interface{} `json:"withheld_in_countries"`
						} `json:"legacy"`
						Professional struct {
							RestID           string `json:"rest_id"`
							ProfessionalType string `json:"professional_type"`
							Category         []struct {
								ID       int    `json:"id"`
								Name     string `json:"name"`
								IconName string `json:"icon_name"`
							} `json:"category"`
						} `json:"professional"`
						SuperFollowEligible bool `json:"super_follow_eligible"`
						SuperFollowedBy     bool `json:"super_followed_by"`
						SuperFollowing      bool `json:"super_following"`
					} `json:"result"`
				} `json:"creator_results"`
			} `json:"metadata"`
			IsSubscribed bool `json:"is_subscribed"`
			Sharings     struct {
				Items []struct {
					SharingID   string `json:"sharing_id"`
					CreatedAtMs int64  `json:"created_at_ms"`
					UpdatedAtMs int64  `json:"updated_at_ms"`
					UserResults struct {
						RestID string `json:"rest_id"`
						Result struct {
							Typename                   string `json:"__typename"`
							ID                         string `json:"id"`
							RestID                     string `json:"rest_id"`
							AffiliatesHighlightedLabel struct {
							} `json:"affiliates_highlighted_label"`
							HasNftAvatar bool `json:"has_nft_avatar"`
							Legacy       struct {
								BlockedBy           bool   `json:"blocked_by"`
								Blocking            bool   `json:"blocking"`
								CanDm               bool   `json:"can_dm"`
								CanMediaTag         bool   `json:"can_media_tag"`
								CreatedAt           string `json:"created_at"`
								DefaultProfile      bool   `json:"default_profile"`
								DefaultProfileImage bool   `json:"default_profile_image"`
								Description         string `json:"description"`
								Entities            struct {
									Description struct {
										Urls []interface{} `json:"urls"`
									} `json:"description"`
									URL struct {
										Urls []struct {
											DisplayURL  string `json:"display_url"`
											ExpandedURL string `json:"expanded_url"`
											URL         string `json:"url"`
											Indices     []int  `json:"indices"`
										} `json:"urls"`
									} `json:"url"`
								} `json:"entities"`
								FastFollowersCount     int           `json:"fast_followers_count"`
								FavouritesCount        int           `json:"favourites_count"`
								FollowRequestSent      bool          `json:"follow_request_sent"`
								FollowedBy             bool          `json:"followed_by"`
								FollowersCount         int           `json:"followers_count"`
								Following              bool          `json:"following"`
								FriendsCount           int           `json:"friends_count"`
								HasCustomTimelines     bool          `json:"has_custom_timelines"`
								IsTranslator           bool          `json:"is_translator"`
								ListedCount            int           `json:"listed_count"`
								Location               string        `json:"location"`
								MediaCount             int           `json:"media_count"`
								Muting                 bool          `json:"muting"`
								Name                   string        `json:"name"`
								NormalFollowersCount   int           `json:"normal_followers_count"`
								Notifications          bool          `json:"notifications"`
								PinnedTweetIdsStr      []interface{} `json:"pinned_tweet_ids_str"`
								PossiblySensitive      bool          `json:"possibly_sensitive"`
								ProfileImageExtensions struct {
									MediaColor struct {
										R struct {
											Ok struct {
												Palette []struct {
													Percentage float64 `json:"percentage"`
													Rgb        struct {
														Blue  int `json:"blue"`
														Green int `json:"green"`
														Red   int `json:"red"`
													} `json:"rgb"`
												} `json:"palette"`
											} `json:"ok"`
										} `json:"r"`
									} `json:"mediaColor"`
								} `json:"profile_image_extensions"`
								ProfileImageURLHTTPS    string        `json:"profile_image_url_https"`
								ProfileInterstitialType string        `json:"profile_interstitial_type"`
								Protected               bool          `json:"protected"`
								ScreenName              string        `json:"screen_name"`
								StatusesCount           int           `json:"statuses_count"`
								TranslatorType          string        `json:"translator_type"`
								URL                     string        `json:"url"`
								Verified                bool          `json:"verified"`
								WantRetweets            bool          `json:"want_retweets"`
								WithheldInCountries     []interface{} `json:"withheld_in_countries"`
							} `json:"legacy"`
							Professional struct {
								RestID           string `json:"rest_id"`
								ProfessionalType string `json:"professional_type"`
								Category         []struct {
									ID       int    `json:"id"`
									Name     string `json:"name"`
									IconName string `json:"icon_name"`
								} `json:"category"`
							} `json:"professional"`
							SuperFollowEligible bool `json:"super_follow_eligible"`
							SuperFollowedBy     bool `json:"super_followed_by"`
							SuperFollowing      bool `json:"super_following"`
						} `json:"result"`
					} `json:"user_results"`
					SharedItem struct {
						Typename     string `json:"__typename"`
						TweetResults struct {
							Result struct {
								Typename string `json:"__typename"`
								RestID   string `json:"rest_id"`
								Core     struct {
									UserResults struct {
										RestID string `json:"rest_id"`
										Result struct {
											Typename                   string `json:"__typename"`
											ID                         string `json:"id"`
											RestID                     string `json:"rest_id"`
											AffiliatesHighlightedLabel struct {
											} `json:"affiliates_highlighted_label"`
											HasNftAvatar bool `json:"has_nft_avatar"`
											Legacy       struct {
												BlockedBy           bool   `json:"blocked_by"`
												Blocking            bool   `json:"blocking"`
												CanDm               bool   `json:"can_dm"`
												CanMediaTag         bool   `json:"can_media_tag"`
												CreatedAt           string `json:"created_at"`
												DefaultProfile      bool   `json:"default_profile"`
												DefaultProfileImage bool   `json:"default_profile_image"`
												Description         string `json:"description"`
												Entities            struct {
													Description struct {
														Urls []interface{} `json:"urls"`
													} `json:"description"`
													URL struct {
														Urls []struct {
															DisplayURL  string `json:"display_url"`
															ExpandedURL string `json:"expanded_url"`
															URL         string `json:"url"`
															Indices     []int  `json:"indices"`
														} `json:"urls"`
													} `json:"url"`
												} `json:"entities"`
												FastFollowersCount      int           `json:"fast_followers_count"`
												FavouritesCount         int           `json:"favourites_count"`
												FollowRequestSent       bool          `json:"follow_request_sent"`
												FollowedBy              bool          `json:"followed_by"`
												FollowersCount          int           `json:"followers_count"`
												Following               bool          `json:"following"`
												FriendsCount            int           `json:"friends_count"`
												HasCustomTimelines      bool          `json:"has_custom_timelines"`
												IsTranslator            bool          `json:"is_translator"`
												ListedCount             int           `json:"listed_count"`
												Location                string        `json:"location"`
												MediaCount              int           `json:"media_count"`
												Muting                  bool          `json:"muting"`
												Name                    string        `json:"name"`
												NormalFollowersCount    int           `json:"normal_followers_count"`
												Notifications           bool          `json:"notifications"`
												PinnedTweetIdsStr       []interface{} `json:"pinned_tweet_ids_str"`
												PossiblySensitive       bool          `json:"possibly_sensitive"`
												ProfileBannerExtensions struct {
													MediaColor struct {
														R struct {
															Ok struct {
																Palette []struct {
																	Percentage float64 `json:"percentage"`
																	Rgb        struct {
																		Blue  int `json:"blue"`
																		Green int `json:"green"`
																		Red   int `json:"red"`
																	} `json:"rgb"`
																} `json:"palette"`
															} `json:"ok"`
														} `json:"r"`
													} `json:"mediaColor"`
												} `json:"profile_banner_extensions"`
												ProfileBannerURL       string `json:"profile_banner_url"`
												ProfileImageExtensions struct {
													MediaColor struct {
														R struct {
															Ok struct {
																Palette []struct {
																	Percentage float64 `json:"percentage"`
																	Rgb        struct {
																		Blue  int `json:"blue"`
																		Green int `json:"green"`
																		Red   int `json:"red"`
																	} `json:"rgb"`
																} `json:"palette"`
															} `json:"ok"`
														} `json:"r"`
													} `json:"mediaColor"`
												} `json:"profile_image_extensions"`
												ProfileImageURLHTTPS    string        `json:"profile_image_url_https"`
												ProfileInterstitialType string        `json:"profile_interstitial_type"`
												Protected               bool          `json:"protected"`
												ScreenName              string        `json:"screen_name"`
												StatusesCount           int           `json:"statuses_count"`
												TranslatorType          string        `json:"translator_type"`
												URL                     string        `json:"url"`
												Verified                bool          `json:"verified"`
												WantRetweets            bool          `json:"want_retweets"`
												WithheldInCountries     []interface{} `json:"withheld_in_countries"`
											} `json:"legacy"`
											Professional struct {
												RestID           string `json:"rest_id"`
												ProfessionalType string `json:"professional_type"`
												Category         []struct {
													ID       int    `json:"id"`
													Name     string `json:"name"`
													IconName string `json:"icon_name"`
												} `json:"category"`
											} `json:"professional"`
											SuperFollowEligible bool `json:"super_follow_eligible"`
											SuperFollowedBy     bool `json:"super_followed_by"`
											SuperFollowing      bool `json:"super_following"`
										} `json:"result"`
									} `json:"user_results"`
								} `json:"core"`
								UnmentionData struct {
								} `json:"unmention_data"`
								EditControl struct {
									EditTweetIds       []string `json:"edit_tweet_ids"`
									EditableUntilMsecs string   `json:"editable_until_msecs"`
									IsEditEligible     bool     `json:"is_edit_eligible"`
									EditsRemaining     string   `json:"edits_remaining"`
								} `json:"edit_control"`
								EditPerspective struct {
									Favorited bool `json:"favorited"`
									Retweeted bool `json:"retweeted"`
								} `json:"edit_perspective"`
								IsTranslatable bool `json:"is_translatable"`
								Legacy         struct {
									CreatedAt         string `json:"created_at"`
									ConversationIDStr string `json:"conversation_id_str"`
									DisplayTextRange  []int  `json:"display_text_range"`
									Entities          struct {
										Media []struct {
											DisplayURL    string `json:"display_url"`
											ExpandedURL   string `json:"expanded_url"`
											IDStr         string `json:"id_str"`
											Indices       []int  `json:"indices"`
											MediaURLHTTPS string `json:"media_url_https"`
											Type          string `json:"type"`
											URL           string `json:"url"`
											Features      struct {
												Large struct {
													Faces []interface{} `json:"faces"`
												} `json:"large"`
												Medium struct {
													Faces []interface{} `json:"faces"`
												} `json:"medium"`
												Small struct {
													Faces []interface{} `json:"faces"`
												} `json:"small"`
												Orig struct {
													Faces []interface{} `json:"faces"`
												} `json:"orig"`
											} `json:"features"`
											Sizes struct {
												Large struct {
													H      int    `json:"h"`
													W      int    `json:"w"`
													Resize string `json:"resize"`
												} `json:"large"`
												Medium struct {
													H      int    `json:"h"`
													W      int    `json:"w"`
													Resize string `json:"resize"`
												} `json:"medium"`
												Small struct {
													H      int    `json:"h"`
													W      int    `json:"w"`
													Resize string `json:"resize"`
												} `json:"small"`
												Thumb struct {
													H      int    `json:"h"`
													W      int    `json:"w"`
													Resize string `json:"resize"`
												} `json:"thumb"`
											} `json:"sizes"`
											OriginalInfo struct {
												Height     int `json:"height"`
												Width      int `json:"width"`
												FocusRects []struct {
													X int `json:"x"`
													Y int `json:"y"`
													W int `json:"w"`
													H int `json:"h"`
												} `json:"focus_rects"`
											} `json:"original_info"`
										} `json:"media"`
										UserMentions []interface{} `json:"user_mentions"`
										Urls         []interface{} `json:"urls"`
										Hashtags     []interface{} `json:"hashtags"`
										Symbols      []interface{} `json:"symbols"`
									} `json:"entities"`
									ExtendedEntities struct {
										Media []struct {
											DisplayURL    string `json:"display_url"`
											ExpandedURL   string `json:"expanded_url"`
											IDStr         string `json:"id_str"`
											Indices       []int  `json:"indices"`
											MediaKey      string `json:"media_key"`
											MediaURLHTTPS string `json:"media_url_https"`
											Type          string `json:"type"`
											URL           string `json:"url"`
											ExtMediaColor struct {
												Palette []struct {
													Percentage float64 `json:"percentage"`
													Rgb        struct {
														Blue  int `json:"blue"`
														Green int `json:"green"`
														Red   int `json:"red"`
													} `json:"rgb"`
												} `json:"palette"`
											} `json:"ext_media_color"`
											ExtMediaAvailability struct {
												Status string `json:"status"`
											} `json:"ext_media_availability"`
											Features struct {
												Large struct {
													Faces []interface{} `json:"faces"`
												} `json:"large"`
												Medium struct {
													Faces []interface{} `json:"faces"`
												} `json:"medium"`
												Small struct {
													Faces []interface{} `json:"faces"`
												} `json:"small"`
												Orig struct {
													Faces []interface{} `json:"faces"`
												} `json:"orig"`
											} `json:"features"`
											Sizes struct {
												Large struct {
													H      int    `json:"h"`
													W      int    `json:"w"`
													Resize string `json:"resize"`
												} `json:"large"`
												Medium struct {
													H      int    `json:"h"`
													W      int    `json:"w"`
													Resize string `json:"resize"`
												} `json:"medium"`
												Small struct {
													H      int    `json:"h"`
													W      int    `json:"w"`
													Resize string `json:"resize"`
												} `json:"small"`
												Thumb struct {
													H      int    `json:"h"`
													W      int    `json:"w"`
													Resize string `json:"resize"`
												} `json:"thumb"`
											} `json:"sizes"`
											OriginalInfo struct {
												Height     int `json:"height"`
												Width      int `json:"width"`
												FocusRects []struct {
													X int `json:"x"`
													Y int `json:"y"`
													W int `json:"w"`
													H int `json:"h"`
												} `json:"focus_rects"`
											} `json:"original_info"`
										} `json:"media"`
									} `json:"extended_entities"`
									FavoriteCount             int    `json:"favorite_count"`
									Favorited                 bool   `json:"favorited"`
									FullText                  string `json:"full_text"`
									IsQuoteStatus             bool   `json:"is_quote_status"`
									Lang                      string `json:"lang"`
									PossiblySensitive         bool   `json:"possibly_sensitive"`
									PossiblySensitiveEditable bool   `json:"possibly_sensitive_editable"`
									QuoteCount                int    `json:"quote_count"`
									ReplyCount                int    `json:"reply_count"`
									RetweetCount              int    `json:"retweet_count"`
									Retweeted                 bool   `json:"retweeted"`
									Source                    string `json:"source"`
									UserIDStr                 string `json:"user_id_str"`
									IDStr                     string `json:"id_str"`
								} `json:"legacy"`
							} `json:"result"`
						} `json:"tweet_results"`
					} `json:"shared_item"`
				} `json:"items"`
				SliceInfo struct {
				} `json:"slice_info"`
			} `json:"sharings"`
			Participants struct {
				Total  int `json:"total"`
				Admins []struct {
					PeriscopeUserID   string `json:"periscope_user_id"`
					Start             int64  `json:"start"`
					TwitterScreenName string `json:"twitter_screen_name"`
					DisplayName       string `json:"display_name"`
					AvatarURL         string `json:"avatar_url"`
					IsVerified        bool   `json:"is_verified"`
					IsMutedByAdmin    bool   `json:"is_muted_by_admin"`
					IsMutedByGuest    bool   `json:"is_muted_by_guest"`
					UserResults       struct {
						RestID string `json:"rest_id"`
						Result struct {
							Typename     string `json:"__typename"`
							HasNftAvatar bool   `json:"has_nft_avatar"`
						} `json:"result"`
					} `json:"user_results"`
					User struct {
						RestID string `json:"rest_id"`
					} `json:"user"`
				} `json:"admins"`
				Speakers []struct {
					PeriscopeUserID   string `json:"periscope_user_id"`
					Start             int64  `json:"start"`
					TwitterScreenName string `json:"twitter_screen_name"`
					DisplayName       string `json:"display_name"`
					AvatarURL         string `json:"avatar_url"`
					IsVerified        bool   `json:"is_verified"`
					IsMutedByAdmin    bool   `json:"is_muted_by_admin"`
					IsMutedByGuest    bool   `json:"is_muted_by_guest"`
					UserResults       struct {
						RestID string `json:"rest_id"`
						Result struct {
							Typename     string `json:"__typename"`
							HasNftAvatar bool   `json:"has_nft_avatar"`
						} `json:"result"`
					} `json:"user_results"`
					User struct {
						RestID string `json:"rest_id"`
					} `json:"user"`
				} `json:"speakers"`
				Listeners []struct {
					PeriscopeUserID   string `json:"periscope_user_id"`
					TwitterScreenName string `json:"twitter_screen_name"`
					DisplayName       string `json:"display_name"`
					AvatarURL         string `json:"avatar_url"`
					IsVerified        bool   `json:"is_verified"`
					UserResults       struct {
						Result struct {
							Typename       string `json:"__typename"`
							HasNftAvatar   bool   `json:"has_nft_avatar"`
							IsBlueVerified bool   `json:"is_blue_verified"`
						} `json:"result"`
						RestID string `json:"rest_id"`
					} `json:"user_results"`
					User struct {
						RestID string `json:"rest_id"`
					} `json:"user"`
					Start          int64 `json:"start,omitempty"`
					IsMutedByAdmin bool  `json:"is_muted_by_admin,omitempty"`
					IsMutedByGuest bool  `json:"is_muted_by_guest,omitempty"`
				} `json:"listeners"`
			} `json:"participants"`
		} `json:"audioSpace"`
	} `json:"data"`
}

func firstNonDefault(vals ...string) string {
	for _, val := range vals {
		if val != "" {
			return val
		}
	}
	log.Warn("First non default value not found")
	return ""
}
