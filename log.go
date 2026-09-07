package plumber

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"time"
)

/*
Logger is the logger of the application and of every component of it.

It keeps the printf ergonomics of the components on top of the handler of the standard library,
where the level of a message is the level of the application and never the level of the handler,
and it captures the caller of a message itself so a line is never reported as coming from the
logger.
*/
type Logger struct {
	log      *slog.Logger
	controls loggerControls
}

// The parts of a handler that stay settable while the loggers of the components are already handed
// out, which is what the root of the application is controlled through.
type loggerControls interface {
	SetOutput(out io.Writer)
	Output() io.Writer
	SetLevel(level slog.Level)
	Level() slog.Level
	SetReportCaller(report bool)
	ReportCaller() bool
}

// The amount of the frames between the caller of a message and the frame that captures it, which
// are the method that is called and the writer of the record itself.
const logger_caller_skip = 3

// Creates a new logger that writes through the given handler.
func NewLogger(handler slog.Handler) *Logger {
	l := &Logger{
		log: slog.New(handler),
	}

	if controls, ok := handler.(loggerControls); ok {
		l.controls = controls
	}

	return l
}

// Returns a logger that carries the given field on every message that is logged through it.
func (l *Logger) With(key string, value any) *Logger {
	return &Logger{
		log:      l.log.With(key, value),
		controls: l.controls,
	}
}

func (l *Logger) Tracef(format string, args ...any) {
	if !l.enabled(LOG_LEVEL_TRACE) {
		return
	}

	l.write(LOG_LEVEL_TRACE, fmt.Sprintf(format, args...))
}

func (l *Logger) Traceln(args ...any) {
	if !l.enabled(LOG_LEVEL_TRACE) {
		return
	}

	l.write(LOG_LEVEL_TRACE, sprintln(args...))
}

func (l *Logger) Debugf(format string, args ...any) {
	if !l.enabled(LOG_LEVEL_DEBUG) {
		return
	}

	l.write(LOG_LEVEL_DEBUG, fmt.Sprintf(format, args...))
}

func (l *Logger) Debugln(args ...any) {
	if !l.enabled(LOG_LEVEL_DEBUG) {
		return
	}

	l.write(LOG_LEVEL_DEBUG, sprintln(args...))
}

func (l *Logger) Infof(format string, args ...any) {
	if !l.enabled(LOG_LEVEL_INFO) {
		return
	}

	l.write(LOG_LEVEL_INFO, fmt.Sprintf(format, args...))
}

func (l *Logger) Infoln(args ...any) {
	if !l.enabled(LOG_LEVEL_INFO) {
		return
	}

	l.write(LOG_LEVEL_INFO, sprintln(args...))
}

func (l *Logger) Info(args ...any) {
	if !l.enabled(LOG_LEVEL_INFO) {
		return
	}

	l.write(LOG_LEVEL_INFO, fmt.Sprint(args...))
}

func (l *Logger) Warnf(format string, args ...any) {
	if !l.enabled(LOG_LEVEL_WARN) {
		return
	}

	l.write(LOG_LEVEL_WARN, fmt.Sprintf(format, args...))
}

func (l *Logger) Warnln(args ...any) {
	if !l.enabled(LOG_LEVEL_WARN) {
		return
	}

	l.write(LOG_LEVEL_WARN, sprintln(args...))
}

func (l *Logger) Errorf(format string, args ...any) {
	if !l.enabled(LOG_LEVEL_ERROR) {
		return
	}

	l.write(LOG_LEVEL_ERROR, fmt.Sprintf(format, args...))
}

func (l *Logger) Errorln(args ...any) {
	if !l.enabled(LOG_LEVEL_ERROR) {
		return
	}

	l.write(LOG_LEVEL_ERROR, sprintln(args...))
}

func (l *Logger) Error(args ...any) {
	if !l.enabled(LOG_LEVEL_ERROR) {
		return
	}

	l.write(LOG_LEVEL_ERROR, fmt.Sprint(args...))
}

func (l *Logger) Logf(level LogLevel, format string, args ...any) {
	if !l.enabled(level) {
		return
	}

	l.write(level, fmt.Sprintf(format, args...))
}

func (l *Logger) Log(level LogLevel, args ...any) {
	if !l.enabled(level) {
		return
	}

	l.write(level, fmt.Sprint(args...))
}

func (l *Logger) Logln(level LogLevel, args ...any) {
	if !l.enabled(level) {
		return
	}

	l.write(level, sprintln(args...))
}

// Sets the level of the application, which every logger that is derived from the root of it is
// gated with.
func (l *Logger) SetLevel(level LogLevel) {
	if l == nil || l.controls == nil {
		return
	}

	l.controls.SetLevel(level.slog())
}

// Returns the level of the application.
func (l *Logger) GetLevel() LogLevel {
	if l == nil || l.controls == nil {
		return LOG_LEVEL_INFO
	}

	return logLevelFromSlog(l.controls.Level())
}

// Sets the writer that the application logs to.
func (l *Logger) SetOutput(out io.Writer) {
	if l == nil || l.controls == nil {
		return
	}

	l.controls.SetOutput(out)
}

// Returns the writer that the application logs to.
func (l *Logger) GetOutput() io.Writer {
	if l == nil || l.controls == nil {
		return nil
	}

	return l.controls.Output()
}

// Sets whether the caller of a message should be reported with it.
func (l *Logger) SetReportCaller(report bool) {
	if l == nil || l.controls == nil {
		return
	}

	l.controls.SetReportCaller(report)
}

// Returns whether the caller of a message is reported with it.
func (l *Logger) GetReportCaller() bool {
	if l == nil || l.controls == nil {
		return false
	}

	return l.controls.ReportCaller()
}

func (l *Logger) enabled(level LogLevel) bool {
	if l == nil || l.log == nil {
		return false
	}

	return l.log.Enabled(context.Background(), level.slog())
}

/*
Writes the message out through the handler of the logger.

The caller is captured here instead of inside the handler, since the frame that logs the message is
the frame that is interesting and not the frame of the logger that hands it over.
*/
func (l *Logger) write(level LogLevel, message string) {
	var pc uintptr

	if l.controls != nil && l.controls.ReportCaller() {
		var pcs [1]uintptr

		runtime.Callers(logger_caller_skip, pcs[:])

		pc = pcs[0]
	}

	_ = l.log.Handler().Handle(
		context.Background(),
		slog.NewRecord(time.Now(), level.slog(), message, pc),
	)
}

// Joins the arguments the way the print family of the standard library does, without the line break
// it appends to them.
func sprintln(args ...any) string {
	return strings.TrimSuffix(fmt.Sprintln(args...), "\n")
}
