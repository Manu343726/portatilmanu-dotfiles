package daemon

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"gopkg.in/natefinch/lumberjack.v2"
)

const levelTrace = slog.Level(-8)

var logLevelVar slog.LevelVar

func setupLogging(logDir, level string, maxMB, backups, age int) {
	os.MkdirAll(logDir, 0755)

	fileWriter := &lumberjack.Logger{
		Filename:   logDir + "/dotfilesd.log",
		MaxSize:    maxMB,
		MaxBackups: backups,
		MaxAge:     age,
		Compress:   true,
	}

	_, lvl, ok := parseLogLevel(level)
	if !ok {
		lvl = slog.LevelInfo
	}
	logLevelVar.Set(lvl)

	multi := io.MultiWriter(os.Stdout, fileWriter)
	handler := slog.NewTextHandler(multi, &slog.HandlerOptions{
		Level: &logLevelVar,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				level := a.Value.Any().(slog.Level)
				if level == levelTrace {
					a.Value = slog.StringValue("TRACE")
				}
			}
			return a
		},
	})
	slog.SetDefault(slog.New(handler))
}

func parseLogLevel(s string) (dotfilesdv1.LogLevel, slog.Level, bool) {
	key := "LOG_LEVEL_" + strings.ToUpper(s)
	v, ok := dotfilesdv1.LogLevel_value[key]
	if !ok {
		switch strings.ToLower(s) {
		case "warn", "warning":
			return dotfilesdv1.LogLevel_LOG_LEVEL_WARN, slog.LevelWarn, true
		}
		return dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED, slog.LevelInfo, false
	}
	enum := dotfilesdv1.LogLevel(v)
	return enum, logLevelToSlog(enum), true
}

func logLevelToSlog(l dotfilesdv1.LogLevel) slog.Level {
	switch l {
	case dotfilesdv1.LogLevel_LOG_LEVEL_TRACE:
		return levelTrace
	case dotfilesdv1.LogLevel_LOG_LEVEL_DEBUG:
		return slog.LevelDebug
	case dotfilesdv1.LogLevel_LOG_LEVEL_INFO:
		return slog.LevelInfo
	case dotfilesdv1.LogLevel_LOG_LEVEL_WARN:
		return slog.LevelWarn
	case dotfilesdv1.LogLevel_LOG_LEVEL_ERROR:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
