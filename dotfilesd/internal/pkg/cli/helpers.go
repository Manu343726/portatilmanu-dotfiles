package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"gopkg.in/natefinch/lumberjack.v2"
)

// sessionProto creates a Session protobuf message from a session ID string.
// Returns nil if the ID is empty, so the daemon creates an ephemeral session.
func sessionProto(id string) *dotfilesdv1.Session {
	if id == "" {
		return nil
	}
	return &dotfilesdv1.Session{Id: id}
}

func SetupLogging(verbose bool) {
	logDir := os.Getenv("DOTFILESD_LOG_DIR")
	if logDir == "" {
		logDir = os.Getenv("HOME") + "/dotfilesd/logs"
	}
	os.MkdirAll(logDir, 0755)

	fileWriter := &lumberjack.Logger{
		Filename:   logDir + "/dotfilesctl.log",
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
	}

	var writers []io.Writer
	writers = append(writers, fileWriter)
	if verbose {
		writers = append(writers, os.Stderr)
	}

	var multi io.Writer
	if len(writers) == 1 {
		multi = writers[0]
	} else {
		multi = io.MultiWriter(writers...)
	}

	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(multi, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}

func Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
