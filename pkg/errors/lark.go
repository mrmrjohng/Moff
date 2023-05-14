package errors

import (
	"fmt"
	"github.com/go-lark/lark"
	"moff.io/moff-social/pkg/log"
	"time"
)

type larkReporter struct {
	bot   *lark.Bot
	delay *rateLimiter
}

func NewLarkReporter(webhook string, silent time.Duration) {
	if webhook == "" {
		log.Warn("empty lark webhook found, skipping lark reporter initialization.")
		return
	}
	larkReporter := &larkReporter{
		bot:   lark.NewNotificationBot(webhook),
		delay: newRateLimiter(silent),
	}
	reporters = append(reporters, larkReporter)
	log.Info("Lark error reporter initialized.")
}

func (r *larkReporter) Report(err error) {
	if err == nil {
		return
	}
	stacks := callers().fullStack()
	limited, stats := r.delay.StackBasedRateLimited(stacks[2])
	if limited {
		return
	}
	pb := lark.NewPostBuilder()
	pb.Title("Discord-bot error")
	pb.TextTag(fmt.Sprintf("Last Report: %v", formatReportTime(stats.lastReportTime)), 1, true)
	pb.TextTag(fmt.Sprintf("\nError Count Since Last Report: %v", stats.occurCountSinceLastReport), 1, true)
	pb.TextTag(fmt.Sprintf("\nMessage: %v", err.Error()), 1, true)
	pb.TextTag("\nStacks:", 1, true)
	for _, s := range stacks {
		pb.TextTag(fmt.Sprintf("\n    %s", s), 1, true)
	}
	if _, err := r.bot.PostNotificationV2(lark.OutcomingMessage{
		MsgType: "post",
		Content: lark.MessageContent{
			Post: pb.Render(),
		},
	}); err != nil {
		log.Error(WithStack(err))
	}
}

func formatReportTime(time *time.Time) string {
	if time == nil {
		return "none"
	}
	return time.Format("2006.01.02 15:04")
}
