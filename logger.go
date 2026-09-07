package plumber

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/cenk1cenk2/plumber/v7/logger"
)

/*
LogLevel is the level of verbosity a message is logged with.

The levels ascend with the verbosity, therefore a level that is more verbose than another one is
greater than it, and the default of the applications doubles as the sentinel that tells the
components of the application to fall back to the level they would use anyway, which is why it
shares its value with the level of a panic that is never used as an output level.
*/
type LogLevel int

const (
	LOG_LEVEL_DEFAULT LogLevel = 0
	LOG_LEVEL_PANIC   LogLevel = 0
	LOG_LEVEL_FATAL   LogLevel = 1
	LOG_LEVEL_ERROR   LogLevel = 2
	LOG_LEVEL_WARN    LogLevel = 3
	LOG_LEVEL_INFO    LogLevel = 4
	LOG_LEVEL_DEBUG   LogLevel = 5
	LOG_LEVEL_TRACE   LogLevel = 6
	LOG_FIELD_CONTEXT string   = "context"
	LOG_FIELD_STATUS  string   = "status"
)

const (
	log_context_disable string = "DISABLE"
	log_context_skipped string = "SKIPPED"

	log_status_fail   string = "FAIL"
	log_status_exit   string = "EXIT"
	log_status_run    string = "RUN"
	log_status_end    string = "END"
	log_status_script string = "SCRIPT"
	log_status_retry  string = "RETRY"
)

// Returns the name of the level.
func (l LogLevel) String() string {
	switch l {
	case LOG_LEVEL_PANIC:
		return "panic"
	case LOG_LEVEL_FATAL:
		return "fatal"
	case LOG_LEVEL_ERROR:
		return "error"
	case LOG_LEVEL_WARN:
		return "warning"
	case LOG_LEVEL_INFO:
		return "info"
	case LOG_LEVEL_DEBUG:
		return "debug"
	case LOG_LEVEL_TRACE:
		return "trace"
	}

	return "unknown"
}

// Parses the given name of a level, where the level of a warning is accepted under both of its
// names.
func ParseLogLevel(level string) (LogLevel, error) {
	switch strings.ToLower(level) {
	case "panic":
		return LOG_LEVEL_PANIC, nil
	case "fatal":
		return LOG_LEVEL_FATAL, nil
	case "error":
		return LOG_LEVEL_ERROR, nil
	case "warn", "warning":
		return LOG_LEVEL_WARN, nil
	case "info":
		return LOG_LEVEL_INFO, nil
	case "debug":
		return LOG_LEVEL_DEBUG, nil
	case "trace":
		return LOG_LEVEL_TRACE, nil
	}

	return LOG_LEVEL_DEFAULT, fmt.Errorf("Not a valid log level: %s", level)
}

// Maps the level to the level of the handler, where the levels that end the application share the
// level of an error since they are never used as an output level.
func (l LogLevel) slog() slog.Level {
	switch l {
	case LOG_LEVEL_TRACE:
		return logger.LevelTrace
	case LOG_LEVEL_DEBUG:
		return slog.LevelDebug
	case LOG_LEVEL_INFO:
		return slog.LevelInfo
	case LOG_LEVEL_WARN:
		return slog.LevelWarn
	case LOG_LEVEL_ERROR, LOG_LEVEL_FATAL, LOG_LEVEL_PANIC:
		return slog.LevelError
	}

	return slog.LevelInfo
}

// Maps the level of the handler back to the level of the application.
func logLevelFromSlog(level slog.Level) LogLevel {
	switch {
	case level <= logger.LevelTrace:
		return LOG_LEVEL_TRACE
	case level <= slog.LevelDebug:
		return LOG_LEVEL_DEBUG
	case level <= slog.LevelInfo:
		return LOG_LEVEL_INFO
	case level <= slog.LevelWarn:
		return LOG_LEVEL_WARN
	}

	return LOG_LEVEL_ERROR
}
