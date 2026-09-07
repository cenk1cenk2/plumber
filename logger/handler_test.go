package logger_test

import (
	"bytes"
	"context"
	"log/slog"
	"runtime"
	"time"

	"github.com/cenk1cenk2/plumber/v6/logger"

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
		handler := logger.NewHandler(logger.Options{
			FieldsOrder:   []string{"context", "status"},
			HideKeys:      true,
			NoColors:      true,
			NoEmptyFields: true,
			TrimMessages:  true,
		})
		handler.SetOutput(output)

		with := handler.WithAttrs([]slog.Attr{
			slog.String("status", "RUN"),
			slog.String("empty", ""),
			slog.String("context", "task"),
		})

		Expect(with.Handle(context.Background(), record(slog.LevelInfo, "done \n"))).To(Succeed())

		Expect(output.String()).To(Equal("[I] [task] [RUN] done\n"))
	})

	It("should format compact fields with keys", func(_ SpecContext) {
		handler := logger.NewHandler(logger.Options{
			FieldsOrder:      []string{"context", "status"},
			NoColors:         true,
			NoFieldsSpace:    true,
			ShowFullLevel:    true,
			NoUppercaseLevel: true,
		})
		handler.SetOutput(output)

		with := handler.WithAttrs([]slog.Attr{
			slog.String("context", "task"),
			slog.String("status", "END"),
		})

		Expect(with.Handle(context.Background(), record(slog.LevelWarn, "done"))).To(Succeed())

		Expect(output.String()).To(Equal("[warning][context:task][status:END] done\n"))
	})

	It("should redact configured secrets from messages", func(_ SpecContext) {
		secrets := []string{"secret-token"}
		handler := logger.NewHandler(logger.Options{
			NoColors: true,
			Secrets:  &secrets,
		})
		handler.SetOutput(output)

		handle(handler, record(slog.LevelInfo, "using secret-token"))

		Expect(output.String()).To(Equal("[I] using [REDACTED]\n"))
	})

	It("should not redact the secrets from the fields", func(_ SpecContext) {
		secrets := []string{"secret-token"}
		handler := logger.NewHandler(logger.Options{
			HideKeys: true,
			NoColors: true,
			Secrets:  &secrets,
		})
		handler.SetOutput(output)

		with := handler.WithAttrs([]slog.Attr{slog.String("context", "secret-token")})

		Expect(with.Handle(context.Background(), record(slog.LevelInfo, "done"))).To(Succeed())

		Expect(output.String()).To(Equal("[I] [secret-token] done\n"))
	})

	It("should overwrite a field that is set more than once", func(_ SpecContext) {
		handler := logger.NewHandler(logger.Options{
			FieldsOrder: []string{"context"},
			HideKeys:    true,
			NoColors:    true,
		})
		handler.SetOutput(output)

		with := handler.
			WithAttrs([]slog.Attr{slog.String("context", "task")}).
			WithAttrs([]slog.Attr{slog.String("context", "DISABLE")})

		Expect(with.Handle(context.Background(), record(slog.LevelInfo, "done"))).To(Succeed())

		Expect(output.String()).To(Equal("[I] [DISABLE] done\n"))
	})

	It("should sort the fields that are not ordered alphabetically", func(_ SpecContext) {
		handler := logger.NewHandler(logger.Options{
			FieldsOrder: []string{"context"},
			NoColors:    true,
		})
		handler.SetOutput(output)

		with := handler.WithAttrs([]slog.Attr{
			slog.String("zulu", "z"),
			slog.String("alpha", "a"),
			slog.String("context", "task"),
		})

		Expect(with.Handle(context.Background(), record(slog.LevelInfo, "done"))).To(Succeed())

		Expect(output.String()).To(Equal("[I] [context:task] [alpha:a] [zulu:z] done\n"))
	})

	DescribeTable(
		"should color the message depending on the level",
		func(_ SpecContext, level slog.Level, expected string) {
			handler := logger.NewHandler(logger.Options{})
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
		handler := logger.NewHandler(logger.Options{})

		Expect(handler.Enabled(context.Background(), slog.LevelInfo)).To(BeTrue())
		Expect(handler.Enabled(context.Background(), slog.LevelDebug)).To(BeFalse())

		handler.SetLevel(logger.LevelTrace)

		Expect(handler.Enabled(context.Background(), logger.LevelTrace)).To(BeTrue())
		Expect(handler.Level()).To(Equal(logger.LevelTrace))
	})

	It("should share the state with the handlers that are derived from it", func(_ SpecContext) {
		handler := logger.NewHandler(logger.Options{NoColors: true})
		with := handler.WithAttrs([]slog.Attr{slog.String("context", "task")})

		handler.SetOutput(output)
		handler.SetLevel(logger.LevelTrace)

		Expect(with.Enabled(context.Background(), logger.LevelTrace)).To(BeTrue())
		Expect(with.Handle(context.Background(), record(logger.LevelTrace, "done"))).To(Succeed())

		Expect(output.String()).To(Equal("[T] [context:task] done\n"))
	})

	It("should not report the caller unless it is asked for", func(_ SpecContext) {
		handler := logger.NewHandler(logger.Options{NoColors: true})
		handler.SetOutput(output)

		var pcs [1]uintptr
		runtime.Callers(1, pcs[:])

		handle(handler, slog.NewRecord(time.Unix(0, 0), slog.LevelInfo, "done", pcs[0]))

		Expect(output.String()).To(Equal("[I] done\n"))

		output.Reset()
		handler.SetReportCaller(true)

		handle(handler, slog.NewRecord(time.Unix(0, 0), slog.LevelInfo, "done", pcs[0]))

		Expect(output.String()).To(ContainSubstring("handler_test.go:"))
	})
})
