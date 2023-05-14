package errors

import (
	"sync"
	"time"
)

type rateLimiter struct {
	lock   sync.Mutex
	silent time.Duration
	buffer map[string]*errorStats
}

func newRateLimiter(silent time.Duration) *rateLimiter {
	return &rateLimiter{
		silent: silent,
		buffer: map[string]*errorStats{},
	}
}

type errorStats struct {
	// 总计的发生次数
	totalOccurCount int
	// 上次报告过后发生的次数
	occurCountSinceLastReport int
	// 最近上报时间
	lastReportTime *time.Time
}

func (in *errorStats) Copy() *errorStats {
	return &errorStats{
		totalOccurCount:           in.totalOccurCount,
		occurCountSinceLastReport: in.occurCountSinceLastReport,
		lastReportTime:            in.lastReportTime,
	}
}

func (b *rateLimiter) StackBasedRateLimited(stack string) (bool, *errorStats) {
	b.lock.Lock()
	defer b.lock.Unlock()
	stats := b.buffer[stack]
	if stats == nil {
		stats = &errorStats{}
		b.buffer[stack] = stats
	}
	cp := stats.Copy()
	// 未上报过，允许直接上报
	now := time.Now()
	if stats.lastReportTime == nil {
		stats.totalOccurCount++
		stats.occurCountSinceLastReport = 0
		stats.lastReportTime = &now
		return false, cp
	}
	// 上报过但不满足推迟时间
	if time.Since(*stats.lastReportTime) < b.silent {
		stats.totalOccurCount++
		stats.occurCountSinceLastReport++
		return true, cp
	}
	// 上报间隔已满足推迟时间
	stats.totalOccurCount++
	stats.occurCountSinceLastReport = 0
	stats.lastReportTime = &now
	return false, cp
}
