package tests

import (
	"context"
	"log/slog"
	"sync"

	"github.com/cenk1cenk2/plumber/v7"
)

// LogRecord is a single message that a capture logger has recorded.
type LogRecord struct {
	Level   slog.Level
	Message string
}

// LogCapture records everything that is logged through the logger it belongs to, instead of writing
// it out anywhere.
type LogCapture struct {
	lock    sync.Mutex
	records []LogRecord
}

/*
Creates a logger that records the messages that are logged through it.

The logger is detached from the application on purpose, since it is handed over to the primitives
that take a logger of their own to report what they swallow.
*/
func NewCaptureLogger() (*plumber.Logger, *LogCapture) {
	capture := &LogCapture{}

	return plumber.NewLogger(&captureHandler{capture: capture}), capture
}

// Returns the records that have been logged so far.
func (c *LogCapture) Records() []LogRecord {
	c.lock.Lock()
	defer c.lock.Unlock()

	return append([]LogRecord{}, c.records...)
}

// Returns the messages of the records that have been logged so far.
func (c *LogCapture) Messages() []string {
	messages := []string{}

	for _, record := range c.Records() {
		messages = append(messages, record.Message)
	}

	return messages
}

func (c *LogCapture) append(record LogRecord) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.records = append(c.records, record)
}

type captureHandler struct {
	capture *LogCapture
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	h.capture.append(LogRecord{
		Level:   record.Level,
		Message: record.Message,
	})

	return nil
}

func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *captureHandler) WithGroup(_ string) slog.Handler {
	return h
}
