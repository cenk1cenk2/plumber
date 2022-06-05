package plumber

import "github.com/sirupsen/logrus"

type (
	LogLevel = logrus.Level
)

const (
	LOG_LEVEL_PANIC LogLevel = logrus.PanicLevel
	LOG_LEVEL_FATAL LogLevel = logrus.FatalLevel
	LOG_LEVEL_ERROR LogLevel = logrus.ErrorLevel
	LOG_LEVEL_WARN  LogLevel = logrus.WarnLevel
	LOG_LEVEL_INFO  LogLevel = logrus.InfoLevel
	LOG_LEVEL_DEBUG LogLevel = logrus.DebugLevel
	LOG_LEVEL_TRACE LogLevel = logrus.TraceLevel
)
