package log

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/t-tomalak/logrus-easy-formatter"
	"os"
)

var logger *customLogger

// nolint:gochecknoinits
func init() {
	logger = newLogger()
}

type customLogger struct {
	*logrus.Logger
}

// SetLevel
// Set log level:
// DebugLevel = 0
// InfoLevel = 1
// WarnLevel = 2
// ErrorLevel = 3
func SetLevel(lvl int) {
	switch lvl {
	case 0:
		Info("log level set to DEBUG.")
		logger.Level = logrus.DebugLevel
	case 1:
		Info("log level set to INFO.")
		logger.Level = logrus.InfoLevel
	case 2:
		Info("log level set to WARN.")
		logger.Level = logrus.WarnLevel
	case 3:
		Info("log level set to ERROR.")
		logger.Level = logrus.ErrorLevel
	default:
		Info("log level set to INFO.")
		logger.Level = logrus.InfoLevel
	}
}

func newLogger() *customLogger {
	logger := &logrus.Logger{
		Out:   os.Stderr,
		Level: logrus.InfoLevel,
		Formatter: &easy.Formatter{
			TimestampFormat: "01-02 15:04:05.000",
			LogFormat:       "[%lvl%]   [%time%]   -   %msg%\r\n",
		},
	}
	return &customLogger{logger}
}

// Debug
func Debug(content interface{}) {
	logger.Debug(content)
}

// Debugf
func Debugf(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)
	logger.Debugf(content)
}

// Info
func Info(content interface{}) {
	logger.Info(content)
}

// Infof
func Infof(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)
	logger.Infof(content)
}

// Warn
func Warn(content interface{}) {
	logger.Warn(content)
}

// Warnf
func Warnf(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)
	logger.Warnf(content)
}

// Error
func Error(content interface{}) {
	logger.Error(content)
}

// Errorf
func Errorf(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)
	logger.Errorf(content)
}

// Fatal
func Fatal(content interface{}) {
	logger.Fatal(content)
}

// Fatalf
func Fatalf(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)
	logger.Fatalf(content)
}
