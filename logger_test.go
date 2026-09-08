package plumber_test

import (
	"bytes"
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
			Entry("panic", plumber.LogLevelPanic, 0, "panic"),
			Entry("fatal", plumber.LogLevelFatal, 1, "fatal"),
			Entry("error", plumber.LogLevelError, 2, "error"),
			Entry("warn", plumber.LogLevelWarn, 3, "warning"),
			Entry("info", plumber.LogLevelInfo, 4, "info"),
			Entry("debug", plumber.LogLevelDebug, 5, "debug"),
			Entry("trace", plumber.LogLevelTrace, 6, "trace"),
		)

		It("should keep the default as the sentinel it is", func(_ SpecContext) {
			Expect(plumber.LogLevelDefault).To(Equal(plumber.LogLevelPanic))
			Expect(plumber.LogLevelError < plumber.LogLevelWarn).To(BeTrue())
			Expect(plumber.LogLevelWarn <= plumber.LogLevelError).To(BeFalse())
		})

		DescribeTable(
			"should parse the names of the levels",
			func(_ SpecContext, name string, expected plumber.LogLevel) {
				level, err := plumber.ParseLogLevel(name)

				Expect(err).ToNot(HaveOccurred())
				Expect(level).To(Equal(expected))
			},
			Entry("panic", "panic", plumber.LogLevelPanic),
			Entry("fatal", "fatal", plumber.LogLevelFatal),
			Entry("error", "error", plumber.LogLevelError),
			Entry("warn", "warn", plumber.LogLevelWarn),
			Entry("warning", "warning", plumber.LogLevelWarn),
			Entry("info", "info", plumber.LogLevelInfo),
			Entry("debug", "DEBUG", plumber.LogLevelDebug),
			Entry("trace", "trace", plumber.LogLevelTrace),
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
			Entry("trace", plumber.LogLevelTrace, logger.LevelTrace),
			Entry("debug", plumber.LogLevelDebug, slog.LevelDebug),
			Entry("info", plumber.LogLevelInfo, slog.LevelInfo),
			Entry("warn", plumber.LogLevelWarn, slog.LevelWarn),
			Entry("error", plumber.LogLevelError, slog.LevelError),
		)

		It("should hand over the writer and the caller reporting of the application", func(_ SpecContext) {
			app := plumbertests.NewPlumber().Plumber
			output := &bytes.Buffer{}
			app.SetLoggerOutput(output)

			app.Log.Info("done")

			Expect(output.String()).To(ContainSubstring("done"))
			Expect(output.String()).ToNot(ContainSubstring("logger_test.go:"))

			output.Reset()
			app.SetLoggerReportCaller(true)

			app.Log.Info("done")

			Expect(output.String()).To(ContainSubstring("logger_test.go:"))
		})
	})
})
