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

// Options are the formatting options of the handler.
type Options struct {
	// FieldsOrder - default: fields sorted alphabetically
	FieldsOrder []string

	// TimestampFormat - default: no timestamp
	TimestampFormat string

	// HideKeys - show [fieldValue] instead of [fieldKey:fieldValue]
	HideKeys bool

	// NoEmptyFields - disable logging empty fields
	NoEmptyFields bool

	// NoColors - disable colors
	NoColors bool

	// NoFieldsColors - apply colors only to the level, default is level + fields
	NoFieldsColors bool

	// NoFieldsSpace - no space between fields
	NoFieldsSpace bool

	// ShowFullLevel - show a full level [WARNING] instead of [W].
	ShowFullLevel bool

	// NoUppercaseLevel - no upper case for level value
	NoUppercaseLevel bool

	// TrimMessages - trim whitespaces on messages
	TrimMessages bool

	// CallerFirst - print caller info first
	CallerFirst bool

	// LevelChars - amount of the characters of the level that are printed, default: 1
	LevelChars int

	// Redact some special keywords in strings
	Secrets *[]string
}

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
	// guards the options and the output, which is also where the records are serialized against
	lock         sync.Mutex
	options      Options
	out          io.Writer
	level        slog.LevelVar
	reportCaller atomic.Bool
}

// NewHandler creates a new handler that writes to the standard output with the info level.
func NewHandler(options Options) *Handler {
	h := &Handler{
		state: &handlerState{
			options: normalize(options),
			out:     os.Stdout,
		},
	}

	h.state.level.Set(slog.LevelInfo)

	return h
}

// Sets the formatting options of every logger that shares the state of this handler.
func (h *Handler) SetOptions(options Options) {
	h.state.lock.Lock()
	defer h.state.lock.Unlock()

	h.state.options = normalize(options)
}

// Returns the formatting options of the handler.
func (h *Handler) Options() Options {
	h.state.lock.Lock()
	defer h.state.lock.Unlock()

	return h.state.options
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

// Returns the writer that the records are written to.
func (h *Handler) Output() io.Writer {
	h.state.lock.Lock()
	defer h.state.lock.Unlock()

	return h.state.out
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

// Returns whether the caller of a record should be reported.
func (h *Handler) ReportCaller() bool {
	return h.state.reportCaller.Load()
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

	options := h.state.options

	b := &bytes.Buffer{}

	if options.TimestampFormat != "" {
		b.WriteString(record.Time.Format(options.TimestampFormat))
	}

	level := levelName(record.Level)
	if !options.NoUppercaseLevel {
		level = strings.ToUpper(level)
	}

	if options.CallerFirst {
		h.writeCaller(b, record)
	}

	if !options.NoColors {
		fmt.Fprintf(b, "\x1b[%dm", levelColor(record.Level))
	}

	if !options.NoFieldsSpace && options.TimestampFormat != "" {
		b.WriteString(" ")
	}

	b.WriteString("[")

	if options.ShowFullLevel || options.LevelChars >= len(level) {
		b.WriteString(level)
	} else {
		b.WriteString(level[:options.LevelChars])
	}

	b.WriteString("]")

	if !options.NoFieldsSpace {
		b.WriteString(" ")
	}

	if !options.NoColors && options.NoFieldsColors {
		b.WriteString("\x1b[0m")
	}

	writeFields(b, options, attrs)

	if options.NoFieldsSpace {
		b.WriteString(" ")
	}

	if !options.NoColors && !options.NoFieldsColors {
		b.WriteString("\x1b[0m")
	}

	message := record.Message

	if options.Secrets != nil && len(*options.Secrets) > 0 {
		for _, secret := range *options.Secrets {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}

	if options.TrimMessages {
		message = strings.TrimRightFunc(message, unicode.IsSpace)
	}

	b.WriteString(message)

	if !options.CallerFirst {
		h.writeCaller(b, record)
	}

	b.WriteByte('\n')

	_, err := h.state.out.Write(b.Bytes())

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
	if !h.ReportCaller() || record.PC == 0 {
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

func writeFields(b *bytes.Buffer, options Options, attrs []slog.Attr) {
	if len(attrs) == 0 {
		return
	}

	if options.FieldsOrder == nil {
		sorted := slices.Clone(attrs)

		slices.SortFunc(sorted, func(a, b slog.Attr) int {
			return cmp.Compare(a.Key, b.Key)
		})

		for _, attr := range sorted {
			writeField(b, options, attr)
		}

		return
	}

	rest := slices.Clone(attrs)

	for _, field := range options.FieldsOrder {
		index := slices.IndexFunc(rest, func(attr slog.Attr) bool {
			return attr.Key == field
		})

		if index < 0 {
			continue
		}

		attr := rest[index]
		rest = slices.Delete(rest, index, index+1)

		writeField(b, options, attr)
	}

	slices.SortFunc(rest, func(a, b slog.Attr) int {
		return cmp.Compare(a.Key, b.Key)
	})

	for _, attr := range rest {
		writeField(b, options, attr)
	}
}

func writeField(b *bytes.Buffer, options Options, attr slog.Attr) {
	value := fmt.Sprintf("%v", attr.Value.Resolve().Any())

	if options.NoEmptyFields && value == "" {
		return
	} else if options.HideKeys {
		fmt.Fprintf(b, "[%s]", value)
	} else {
		fmt.Fprintf(b, "[%s:%s]", attr.Key, value)
	}

	if !options.NoFieldsSpace {
		b.WriteString(" ")
	}
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

func normalize(options Options) Options {
	if options.LevelChars <= 0 {
		options.LevelChars = 1
	}

	return options
}

const (
	colorRed     = 31
	colorYellow  = 33
	colorGray    = 37
	colorCyan    = 36
	colorMagenta = 35
)

// Returns the name of the given level, which is the name of the closest level that is not more
// severe than it.
func levelName(level slog.Level) string {
	switch {
	case level <= LevelTrace:
		return "trace"
	case level <= slog.LevelDebug:
		return "debug"
	case level <= slog.LevelInfo:
		return "info"
	case level <= slog.LevelWarn:
		return "warning"
	default:
		return "error"
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
