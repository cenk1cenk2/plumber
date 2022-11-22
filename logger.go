package plumber

import "github.com/sirupsen/logrus"

type (
	LogLevel = logrus.Level
)

const (
	LOG_LEVEL_DEFAULT LogLevel = 0
	LOG_LEVEL_PANIC   LogLevel = logrus.PanicLevel
	LOG_LEVEL_FATAL   LogLevel = logrus.FatalLevel
	LOG_LEVEL_ERROR   LogLevel = logrus.ErrorLevel
	LOG_LEVEL_WARN    LogLevel = logrus.WarnLevel
	LOG_LEVEL_INFO    LogLevel = logrus.InfoLevel
	LOG_LEVEL_DEBUG   LogLevel = logrus.DebugLevel
	LOG_LEVEL_TRACE   LogLevel = logrus.TraceLevel
	LOG_FIELD_CONTEXT string   = "context"
	LOG_FIELD_STATUS  string   = "status"
)

const (
	log_context_disable string = "DISABLE"
	log_context_skipped string = "SKIPPED"

	log_status_fail string = "FAIL"
	log_status_exit string = "EXIT"
	log_status_run  string = "RUN"
	log_status_end  string = "END"
)
