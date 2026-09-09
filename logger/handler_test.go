package logger_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"runtime"
	"sync"
	"time"

	"github.com/cenk1cenk2/plumber/v7/logger"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

/*
The renderer that the styles of this file are composed with, forced onto the same profile the
handler forces its own renderer to, so that the rendered bytes are deterministic regardless of the
terminal the suite runs in.

The styles below express the theme the handler is expected to render, declared independently of the
handler's own style table, so that a regression in the source styling fails a spec here instead of
both sides drifting together.
*/
var stylesRenderer = func() *lipgloss.Renderer {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.ANSI)

	return renderer
}()

// The style that every element of a record of the given level is expected to inherit from, dimmed
// as a whole for the levels that are only noise.
func styleBase(level slog.Level) lipgloss.Style {
	return stylesRenderer.NewStyle().TabWidth(lipgloss.NoTabConversion).Faint(level <= slog.LevelDebug)
}

// The style of the badge that is expected to name the level of a record.
func styleBadge(level slog.Level) lipgloss.Style {
	return styleBase(level).Bold(true).Foreground(styleLevelColor(level))
}

// The color a badge is expected to carry for the given level.
func styleLevelColor(level slog.Level) lipgloss.ANSIColor {
	switch {
	case level <= logger.LevelTrace:
		return lipgloss.ANSIColor(5) // magenta
	case level <= slog.LevelDebug:
		return lipgloss.ANSIColor(7) // gray
	case level <= slog.LevelInfo:
		return lipgloss.ANSIColor(6) // cyan
	case level <= slog.LevelWarn:
		return lipgloss.ANSIColor(3) // yellow
	default:
		return lipgloss.ANSIColor(1) // red
	}
}

// The style a field with the given key is expected to carry at the given level.
func styleField(level slog.Level, key string) lipgloss.Style {
	switch key {
	case "context":
		return styleBase(level).Foreground(lipgloss.ANSIColor(4)) // blue
	case "status":
		return styleBase(level).Foreground(lipgloss.ANSIColor(2)) // green
	default:
		return styleBase(level).Faint(true)
	}
}

func handle(handler *logger.Handler, record slog.Record) {
	GinkgoHelper()

	Expect(handler.Handle(context.Background(), record)).To(Succeed())
}

func record(level slog.Level, message string) slog.Record {
	return slog.NewRecord(time.Unix(0, 0), level, message, 0)
}

// The badge of a level as it is expected to be rendered, composed from the style declared above
// instead of a hand-written escape sequence.
func badge(level slog.Level, initial string) string {
	return styleBadge(level).Render("["+initial+"]") + " "
}

var (
	badgeTrace = badge(logger.LevelTrace, "T")
	badgeDebug = badge(slog.LevelDebug, "D")
	badgeInfo  = badge(slog.LevelInfo, "I")
	badgeWarn  = badge(slog.LevelWarn, "W")
	badgeError = badge(slog.LevelError, "E")
)

// The fields as they are expected to be rendered for a record at the info level, which is not
// dimmed as a whole.
func contextField(value string) string {
	return styleField(slog.LevelInfo, "context").Render("["+value+"]") + " "
}

func statusField(value string) string {
	return styleField(slog.LevelInfo, "status").Render("["+value+"]") + " "
}

func field(value string) string {
	return styleField(slog.LevelInfo, "other").Render("["+value+"]") + " "
}

// The message as it is expected to be rendered for a record at the given level.
func message(level slog.Level, value string) string {
	return styleBase(level).Render(value)
}

