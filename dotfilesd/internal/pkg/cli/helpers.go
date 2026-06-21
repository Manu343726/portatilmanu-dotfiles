package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"gopkg.in/natefinch/lumberjack.v2"
)

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
// Logs go exclusively to the log file — never to stdout or stderr.
// The log file is created in the DOTFILESD_LOG_DIR (defaults to
// ~/dotfilesd/logs/dotfilesctl.log) with rotation via lumberjack.
//
// level can be: trace, debug, info, warn, error.
// If empty or invalid, info is used.
func SetupLogging(level string) {
	logDir := os.Getenv("DOTFILESD_LOG_DIR")
	if logDir == "" {
		logDir = os.Getenv("HOME") + "/dotfilesd/logs"
	}
	os.MkdirAll(logDir, 0755)

	writer := &lumberjack.Logger{
		Filename:   logDir + "/dotfilesctl.log",
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
	}

	lvl, ok := parseLogLevelStr(level)
	if !ok {
		lvl = slog.LevelInfo
	}

	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				level := a.Value.Any().(slog.Level)
				if level == cliTraceLevel {
					a.Value = slog.StringValue("TRACE")
				}
			}
			return a
		},
	})
	slog.SetDefault(slog.New(handler))
}

func Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
