package errors

import (
	"bytes"
	"github.com/certifi/gocertifi"
	"github.com/getsentry/sentry-go"
	"moff.io/moff-social/pkg/errors/reporter"
	"moff.io/moff-social/pkg/log"
	"os"
	"strconv"
	"time"
)

var (
	reporters []Reporter
)

func init() {
	reporters = make([]Reporter, 0)
	if os.Getenv(debugMode) == "" {
		log.Info("Env DEBUG not set, report errors enabled.")
	} else {
		log.Info("Env DEBUG set, report errors disabled.")
	}
}

func report(err error) {
	if reporters == nil || err == nil {
		return
	}
	if os.Getenv(debugMode) != "" {
		return
	}
	for _, r := range reporters {
		r.Report(err)
	}
}

// Reporter 错误报告器
type Reporter interface {
	Report(error)
}

type sentryReporter struct {
}

func (s *sentryReporter) Report(err error) {
	sentry.CaptureException(err)
}

// 设置该变量，则sentry不会上报
const debugMode = "DEBUG"

// NewSentryReporter
// 初始化错误sentry报告器
// 当使用本公共包构建带report的报告时，会上报错误至已创建的sentry仓库.
// 环境变量DEBUG不为空时，不会产生错误上报
func NewSentryReporter(sentryDSN string) error {
	if sentryDSN == "" {
		log.Warn("empty DSN found, skipping sentry reporter initialization.")
		return nil
	}
	sentryClientOptions := sentry.ClientOptions{
		Dsn: sentryDSN,
	}

	rootCAs, err := gocertifi.CACerts()
	if err != nil {
		return Wrap(err, "init sentry CA")
	}

	sentryClientOptions.CaCerts = rootCAs
	err = sentry.Init(sentryClientOptions)
	if err != nil {
		return Wrap(err, "init sentry")
	}
	log.Info("sentry error reporter initialized.")
	reporters = append(reporters, &sentryReporter{})
	return nil
}

type dingTalkRobotReporter struct {
	limiter *rateLimiter
	reporter.DingTalkRobot
}

// NewDingTalkReporter
// 初始化钉钉机器人上报错误至指定的webhook
// 环境变量DEBUG不为空时，不会产生错误上报
func NewDingTalkReporter(webhook, secret string, reportDelay time.Duration) {
	if webhook == "" {
		log.Warn("empty dingtalk webhook found, skipping dingtalk reporter initialization.")
		return
	}
	robot := reporter.NewDingTalkRobot(webhook).WithSecret(secret)
	reporters = append(reporters, &dingTalkRobotReporter{limiter: newRateLimiter(reportDelay), DingTalkRobot: robot})
	log.Info("dingtalk error reporter initialized.")
}

const (
	errorField  = "error: "
	stacksField = "\nstacks:\n"
	breakline   = "\n"
	indent      = "	"
)

func (r *dingTalkRobotReporter) Report(err error) {
	if err == nil {
		return
	}
	stacks := callers().fullStack()
	limited, stats := r.limiter.StackBasedRateLimited(stacks[2])
	if limited {
		return
	}
	var content bytes.Buffer
	content.WriteString("last report:")
	content.WriteString(stats.lastReportTime.Format("2006.01.02 15:04"))
	content.WriteString(breakline)
	content.WriteString("occur since last report:")
	content.WriteString(strconv.Itoa(stats.occurCountSinceLastReport))
	content.WriteString(breakline)
	content.WriteString(errorField)
	content.WriteString(err.Error())
	content.WriteString(stacksField)
	for _, s := range stacks {
		content.WriteString(indent)
		content.WriteString(s)
		content.WriteString(breakline)
	}
	if err := r.SendText(content.String(), nil, true); err != nil {
		log.Info(WithStack(err))
	}
}
