package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"dotfilesd/internal/pkg/logging"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

// DefaultSessionID is set by the root command's PersistentPreRunE so that
// plugin command builders (which run later in RunE) can pass the session
// context through executor calls for diagnostics traceability.
var DefaultSessionID string

// cliTraceLevel is a custom slog level for TRACE, below Debug.
const cliTraceLevel = slog.Level(-8)

// sessionProto creates a Session protobuf message from a session ID string.
// Returns nil if the ID is empty, so the daemon creates an ephemeral session.
// When a session is created, the current shell context (cwd, SHELL, env) is
// captured and sent to the daemon so commands behave as if run in the CLI
// terminal directly.
func sessionProto(id string) *dotfilesdv1.Session {
	if id == "" {
		return nil
	}
	cwd, _ := os.Getwd()
	shell := os.Getenv("SHELL")
	env := make(map[string]string)
	for _, e := range os.Environ() {
		if k, v, ok := strings.Cut(e, "="); ok {
			env[k] = v
		}
	}
	return &dotfilesdv1.Session{
		Id: id,
		Shell: &dotfilesdv1.Shell{
			CurrentShell: shell,
			Cwd:          cwd,
			Env:          env,
		},
	}
}

// ParseLogLevelStr converts a level string to slog.Level.
// Valid: trace, debug, info, warn, error.
func parseLogLevelStr(s string) (slog.Level, bool) {
	switch strings.ToLower(s) {
	case "trace":
		return cliTraceLevel, true
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}

// SetupLogging configures the CLI's structured logger.
//
// Logs go to a rotating file (<DOTFILESD_LOG_DIR>/dotfilesctl.log).
// The new logging package is configured with:
//   - A rotating file sink (for CLI history)
//   - A stderr sink (for interactive CLI output)
//   - Level-based filtering
//
// level can be: trace, debug, info, warn, error.
// If empty or invalid, info is used.
func SetupLogging(level string) {
	logDir := os.Getenv("DOTFILESD_LOG_DIR")
	if logDir == "" {
		logDir = os.Getenv("HOME") + "/dotfilesd/logs"
	}
	os.MkdirAll(logDir, 0755)

	// Validate and normalise the level.
	lvl := level
	if lvl == "" {
		lvl = "info"
	}
	if _, ok := logging.ParseLevel(lvl); !ok {
		lvl = "info"
	}

	cfg := logging.Config{
		Formatter: logging.FormatterConfig{
			TimeFormat: "15:04:05.000",
			Color:      boolPtr(false), // CLI logs to file only by default
		},
		Sinks: []logging.SinkConfig{
			{
				Name:       "file",
				Type:       "rotating_file",
				Path:       logDir + "/dotfilesctl.log",
				MaxSizeMB:  10,
				MaxBackups: 5,
				MaxAgeDays: 30,
			},
		},
		Loggers: []logging.LoggerConfig{
			{
				Name:   "root",
				Level:  lvl,
				Sinks:  []string{"file"},
				Source: false,
			},
		},
	}

	logging.Configure(cfg)

	// Also bridge to slog so CLI code using slog still works.
	slogHandler := logging.NewSlogHandler(logging.NewPackageLogger("cli"))
	slog.SetDefault(slog.New(slogHandler))
}

// boolPtr returns a pointer to a bool.
func boolPtr(v bool) *bool { return &v }

func Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
