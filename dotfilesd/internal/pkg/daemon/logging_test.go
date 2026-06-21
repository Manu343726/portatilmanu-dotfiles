package daemon

import (
	"log/slog"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Logging", func() {
	Describe("parseLogLevel", func() {
		It("parses trace", func() {
			enum, level, ok := parseLogLevel("trace")
			Expect(ok).To(BeTrue())
			Expect(enum).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_TRACE))
			Expect(level).To(Equal(levelTrace))
		})

		It("parses debug", func() {
			enum, level, ok := parseLogLevel("debug")
			Expect(ok).To(BeTrue())
			Expect(enum).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_DEBUG))
			Expect(level).To(Equal(slog.LevelDebug))
		})

		It("parses info", func() {
			enum, level, ok := parseLogLevel("info")
			Expect(ok).To(BeTrue())
			Expect(enum).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_INFO))
			Expect(level).To(Equal(slog.LevelInfo))
		})

		It("parses warn", func() {
			enum, level, ok := parseLogLevel("warn")
			Expect(ok).To(BeTrue())
			Expect(enum).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_WARN))
			Expect(level).To(Equal(slog.LevelWarn))
		})

		It("parses warning as warn", func() {
			enum, level, ok := parseLogLevel("warning")
			Expect(ok).To(BeTrue())
			Expect(enum).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_WARN))
			Expect(level).To(Equal(slog.LevelWarn))
		})

		It("parses error", func() {
			enum, level, ok := parseLogLevel("error")
			Expect(ok).To(BeTrue())
			Expect(enum).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_ERROR))
			Expect(level).To(Equal(slog.LevelError))
		})

		It("returns false for invalid level", func() {
			_, _, ok := parseLogLevel("invalid")
			Expect(ok).To(BeFalse())
		})

		It("returns false for empty string", func() {
			_, _, ok := parseLogLevel("")
			Expect(ok).To(BeFalse())
		})

		It("is case insensitive", func() {
			_, _, ok := parseLogLevel("DEBUG")
			Expect(ok).To(BeTrue())

			_, _, ok2 := parseLogLevel("Info")
			Expect(ok2).To(BeTrue())
		})
	})

	Describe("logLevelToSlog", func() {
		It("converts trace level", func() {
			Expect(logLevelToSlog(dotfilesdv1.LogLevel_LOG_LEVEL_TRACE)).To(Equal(levelTrace))
		})

		It("converts debug level", func() {
			Expect(logLevelToSlog(dotfilesdv1.LogLevel_LOG_LEVEL_DEBUG)).To(Equal(slog.LevelDebug))
		})

		It("converts info level", func() {
			Expect(logLevelToSlog(dotfilesdv1.LogLevel_LOG_LEVEL_INFO)).To(Equal(slog.LevelInfo))
		})

		It("converts warn level", func() {
			Expect(logLevelToSlog(dotfilesdv1.LogLevel_LOG_LEVEL_WARN)).To(Equal(slog.LevelWarn))
		})

		It("converts error level", func() {
			Expect(logLevelToSlog(dotfilesdv1.LogLevel_LOG_LEVEL_ERROR)).To(Equal(slog.LevelError))
		})

		It("defaults to info for unspecified", func() {
			Expect(logLevelToSlog(dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED)).To(Equal(slog.LevelInfo))
		})
	})
})
