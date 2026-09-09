package logger_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"runtime"
	"sync"
	"time"

	"github.com/cenk1cenk2/plumber/v7/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func handle(handler *logger.Handler, record slog.Record) {
	GinkgoHelper()

	Expect(handler.Handle(context.Background(), record)).To(Succeed())
}

func record(level slog.Level, message string) slog.Record {
	return slog.NewRecord(time.Unix(0, 0), level, message, 0)
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

		Expect(output.String()).To(Equal("\x1b[36m[I] [task] [RUN] \x1b[0mdone\n"))
	})

	It("should redact configured secrets from messages", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.AddSecrets("secret-token")

		handle(handler, record(slog.LevelInfo, "using secret-token"))

		Expect(output.String()).To(Equal("\x1b[36m[I] \x1b[0musing [REDACTED]\n"))
	})

	It("should redact the secrets from the fields", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.AddSecrets("secret-token")

		with := handler.WithAttrs([]slog.Attr{slog.String("context", "secret-token")})

		Expect(with.Handle(context.Background(), record(slog.LevelInfo, "done"))).To(Succeed())

		Expect(output.String()).To(Equal("\x1b[36m[I] [[REDACTED]] \x1b[0mdone\n"))
	})

	It("should redact more than one secret in a single record", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.AddSecrets("first-secret", "second-secret")

		handle(handler, record(slog.LevelInfo, "using first-secret and second-secret"))

		Expect(output.String()).To(Equal("\x1b[36m[I] \x1b[0musing [REDACTED] and [REDACTED]\n"))
	})

	It("should redact a secret that overlaps a longer one as a whole", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.AddSecrets("token", "token-of-the-application")

		handle(handler, record(slog.LevelInfo, "using token-of-the-application"))

		Expect(output.String()).To(Equal("\x1b[36m[I] \x1b[0musing [REDACTED]\n"))
	})

	DescribeTable(
		"should redact the encoded variants of a secret",
		func(_ SpecContext, encode func(string) string) {
			handler := logger.NewHandler()
			handler.SetOutput(output)
			handler.AddSecrets("secret token/value?")

			handle(handler, record(slog.LevelInfo, "using "+encode("secret token/value?")))

			Expect(output.String()).To(Equal("\x1b[36m[I] \x1b[0musing [REDACTED]\n"))
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

		Expect(output.String()).To(Equal("\x1b[36m[I] \x1b[0m[REDACTED] apple\n"))
	})

	It("should ignore the values that are empty", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.AddSecrets("")

		handle(handler, record(slog.LevelInfo, "an apple a day"))

		Expect(output.String()).To(Equal("\x1b[36m[I] \x1b[0man apple a day\n"))
	})

	It("should redact the secrets that are added after the loggers are derived", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		derived := slog.New(handler).With(slog.String("context", "task"))

		handler.AddSecrets("secret-token")

		derived.Info("using secret-token")

		Expect(output.String()).To(Equal("\x1b[36m[I] [task] \x1b[0musing [REDACTED]\n"))
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

		Expect(output.String()).To(HaveSuffix("\x1b[36m[I] \x1b[0musing [REDACTED]\n"))
	})

	It("should overwrite a field that is set more than once", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		with := handler.
			WithAttrs([]slog.Attr{slog.String("context", "task")}).
			WithAttrs([]slog.Attr{slog.String("context", "DISABLE")})

		Expect(with.Handle(context.Background(), record(slog.LevelInfo, "done"))).To(Succeed())

		Expect(output.String()).To(Equal("\x1b[36m[I] [DISABLE] \x1b[0mdone\n"))
	})

	It("should carry the attributes of the loggers that are derived from it", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		log := slog.New(handler)
		derived := log.With(slog.String("context", "task")).With(slog.String("status", "RUN"))

		derived.Info("done")
		log.Info("root")

		Expect(output.String()).To(Equal("\x1b[36m[I] [task] [RUN] \x1b[0mdone\n\x1b[36m[I] \x1b[0mroot\n"))
	})

	It("should report the caller of the message and never the logger itself", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)
		handler.SetReportCaller(true)

		slog.New(handler).Info("done")

		Expect(output.String()).To(ContainSubstring("handler_test.go:"))
		Expect(output.String()).ToNot(ContainSubstring("log/slog"))
	})

	It("should sort the fields that are not ordered alphabetically", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		with := handler.WithAttrs([]slog.Attr{
			slog.String("zulu", "z"),
			slog.String("alpha", "a"),
			slog.String("context", "task"),
		})

		Expect(with.Handle(context.Background(), record(slog.LevelInfo, "done"))).To(Succeed())

		Expect(output.String()).To(Equal("\x1b[36m[I] [task] [a] [z] \x1b[0mdone\n"))
	})

	DescribeTable(
		"should color the message depending on the level",
		func(_ SpecContext, level slog.Level, expected string) {
			handler := logger.NewHandler()
			handler.SetOutput(output)

			handle(handler, record(level, "done"))

			Expect(output.String()).To(Equal(expected))
		},
		Entry("trace", logger.LevelTrace, "\x1b[35m[T] \x1b[0mdone\n"),
		Entry("debug", slog.LevelDebug, "\x1b[37m[D] \x1b[0mdone\n"),
		Entry("info", slog.LevelInfo, "\x1b[36m[I] \x1b[0mdone\n"),
		Entry("warn", slog.LevelWarn, "\x1b[33m[W] \x1b[0mdone\n"),
		Entry("error", slog.LevelError, "\x1b[31m[E] \x1b[0mdone\n"),
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

		Expect(output.String()).To(Equal("\x1b[35m[T] [task] \x1b[0mdone\n"))
	})

	It("should not report the caller unless it is asked for", func(_ SpecContext) {
		handler := logger.NewHandler()
		handler.SetOutput(output)

		var pcs [1]uintptr
		runtime.Callers(1, pcs[:])

		handle(handler, slog.NewRecord(time.Unix(0, 0), slog.LevelInfo, "done", pcs[0]))

		Expect(output.String()).To(Equal("\x1b[36m[I] \x1b[0mdone\n"))

		output.Reset()
		handler.SetReportCaller(true)

		handle(handler, slog.NewRecord(time.Unix(0, 0), slog.LevelInfo, "done", pcs[0]))

		Expect(output.String()).To(ContainSubstring("handler_test.go:"))
	})
})
