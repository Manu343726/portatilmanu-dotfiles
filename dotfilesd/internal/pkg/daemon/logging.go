package daemon

import (
	"log/slog"
	"os"
	"strings"

	"dotfilesd/internal/pkg/logging"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

const levelTrace = slog.Level(-8)

// logLevelVar allows runtime reconfiguration of the slog level.
// It's used by the config server's Reconfigure RPC for backward compat.
var logLevelVar slog.LevelVar

// setupLogging configures the new logging package based on daemon config.
// It creates:
//   - A rotating file sink for daemon logs (<LogDir>/dotfilesd.log)
//   - A stdout sink
//   - An slog bridge so existing code using slog still works
//
// After this, package-level logging functions (logging.Info, etc.) and
// slog.SetDefault are both operational.
func (d *Daemon) setupLogging() {
	logDir := d.config.LogDir
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = home + "/dotfilesd/logs"
	}
	os.MkdirAll(logDir, 0755)

	// Build the logging config with daemon settings.
	cfg := logging.Config{
		Formatter: logging.FormatterConfig{
			TimeFormat:     "15:04:05.000",
			Color:          boolPtr(!isCI()),
			SourceLocation: true,
			NoColorPrefix:  false,
		},
		Sinks: []logging.SinkConfig{
			{
				Name: "stdout",
				Type: "stdout",
			},
			{
				Name:       "file",
				Type:       "rotating_file",
				Path:       logDir + "/dotfilesd.log",
				MaxSizeMB:  d.config.LogMaxMB,
				MaxBackups: d.config.LogBackup,
				MaxAgeDays: d.config.LogAge,
			},
		},
		Loggers: []logging.LoggerConfig{
			{
				Name:   "root",
				Level:  d.config.LogLevel,
				Sinks:  []string{"stdout", "file"},
				Source: false,
			},
		},
		DefaultMaxSizeMB:  nonZero(d.config.LogMaxMB, 10),
		DefaultMaxBackups: nonZero(d.config.LogBackup, 5),
		DefaultMaxAgeDays: nonZero(d.config.LogAge, 30),
	}

	// Configure the global logging.
	logging.Configure(cfg)
	d.logger = logging.Global()

	// Also bridge to slog so that existing code using slog (e.g. the
	// execution context server) still produces output through our system.
	slogHandler := logging.NewSlogHandler(logging.NewPackageLogger("daemon"))
	slog.SetDefault(slog.New(slogHandler))
}

// nonZero returns val if non-zero, else def.
func nonZero(val, def int) int {
	if val != 0 {
		return val
	}
	return def
}

// boolPtr returns a pointer to a bool.
func boolPtr(v bool) *bool { return &v }

// isCI returns true if running in a CI environment (no TTY).
func isCI() bool {
	// Detect headless environments.
	_, ok := os.LookupEnv("CI")
	if ok {
		return true
	}
	_, ok = os.LookupEnv("DOTFILESD_NO_COLOR")
	if ok {
		return true
	}
	return false
}

// parseLogLevel converts a log level string to a protobuf LogLevel and
// slog.Level pair. Used by the exec server for per-command level overrides.
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

// logLevelToSlog converts a protobuf LogLevel to slog.Level.
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
