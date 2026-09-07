package plumber_test

import (
	"log/slog"

	"github.com/cenk1cenk2/plumber/v7"
	"github.com/cenk1cenk2/plumber/v7/logger"
	plumbertests "github.com/cenk1cenk2/plumber/v7/tests"

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

	Describe("controls", func() {
		DescribeTable(
			"should gate the messages of the application with the level that is set",
			func(ctx SpecContext, level plumber.LogLevel, expected slog.Level) {
				app := plumbertests.NewPlumber().Plumber
				app.SetLoggerLevel(level)

				Expect(app.GetLoggerLevel()).To(Equal(level))
				Expect(app.Log.Enabled(ctx, expected)).To(BeTrue())
				Expect(app.Log.Enabled(ctx, expected-1)).To(BeFalse())
			},
			Entry("trace", plumber.LOG_LEVEL_TRACE, logger.LevelTrace),
			Entry("debug", plumber.LOG_LEVEL_DEBUG, slog.LevelDebug),
			Entry("info", plumber.LOG_LEVEL_INFO, slog.LevelInfo),
			Entry("warn", plumber.LOG_LEVEL_WARN, slog.LevelWarn),
			Entry("error", plumber.LOG_LEVEL_ERROR, slog.LevelError),
		)

		It("should hand over the writer and the caller reporting of the application", func(_ SpecContext) {
			app := plumbertests.NewPlumber().Plumber

			Expect(app.GetLoggerOutput()).To(Equal(GinkgoWriter))
			Expect(app.GetLoggerReportCaller()).To(BeFalse())

			app.SetLoggerReportCaller(true)

			Expect(app.GetLoggerReportCaller()).To(BeTrue())
		})
	})
})
