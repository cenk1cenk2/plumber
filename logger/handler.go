package logger

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
)

// LevelTrace is the most verbose level of the handler, which slog itself does not know about.
const LevelTrace = slog.Level(-8)

// The fields that are written out before every other one, which are sorted alphabetically after
// them.
var fieldsOrder = []string{"context", "status"}

/*
Handler is the slog.Handler that writes out the log records of the application.

The state of the handler is shared between every handler that is derived from it through the
attributes of a logger, therefore the output, the level and the caller reporting of the application
can still be swapped on the fly while the loggers of the components are already handed out.
*/
type Handler struct {
	state  *handlerState
	attrs  []slog.Attr
	groups []string
}

type handlerState struct {
	// guards the output, which is also where the records are serialized against
	lock         sync.Mutex
	out          io.Writer
	redactor     redactor
	level        slog.LevelVar
	reportCaller atomic.Bool
}

// NewHandler creates a new handler that writes to the standard output with the info level.
func NewHandler() *Handler {
	h := &Handler{
		state: &handlerState{
			out: os.Stdout,
		},
	}

	h.state.level.Set(slog.LevelInfo)

	return h
}

// Sets the writer that the records are written to.
func (h *Handler) SetOutput(out io.Writer) {
	if out == nil {
		out = os.Stdout
	}

	h.state.lock.Lock()
	defer h.state.lock.Unlock()

	h.state.out = out
}

// Sets the level that the records are gated with.
func (h *Handler) SetLevel(level slog.Level) {
	h.state.level.Set(level)
}

// Returns the level that the records are gated with.
func (h *Handler) Level() slog.Level {
	return h.state.level.Level()
}

// Sets whether the caller of a record should be reported.
func (h *Handler) SetReportCaller(report bool) {
	h.state.reportCaller.Store(report)
}

/*
Registers sensitive values that are masked out of every record that is written out afterwards.

Next to the value itself the common encodings of it are masked as well, so that a value that leaks
through an url or a base64 payload is still caught.
*/
func (h *Handler) AddSecrets(values ...string) {
	h.state.redactor.add(values...)
}

func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.state.level.Level()
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	next := h.clone()

	for _, attr := range attrs {
		next.attrs = upsert(next.attrs, h.qualify(attr))
	}

	return next
}

func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	next := h.clone()
	next.groups = append(next.groups, name)

	return next
}

func (h *Handler) Handle(_ context.Context, record slog.Record) error {
	attrs := slices.Clone(h.attrs)

	record.Attrs(func(attr slog.Attr) bool {
		attrs = upsert(attrs, h.qualify(attr))

		return true
	})

	h.state.lock.Lock()
	defer h.state.lock.Unlock()

	b := &bytes.Buffer{}

	h.writeCaller(b, record)

	fmt.Fprintf(b, "\x1b[%dm[%s] ", levelColor(record.Level), levelInitial(record.Level))

	writeFields(b, attrs)

	b.WriteString("\x1b[0m")

	b.WriteString(strings.TrimRightFunc(record.Message, unicode.IsSpace))
	b.WriteByte('\n')

	// the whole record is masked in a single pass instead of only the message, so that a secret
	// that shows up in a field or in the caller is caught as well
	_, err := io.WriteString(h.state.out, h.state.redactor.redact(b.String()))

	return err
}

func (h *Handler) clone() *Handler {
	return &Handler{
		state:  h.state,
		attrs:  slices.Clone(h.attrs),
		groups: slices.Clone(h.groups),
	}
}

// Qualifies the key of an attribute with the groups that the handler is nested in.
func (h *Handler) qualify(attr slog.Attr) slog.Attr {
	if len(h.groups) == 0 {
		return attr
	}

	attr.Key = strings.Join(append(slices.Clone(h.groups), attr.Key), ".")

	return attr
}

func (h *Handler) writeCaller(b *bytes.Buffer, record slog.Record) {
	if !h.state.reportCaller.Load() || record.PC == 0 {
		return
	}

	frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()

	if frame.File == "" {
		return
	}

	fmt.Fprintf(
		b,
		"(%s:%d %s)",
		frame.File,
		frame.Line,
		frame.Function,
	)
}

func writeFields(b *bytes.Buffer, attrs []slog.Attr) {
	if len(attrs) == 0 {
		return
	}

	rest := slices.Clone(attrs)

	for _, field := range fieldsOrder {
		index := slices.IndexFunc(rest, func(attr slog.Attr) bool {
			return attr.Key == field
		})

		if index < 0 {
			continue
		}

		attr := rest[index]
		rest = slices.Delete(rest, index, index+1)

		writeField(b, attr)
	}

	slices.SortFunc(rest, func(a, b slog.Attr) int {
		return cmp.Compare(a.Key, b.Key)
	})

	for _, attr := range rest {
		writeField(b, attr)
	}
}

func writeField(b *bytes.Buffer, attr slog.Attr) {
	value := fmt.Sprintf("%v", attr.Value.Resolve().Any())

	if value == "" {
		return
	}

	fmt.Fprintf(b, "[%s] ", value)
}

// Adds the attribute to the given attributes, where an attribute that is already there is
// overwritten instead of written out twice.
func upsert(attrs []slog.Attr, attr slog.Attr) []slog.Attr {
	if index := slices.IndexFunc(attrs, func(current slog.Attr) bool {
		return current.Key == attr.Key
	}); index >= 0 {
		attrs[index] = attr

		return attrs
	}

	return append(attrs, attr)
}

const (
	colorRed     = 31
	colorYellow  = 33
	colorGray    = 37
	colorCyan    = 36
	colorMagenta = 35
)

// Returns the initial of the name of the closest level that is not more severe than the given one.
func levelInitial(level slog.Level) string {
	switch {
	case level <= LevelTrace:
		return "T"
	case level <= slog.LevelDebug:
		return "D"
	case level <= slog.LevelInfo:
		return "I"
	case level <= slog.LevelWarn:
		return "W"
	default:
		return "E"
	}
}

func levelColor(level slog.Level) int {
	switch {
	case level <= LevelTrace:
		return colorMagenta
	case level <= slog.LevelDebug:
		return colorGray
	case level <= slog.LevelInfo:
		return colorCyan
	case level <= slog.LevelWarn:
		return colorYellow
	default:
		return colorRed
	}
}
