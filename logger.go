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
	LogLevelDefault LogLevel = 0
	LogLevelPanic   LogLevel = 0
	LogLevelFatal   LogLevel = 1
	LogLevelError   LogLevel = 2
	LogLevelWarn    LogLevel = 3
	LogLevelInfo    LogLevel = 4
	LogLevelDebug   LogLevel = 5
	LogLevelTrace   LogLevel = 6
	LogFieldContext string   = "context"
	LogFieldStatus  string   = "status"
)

const (
	logContextDisable string = "DISABLE"
	logContextSkipped string = "SKIPPED"

	logStatusFail   string = "FAIL"
	logStatusExit   string = "EXIT"
	logStatusRun    string = "RUN"
	logStatusEnd    string = "END"
	logStatusScript string = "SCRIPT"
	logStatusRetry  string = "RETRY"
)

// Returns the name of the level.
func (l LogLevel) String() string {
	switch l {
	case LogLevelPanic:
		return "panic"
	case LogLevelFatal:
		return "fatal"
	case LogLevelError:
		return "error"
	case LogLevelWarn:
		return "warning"
	case LogLevelInfo:
		return "info"
	case LogLevelDebug:
		return "debug"
	case LogLevelTrace:
		return "trace"
	}

	return "unknown"
}

// Parses the given name of a level, where the level of a warning is accepted under both of its
// names.
func ParseLogLevel(level string) (LogLevel, error) {
	switch strings.ToLower(level) {
	case "panic":
		return LogLevelPanic, nil
	case "fatal":
		return LogLevelFatal, nil
	case "error":
		return LogLevelError, nil
	case "warn", "warning":
		return LogLevelWarn, nil
	case "info":
		return LogLevelInfo, nil
	case "debug":
		return LogLevelDebug, nil
	case "trace":
		return LogLevelTrace, nil
	}

	return LogLevelDefault, fmt.Errorf("Not a valid log level: %s", level)
}

// Maps the level to the level of the handler, where the levels that end the application share the
// level of an error since they are never used as an output level.
func (l LogLevel) slog() slog.Level {
	switch l {
	case LogLevelTrace:
		return logger.LevelTrace
	case LogLevelDebug:
		return slog.LevelDebug
	case LogLevelInfo:
		return slog.LevelInfo
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelError, LogLevelFatal, LogLevelPanic:
		return slog.LevelError
	}

	return slog.LevelInfo
}

// Maps the level of the handler back to the level of the application.
func logLevelFromSlog(level slog.Level) LogLevel {
	switch {
	case level <= logger.LevelTrace:
		return LogLevelTrace
	case level <= slog.LevelDebug:
		return LogLevelDebug
	case level <= slog.LevelInfo:
		return LogLevelInfo
	case level <= slog.LevelWarn:
		return LogLevelWarn
	}

	return LogLevelError
}
