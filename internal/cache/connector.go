package cache

import (
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/go-redis/redis/v8"
	"github.com/go-redis/redis_rate/v9"
	"moff.io/moff-social/internal/config"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strconv"
	"time"
)

type Tup struct {
	value    string
	expireAt int64
}

type Tup2 struct {
	value    int
	expireAt int64
}

var (
	Redis        *redis.Client
	RateLimiter  *redis_rate.Limiter
	channelCache map[string]Tup
	guildCache   map[string]Tup2
	userCache    map[string]Tup
)

func Init(cred *config.DBCredential) {
	db, _ := strconv.ParseInt(cred.Database, 10, 64)
	Redis = redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%v:%v", cred.Address, cred.Port),
		DB:   int(db),
	})
	if _, err := Redis.Ping(context.TODO()).Result(); err != nil {
		log.Fatalf("ping to redis:%v", err)
	}
	RateLimiter = redis_rate.NewLimiter(Redis)
	channelCache = make(map[string]Tup)
	guildCache = make(map[string]Tup2)
	userCache = make(map[string]Tup)
}

// GetOrUpdateChannelInfo assumes that channel ids are universally unique
func GetOrUpdateChannelInfo(s *discordgo.Session, chnId string) string {
	if chnId == "" {
		return ""
	}
	tup, ok := channelCache[chnId]
	if ok && tup.expireAt > time.Now().Unix() {
		return tup.value
	}
	chn, err := s.Channel(chnId)
	if err == nil {
		channelCache[chnId] = Tup{chn.Name, time.Now().Unix() + int64(60*60)}
		return chn.Name
	} else {
		log.Errorf("failed to fetch channel: %v, err: %v", chnId, err)
		return ""
	}
}

func GetOrUpdateGuildInfo(s *discordgo.Session, inviteCode string, guildId string) int {
	if guildId == "" || inviteCode == "" {
		return 0
	}
	tup, ok := guildCache[guildId]
	if ok && tup.expireAt > time.Now().Unix() {
		return tup.value
	}
	inv, err := s.InviteWithCounts(inviteCode)
	if err == nil {
		guildCache[guildId] = Tup2{inv.ApproximateMemberCount, time.Now().Unix() + int64(5*60)}
		return inv.ApproximateMemberCount
	} else {
		log.Errorf("failed to fetch guild members: %v, err: %v", inviteCode, err)
		return 0
	}
}

func GetOrUpdateUserInfo(s *discordgo.Session, userId string) string {
	tup, ok := userCache[userId]
	if ok && tup.expireAt > time.Now().Unix() {
		return tup.value
	}
	u, err := s.User(userId)
	if err == nil {
		userCache[userId] = Tup{u.Username, time.Now().Unix() + int64(60*60*6)}
		return u.Username
	} else {
		log.Errorf("failed to fetch user: %v, err: %v", userId, err)
		return ""
	}
}

func Close() {
	if Redis != nil {
		Redis.Close()
		Redis = nil
	}
}

func DeleteFromPrefix(prefix string) error {
	var (
		cursor uint64
		match        = fmt.Sprintf("%v*", prefix)
		ctx          = context.TODO()
		count  int64 = 200
	)
	log.Debugf("deleting cache pattern %v", match)
	for {
		keys, c, err := Redis.Scan(ctx, cursor, match, count).Result()
		if err != nil {
			return errors.WrapAndReport(err, "scan caches")
		}
		cursor = c
		if len(keys) > 0 {
			err = Redis.Del(ctx, keys...).Err()
			if err != nil {
				return errors.WrapAndReport(err, "delete caches")
			}
		}
		if c == 0 {
			return nil
		}
	}
}
