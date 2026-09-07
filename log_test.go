package plumber_test

import (
	"bytes"
	"fmt"

	"github.com/cenk1cenk2/plumber/v6"
	"github.com/cenk1cenk2/plumber/v6/logger"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("logger", func() {
	Describe("levels", func() {
		DescribeTable(
			"should keep the numeric values and the ascending verbosity",
			func(_ SpecContext, level plumber.LogLevel, value int, name string) {
				Expect(int(level)).To(Equal(value))
				Expect(level.String()).To(Equal(name))
			},
			Entry("panic", plumber.LOG_LEVEL_PANIC, 0, "panic"),
			Entry("fatal", plumber.LOG_LEVEL_FATAL, 1, "fatal"),
			Entry("error", plumber.LOG_LEVEL_ERROR, 2, "error"),
			Entry("warn", plumber.LOG_LEVEL_WARN, 3, "warning"),
			Entry("info", plumber.LOG_LEVEL_INFO, 4, "info"),
			Entry("debug", plumber.LOG_LEVEL_DEBUG, 5, "debug"),
			Entry("trace", plumber.LOG_LEVEL_TRACE, 6, "trace"),
		)

		It("should keep the default as the sentinel it is", func(_ SpecContext) {
			Expect(plumber.LOG_LEVEL_DEFAULT).To(Equal(plumber.LOG_LEVEL_PANIC))
			Expect(plumber.LOG_LEVEL_ERROR < plumber.LOG_LEVEL_WARN).To(BeTrue())
			Expect(plumber.LOG_LEVEL_WARN <= plumber.LOG_LEVEL_ERROR).To(BeFalse())
		})

		DescribeTable(
			"should parse the names of the levels",
			func(_ SpecContext, name string, expected plumber.LogLevel) {
				level, err := plumber.ParseLogLevel(name)

				Expect(err).ToNot(HaveOccurred())
				Expect(level).To(Equal(expected))
			},
			Entry("panic", "panic", plumber.LOG_LEVEL_PANIC),
			Entry("fatal", "fatal", plumber.LOG_LEVEL_FATAL),
			Entry("error", "error", plumber.LOG_LEVEL_ERROR),
			Entry("warn", "warn", plumber.LOG_LEVEL_WARN),
			Entry("warning", "warning", plumber.LOG_LEVEL_WARN),
			Entry("info", "info", plumber.LOG_LEVEL_INFO),
			Entry("debug", "DEBUG", plumber.LOG_LEVEL_DEBUG),
			Entry("trace", "trace", plumber.LOG_LEVEL_TRACE),
		)

		It("should fail on a level that does not exist", func(_ SpecContext) {
			_, err := plumber.ParseLogLevel("verbose")

			Expect(err).To(MatchError(ContainSubstring("verbose")))
		})
	})

	Describe("wrapper", func() {
		var (
			output *bytes.Buffer
			log    *plumber.Logger
		)

		BeforeEach(func() {
			output = &bytes.Buffer{}

			handler := logger.NewHandler(logger.Options{
				FieldsOrder:   []string{plumber.LOG_FIELD_CONTEXT, plumber.LOG_FIELD_STATUS},
				HideKeys:      true,
				NoColors:      true,
				NoEmptyFields: true,
				TrimMessages:  true,
				CallerFirst:   true,
			})
			handler.SetOutput(output)

			log = plumber.NewLogger(handler)
			log.SetLevel(plumber.LOG_LEVEL_TRACE)
		})

		DescribeTable(
			"should keep the printf ergonomics of every level",
			func(_ SpecContext, fn func(*plumber.Logger), expected string) {
				fn(log)

				Expect(output.String()).To(Equal(expected))
			},
			Entry("tracef", func(l *plumber.Logger) { l.Tracef("%s -> %d", "run", 1) }, "[T] run -> 1\n"),
			Entry("traceln", func(l *plumber.Logger) { l.Traceln("run", 1) }, "[T] run 1\n"),
			Entry("debugf", func(l *plumber.Logger) { l.Debugf("%s", "run") }, "[D] run\n"),
			Entry("debugln", func(l *plumber.Logger) { l.Debugln("run") }, "[D] run\n"),
			Entry("infof", func(l *plumber.Logger) { l.Infof("%s", "run") }, "[I] run\n"),
			Entry("infoln", func(l *plumber.Logger) { l.Infoln("run") }, "[I] run\n"),
			Entry("info", func(l *plumber.Logger) { l.Info("run") }, "[I] run\n"),
			Entry("warnf", func(l *plumber.Logger) { l.Warnf("%s", "run") }, "[W] run\n"),
			Entry("warnln", func(l *plumber.Logger) { l.Warnln("run") }, "[W] run\n"),
			Entry("errorf", func(l *plumber.Logger) { l.Errorf("%s", "run") }, "[E] run\n"),
			Entry("errorln", func(l *plumber.Logger) { l.Errorln(fmt.Errorf("run")) }, "[E] run\n"),
			Entry("error", func(l *plumber.Logger) { l.Error("run") }, "[E] run\n"),
			Entry(
				"logf",
				func(l *plumber.Logger) { l.Logf(plumber.LOG_LEVEL_WARN, "%s", "run") },
				"[W] run\n",
			),
			Entry("log", func(l *plumber.Logger) { l.Log(plumber.LOG_LEVEL_DEBUG, "run") }, "[D] run\n"),
			Entry(
				"logln",
				func(l *plumber.Logger) {
					line := "run\n"

					l.Logln(plumber.LOG_LEVEL_TRACE, line)
				},
				"[T] run\n",
			),
		)

		It("should carry the fields that are set on it", func(_ SpecContext) {
			log.With(plumber.LOG_FIELD_CONTEXT, "task").
				With(plumber.LOG_FIELD_STATUS, "RUN").
				Infoln("done")

			Expect(output.String()).To(Equal("[I] [task] [RUN] done\n"))
		})

		It("should never let the fields of a logger leak into the one it is derived from", func(_ SpecContext) {
			derived := log.With(plumber.LOG_FIELD_CONTEXT, "task")

			log.Infoln("root")
			derived.Infoln("derived")

			Expect(output.String()).To(Equal("[I] root\n[I] [task] derived\n"))
		})

		It("should gate the messages with the level of the application", func(_ SpecContext) {
			log.SetLevel(plumber.LOG_LEVEL_WARN)

			Expect(log.GetLevel()).To(Equal(plumber.LOG_LEVEL_WARN))

			log.Infoln("info")
			log.Warnln("warn")

			Expect(output.String()).To(Equal("[W] warn\n"))
		})

		It("should report the caller of the message and never the logger itself", func(_ SpecContext) {
			log.SetReportCaller(true)

			log.Infoln("done")

			Expect(output.String()).To(ContainSubstring("log_test.go:"))
			Expect(output.String()).ToNot(ContainSubstring("log.go:"))
		})
	})

	Describe("capture", func() {
		It("should record the messages that are logged through it", func(_ SpecContext) {
			log, capture := plumbertests.NewCaptureLogger()

			log.Warnf("%s has failed", "job")

			Expect(capture.Messages()).To(Equal([]string{"job has failed"}))
		})
	})
})
