package logger

import (
	"bytes"
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

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// LevelTrace is the most verbose level of the handler, which slog itself does not know about.
const LevelTrace = slog.Level(-8)

// The fields that carry a color of their own, since they name the origin and the state of a record.
const (
	fieldContext = "context"
	fieldStatus  = "status"
)

// The fields that are written out before every other one, which keep the order they were added in
// after them.
var fieldsOrder = []string{fieldContext, fieldStatus}

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
	profile      termenv.Profile
	theme        theme
	redactor     redactor
	level        slog.LevelVar
	reportCaller atomic.Bool
}

// NewHandler creates a new handler that writes to the standard output with the info level.
func NewHandler() *Handler {
	h := &Handler{
		state: &handlerState{
			out:     os.Stdout,
			profile: colorProfile(),
		},
	}

	h.state.theme = newTheme(h.state.out, h.state.profile)
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
	// the renderer of the theme is bound to the writer it was created for, so it is rebuilt for
	// the writer that takes over while the profile that was resolved once stays the same
	h.state.theme = newTheme(out, h.state.profile)
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
	theme := h.state.theme

	h.writeCaller(b, theme, record)

	b.WriteString(theme.badge(record.Level).Render("[" + levelInitial(record.Level) + "]"))
	b.WriteByte(' ')

	writeFields(b, theme, record.Level, attrs)

	// a message that is empty is left unstyled, so that it does not end up as a pair of escape
	// sequences with nothing between them
	if message := strings.TrimRightFunc(record.Message, unicode.IsSpace); message != "" {
		b.WriteString(theme.base(record.Level).Render(message))
	}

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

func (h *Handler) writeCaller(b *bytes.Buffer, theme theme, record slog.Record) {
	if !h.state.reportCaller.Load() || record.PC == 0 {
		return
	}

	frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()

	if frame.File == "" {
		return
	}

	b.WriteString(theme.base(record.Level).Render(fmt.Sprintf(
		"(%s:%d %s)",
		frame.File,
		frame.Line,
		frame.Function,
	)))
}

func writeFields(b *bytes.Buffer, theme theme, level slog.Level, attrs []slog.Attr) {
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

		writeField(b, theme, level, attr)
	}

	// the fields that are left keep the order they were added in, which is the derivation order of
	// the loggers followed by the order of the attributes of the call itself
	for _, attr := range rest {
		writeField(b, theme, level, attr)
	}
}

func writeField(b *bytes.Buffer, theme theme, level slog.Level, attr slog.Attr) {
	value := fmt.Sprintf("%v", attr.Value.Resolve().Any())

	if value == "" {
		return
	}

	b.WriteString(theme.field(level, attr.Key).Render("[" + value + "]"))
	b.WriteByte(' ')
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

// The palette of the handler, which stays inside the sixteen colors that every log viewer agrees
// on, where the two that the levels leave over mark the context and the status fields.
const (
	colorRed     = lipgloss.ANSIColor(1)
	colorGreen   = lipgloss.ANSIColor(2)
	colorYellow  = lipgloss.ANSIColor(3)
	colorBlue    = lipgloss.ANSIColor(4)
	colorMagenta = lipgloss.ANSIColor(5)
	colorCyan    = lipgloss.ANSIColor(6)
	colorGray    = lipgloss.ANSIColor(7)
)

/*
The styles of the elements of a record.

The renderer is bound to the writer that the records are written to and its color profile is forced
instead of detected, because plumber mostly runs in a ci where the log viewer renders the escape
sequences although the output it is handed is never a terminal.
*/
type theme struct {
	renderer *lipgloss.Renderer
}

func newTheme(out io.Writer, profile termenv.Profile) theme {
	renderer := lipgloss.NewRenderer(out)
	renderer.SetColorProfile(profile)

	return theme{renderer: renderer}
}

/*
Returns the style that every element of a record of the given level inherits from.

The levels that are only there to be read while something is being chased down are dimmed as a
whole, so that they recede behind the records that carry the progress of a pipeline.

Tab conversion is turned off throughout, since the styling should never touch the content it wraps.
*/
func (t theme) base(level slog.Level) lipgloss.Style {
	return t.renderer.NewStyle().
		TabWidth(lipgloss.NoTabConversion).
		Faint(level <= slog.LevelDebug)
}

// Returns the style of the badge that names the level of a record.
func (t theme) badge(level slog.Level) lipgloss.Style {
	return t.base(level).Bold(true).Foreground(levelColor(level))
}

// Returns the style of the field with the given key.
func (t theme) field(level slog.Level, key string) lipgloss.Style {
	switch key {
	case fieldContext:
		return t.base(level).Foreground(colorBlue)
	case fieldStatus:
		return t.base(level).Foreground(colorGreen)
	default:
		return t.base(level).Faint(true)
	}
}

/*
Returns the color profile that the records are rendered with.

The profile is forced rather than detected from the writer, because the output of plumber mostly
ends up in a ci log viewer that renders the escape sequences while it is not a terminal, which is
what a detection would key off of. Only the environment gets a say over it, following the semantics
that no-color.org lays out.
*/
func colorProfile() termenv.Profile {
	if force := os.Getenv("CLICOLOR_FORCE"); force != "" && force != "0" {
		return termenv.ANSI
	}

	if os.Getenv("NO_COLOR") != "" {
		return termenv.Ascii
	}

	return termenv.ANSI
}

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

func levelColor(level slog.Level) lipgloss.ANSIColor {
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
