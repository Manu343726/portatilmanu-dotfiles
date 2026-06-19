package main

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

func hasSudo() bool {
	_, err := exec.LookPath("sudo")
	return err == nil
}

func hasPkexec() bool {
	_, err := exec.LookPath("pkexec")
	return err == nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func runCmd(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func runCmdFull(name string, args ...string) (string, string, int) {
	var stdout, stderr strings.Builder
	cmd := exec.Command(name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return stdout.String(), stderr.String(), exitCode
}

func parseLogLevel(s string) (dotfilesdv1.LogLevel, slog.Level, bool) {
	key := "LOG_LEVEL_" + strings.ToUpper(s)
	v, ok := dotfilesdv1.LogLevel_value[key]
	if !ok {
		if strings.ToLower(s) == "warn" {
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

func fmtSscanf(str string, v any) (int, error) {
	return fmt.Sscanf(str, "%d", v)
}