var _ = Describe("Handler", func() {
	var output *bytes.Buffer

	BeforeEach(func() {
		output = &bytes.Buffer{}
	})

	It("should format ordered fields and trim messages", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		with := handler.WithAttrs([]slog.Attr{
			slog.String("status", "RUN"),
			slog.String("empty", ""),
			slog.String("context", "task"),
		})

		Expect(with.Handle(context.Background(), record(slog.LevelInfo, "done \n"))).To(Succeed())

		Expect(output.String()).To(Equal(badgeInfo + contextField("task") + statusField("RUN") + "done\n"))
	})

	It("should redact configured secrets from messages", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.AddSecrets("secret-token")

		handle(handler, record(slog.LevelInfo, "using secret-token"))

		Expect(output.String()).To(Equal(badgeInfo + "using [REDACTED]\n"))
	})

	It("should redact the secrets from the fields", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.AddSecrets("secret-token")

		with := handler.WithAttrs([]slog.Attr{slog.String("context", "secret-token")})

		Expect(with.Handle(context.Background(), record(slog.LevelInfo, "done"))).To(Succeed())

		Expect(output.String()).To(Equal(badgeInfo + contextField("[REDACTED]") + "done\n"))
	})

	It("should redact more than one secret in a single record", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.AddSecrets("first-secret", "second-secret")

		handle(handler, record(slog.LevelInfo, "using first-secret and second-secret"))

		Expect(output.String()).To(Equal(badgeInfo + "using [REDACTED] and [REDACTED]\n"))
	})

	It("should redact a secret that overlaps a longer one as a whole", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.AddSecrets("token", "token-of-the-application")

		handle(handler, record(slog.LevelInfo, "using token-of-the-application"))

		Expect(output.String()).To(Equal(badgeInfo + "using [REDACTED]\n"))
	})

	DescribeTable(
		"should redact the encoded variants of a secret",
		func(_ SpecContext, encode func(string) string) {
			handler := logger.NewHandler()
			handler.SetOutput(output)
			handler.AddSecrets("secret token/value?")

			handle(handler, record(slog.LevelInfo, "using "+encode("secret token/value?")))

			Expect(output.String()).To(Equal(badgeInfo + "using [REDACTED]\n"))
		},
		Entry("url", url.QueryEscape),
		Entry("base64", func(value string) string {
			return base64.StdEncoding.EncodeToString([]byte(value))
		}),
		Entry("base64 without padding", func(value string) string {
			return base64.RawStdEncoding.EncodeToString([]byte(value))
		}),
		Entry("base64 for urls", func(value string) string {
			return base64.URLEncoding.EncodeToString([]byte(value))
		}),
		Entry("base64 for urls without padding", func(value string) string {
			return base64.RawURLEncoding.EncodeToString([]byte(value))
		}),
	)

	It("should redact a secret however short it is", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.AddSecrets("an")

		handle(handler, record(slog.LevelInfo, "an apple"))

		Expect(output.String()).To(Equal(badgeInfo + "[REDACTED] apple\n"))
	})

	It("should ignore the values that are empty", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.AddSecrets("")

		handle(handler, record(slog.LevelInfo, "an apple a day"))

		Expect(output.String()).To(Equal(badgeInfo + "an apple a day\n"))
	})

	It("should redact the secrets that are added after the loggers are derived", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		derived := slog.New(handler).With(slog.String("context", "task"))

		handler.AddSecrets("secret-token")

		derived.Info("using secret-token")

		Expect(output.String()).To(Equal(badgeInfo + contextField("task") + "using [REDACTED]\n"))
	})

	It("should redact the secrets that are reported with the caller", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.SetReportCaller(true)
		handler.AddSecrets("secret-token")

		slog.New(handler).Info("using secret-token")

		Expect(output.String()).To(ContainSubstring("handler_test.go:"))
		Expect(output.String()).To(ContainSubstring("[REDACTED]"))
		Expect(output.String()).ToNot(ContainSubstring("secret-token"))
	})

	It("should register the secrets while the records are written out", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		wg := sync.WaitGroup{}
		wg.Add(2)

		go func() {
			defer GinkgoRecover()
			defer wg.Done()

			for i := range 100 {
				handler.AddSecrets(fmt.Sprintf("secret-token-%d", i))
			}
		}()

		go func() {
			defer GinkgoRecover()
			defer wg.Done()

			for range 100 {
				handle(handler, record(slog.LevelInfo, "done"))
			}
		}()

		wg.Wait()

		handle(handler, record(slog.LevelInfo, "using secret-token-99"))

		Expect(output.String()).To(HaveSuffix(badgeInfo + "using [REDACTED]\n"))
	})

	It("should overwrite a field that is set more than once", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		with := handler.
			WithAttrs([]slog.Attr{slog.String("context", "task")}).
			WithAttrs([]slog.Attr{slog.String("context", "DISABLE")})

		Expect(with.Handle(context.Background(), record(slog.LevelInfo, "done"))).To(Succeed())

		Expect(output.String()).To(Equal(badgeInfo + contextField("DISABLE") + "done\n"))
	})

	It("should carry the attributes of the loggers that are derived from it", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		log := slog.New(handler)
		derived := log.With(slog.String("context", "task")).With(slog.String("status", "RUN"))

		derived.Info("done")
		log.Info("root")

		Expect(output.String()).To(Equal(badgeInfo + contextField("task") + statusField("RUN") + "done\n" + badgeInfo + "root\n"))
	})

	It("should report the caller of the message and never the logger itself", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.SetReportCaller(true)

		slog.New(handler).Info("done")

		Expect(output.String()).To(ContainSubstring("handler_test.go:"))
		Expect(output.String()).ToNot(ContainSubstring("log/slog"))
	})

	It("should keep the order of the fields that are added while the loggers are derived", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		slog.New(handler).
			With(slog.String("zulu", "z")).
			With(slog.String("context", "task")).
			Info("done", slog.String("alpha", "a"))

		Expect(output.String()).To(Equal(badgeInfo + contextField("task") + field("z") + field("a") + "done\n"))
	})

	It("should render the house fields before every other one however they sort", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		with := handler.WithAttrs([]slog.Attr{
			slog.String("beta", "b"),
			slog.String("status", "RUN"),
			slog.String("alpha", "a"),
			slog.String("context", "task"),
		})

		Expect(with.Handle(context.Background(), record(slog.LevelInfo, "done"))).To(Succeed())

		Expect(output.String()).To(Equal(
			badgeInfo + contextField("task") + statusField("RUN") + field("b") + field("a") + "done\n",
		))
	})

	It("should style the badge, the context and the status of a record", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		with := handler.WithAttrs([]slog.Attr{
			slog.String("context", "task"),
			slog.String("status", "RUN"),
			slog.String("other", "value"),
		})

		Expect(with.Handle(context.Background(), record(slog.LevelWarn, "done"))).To(Succeed())

		Expect(output.String()).To(Equal(
			badge(slog.LevelWarn, "W") +
				styleField(slog.LevelWarn, "context").Render("[task]") + " " +
				styleField(slog.LevelWarn, "status").Render("[RUN]") + " " +
				styleField(slog.LevelWarn, "other").Render("[value]") + " " +
				"done\n",
		))
	})

	DescribeTable(
		"should dim the levels that are only noise as a whole",
		func(_ SpecContext, level slog.Level, expectedBadge string) {
			handler := logger.NewHandler()
			handler.SetOutput(output)
			handler.SetLevel(logger.LevelTrace)

			with := handler.WithAttrs([]slog.Attr{
				slog.String("context", "task"),
				slog.String("status", "RUN"),
				slog.String("other", "value"),
			})

			Expect(with.Handle(context.Background(), record(level, "done"))).To(Succeed())

			Expect(output.String()).To(Equal(
				expectedBadge +
					styleField(level, "context").Render("[task]") + " " +
					styleField(level, "status").Render("[RUN]") + " " +
					styleField(level, "other").Render("[value]") + " " +
					message(level, "done") + "\n",
			))
		},
		Entry("trace", logger.LevelTrace, badgeTrace),
		Entry("debug", slog.LevelDebug, badgeDebug),
	)

	It("should redact a secret that is wrapped in the styling of a field", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.SetLevel(logger.LevelTrace)
		handler.AddSecrets("secret-token")

		with := handler.WithAttrs([]slog.Attr{slog.String("status", "secret-token")})

		Expect(with.Handle(context.Background(), record(slog.LevelDebug, "done"))).To(Succeed())

		Expect(output.String()).To(Equal(
			badgeDebug + styleField(slog.LevelDebug, "status").Render("[[REDACTED]]") + " " + message(slog.LevelDebug, "done") + "\n",
		))
	})

	It("should write out the records without any escape sequences when colors are turned off", func(_ SpecContext) {
		GinkgoT().Setenv("NO_COLOR", "1")

		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.SetLevel(logger.LevelTrace)

		with := handler.WithAttrs([]slog.Attr{
			slog.String("context", "task"),
			slog.String("status", "RUN"),
			slog.String("other", "value"),
		})

		Expect(with.Handle(context.Background(), record(slog.LevelDebug, "done"))).To(Succeed())
		Expect(with.Handle(context.Background(), record(slog.LevelError, "failed"))).To(Succeed())

		Expect(output.String()).To(Equal("[D] [task] [RUN] [value] done\n[E] [task] [RUN] [value] failed\n"))
		Expect(output.String()).ToNot(ContainSubstring("\x1b"))
	})

	It("should render the colors although the output is not a terminal", func(_ SpecContext) {
		GinkgoT().Setenv("NO_COLOR", "1")
		GinkgoT().Setenv("CLICOLOR_FORCE", "1")

		handler := logger.NewHandler()
		handler.SetOutput(output)

		handle(handler, record(slog.LevelInfo, "done"))

		Expect(output.String()).To(Equal(badgeInfo + "done\n"))
	})

	It("should keep the profile of the writer that takes over", func(_ SpecContext) {
		GinkgoT().Setenv("NO_COLOR", "1")

		handler := logger.NewHandler()
		handler.SetOutput(output)

		handle(handler, record(slog.LevelInfo, "done"))

		other := &bytes.Buffer{}
		handler.SetOutput(other)

		handle(handler, record(slog.LevelInfo, "done"))

		Expect(other.String()).To(Equal(output.String()))
		Expect(other.String()).To(Equal("[I] done\n"))
	})

	DescribeTable(
		"should color the badge depending on the level",
		func(_ SpecContext, level slog.Level, expectedBadge string) {
			handler := logger.NewHandler()
			handler.SetOutput(output)

			handle(handler, record(level, "done"))

			Expect(output.String()).To(Equal(expectedBadge + message(level, "done") + "\n"))
		},
		Entry("trace", logger.LevelTrace, badgeTrace),
		Entry("debug", slog.LevelDebug, badgeDebug),
		Entry("info", slog.LevelInfo, badgeInfo),
		Entry("warn", slog.LevelWarn, badgeWarn),
		Entry("error", slog.LevelError, badgeError),
	)

	It("should gate the records with the level that is set", func(_ SpecContext) {
		handler := logger.NewHandler()

		Expect(handler.Enabled(context.Background(), slog.LevelInfo)).To(BeTrue())
		Expect(handler.Enabled(context.Background(), slog.LevelDebug)).To(BeFalse())

		handler.SetLevel(logger.LevelTrace)

		Expect(handler.Enabled(context.Background(), logger.LevelTrace)).To(BeTrue())
		Expect(handler.Level()).To(Equal(logger.LevelTrace))
	})

	It("should share the state with the handlers that are derived from it", func(_ SpecContext) {
		handler := logger.NewHandler()
		with := handler.WithAttrs([]slog.Attr{slog.String("context", "task")})

		handler.SetOutput(output)
		handler.SetLevel(logger.LevelTrace)

		Expect(with.Enabled(context.Background(), logger.LevelTrace)).To(BeTrue())
		Expect(with.Handle(context.Background(), record(logger.LevelTrace, "done"))).To(Succeed())

		Expect(output.String()).To(Equal(
			badgeTrace + styleField(logger.LevelTrace, "context").Render("[task]") + " " + message(logger.LevelTrace, "done") + "\n",
		))
	})

	It("should not report the caller unless it is asked for", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		var pcs [1]uintptr
		runtime.Callers(1, pcs[:])

		handle(handler, slog.NewRecord(time.Unix(0, 0), slog.LevelInfo, "done", pcs[0]))

		Expect(output.String()).To(Equal(badgeInfo + "done\n"))

		output.Reset()
		handler.SetReportCaller(true)

		handle(handler, slog.NewRecord(time.Unix(0, 0), slog.LevelInfo, "done", pcs[0]))

		Expect(output.String()).To(ContainSubstring("handler_test.go:"))
	})

	// This spec pins the real rendered bytes independently of both the source and the test style
	// declarations above, so a genuine escape-sequence regression is still caught even if the two
	// were to drift together.
	DescribeTable(
		"should keep the exact bytes of the styled output",
		func(_ SpecContext, attrs []slog.Attr, rec slog.Record, expected string) {
			handler := logger.NewHandler()
			handler.SetOutput(output)

			var h slog.Handler = handler
			if len(attrs) > 0 {
				h = handler.WithAttrs(attrs)
			}

			Expect(h.Handle(context.Background(), rec)).To(Succeed())

			Expect(output.String()).To(Equal(expected))
		},
		Entry("the trace badge", []slog.Attr(nil), record(logger.LevelTrace, "done"), "\x1b[1;2;35m[T]\x1b[0m \x1b[2mdone\x1b[0m\n"),
		Entry("the debug badge", []slog.Attr(nil), record(slog.LevelDebug, "done"), "\x1b[1;2;37m[D]\x1b[0m \x1b[2mdone\x1b[0m\n"),
		Entry("the info badge", []slog.Attr(nil), record(slog.LevelInfo, "done"), "\x1b[1;36m[I]\x1b[0m done\n"),
		Entry("the warn badge", []slog.Attr(nil), record(slog.LevelWarn, "done"), "\x1b[1;33m[W]\x1b[0m done\n"),
		Entry("the error badge", []slog.Attr(nil), record(slog.LevelError, "done"), "\x1b[1;31m[E]\x1b[0m done\n"),
		Entry(
			"a context field",
			[]slog.Attr{slog.String("context", "task")},
			record(slog.LevelInfo, "done"),
			"\x1b[1;36m[I]\x1b[0m \x1b[34m[task]\x1b[0m done\n",
		),
		Entry(
			"a status field",
			[]slog.Attr{slog.String("status", "RUN")},
			record(slog.LevelInfo, "done"),
			"\x1b[1;36m[I]\x1b[0m \x1b[32m[RUN]\x1b[0m done\n",
		),
		Entry(
			"a consumer field",
			[]slog.Attr{slog.String("other", "value")},
			record(slog.LevelInfo, "done"),
			"\x1b[1;36m[I]\x1b[0m \x1b[2m[value]\x1b[0m done\n",
		),
		Entry(
			"a whole-line-faint trace record with a field",
			[]slog.Attr{slog.String("context", "task")},
			record(logger.LevelTrace, "done"),
			"\x1b[1;2;35m[T]\x1b[0m \x1b[2;34m[task]\x1b[0m \x1b[2mdone\x1b[0m\n",
		),
	)

	// This spec pins the zero-escape byte contract when colors are turned off, which is the other
	// end of the styled-output contract above.
	It("should keep the exact zero-escape bytes when colors are turned off", func(_ SpecContext) {
		GinkgoT().Setenv("NO_COLOR", "1")

		handler := logger.NewHandler()
		handler.SetOutput(output)

		handle(handler, record(slog.LevelInfo, "done"))

		Expect(output.String()).To(Equal("[I] done\n"))
	})
})
