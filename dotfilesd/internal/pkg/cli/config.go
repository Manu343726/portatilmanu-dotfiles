package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// RunReload reloads dotfiles configs by running scripts in the reload/ tree.
func RunReload(clients *Clients, sessionID, target string) error {
	if target == "" {
		target = "all"
	}
	slog.Info("config reload via script", "target", target, "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.RunScriptRequest{
		Session: sessionProto(sessionID),
		Source: &dotfilesdv1.RunScriptRequest_RegisteredScript{
			RegisteredScript: "reload/" + target,
		},
	})
	resp, err := clients.Script.RunScript(context.Background(), req)
	if err != nil {
		slog.Error("reload script failed", "target", target, "error", err)
		return fmt.Errorf("reload %s failed: %w", target, err)
	}
	for _, step := range resp.Msg.Steps {
		status := "ok"
		if step.ExitCode != 0 {
			status = "error"
		}
		fmt.Printf("%-6s %s: %s\n", status, step.SourceLine, step.Stdout)
		if step.Stderr != "" {
			fmt.Fprint(os.Stderr, step.Stderr)
		}
	}
	return nil
}

func RunReconfigure(clients *Clients, sessionID, levelStr string) error {
	if levelStr == "" {
		return fmt.Errorf("--log-level is required (trace, debug, info, warn, error)")
	}
	logLevel := ParseLogLevel(levelStr)
	if logLevel == dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED {
		return fmt.Errorf("invalid log level: %s (valid: trace, debug, info, warn, error)", levelStr)
	}
	slog.Info("daemon reconfigure", "new_level", levelStr, "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.ReconfigureRequest{
		LogLevel: logLevel,
		Session:  sessionProto(sessionID),
	})
	resp, err := clients.Cfg.Reconfigure(context.Background(), req)
	if err != nil {
		slog.Error("reconfigure failed", "error", err)
		return fmt.Errorf("reconfigure failed: %w", err)
	}
	fmt.Println(resp.Msg.Message)
	if !resp.Msg.Success {
		os.Exit(1)
	}
	return nil
}

func RunRestart(clients *Clients, sessionID string) error {
	slog.Info("daemon restart requested", "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.RestartRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Cfg.Restart(context.Background(), req)
	if err != nil {
		slog.Error("restart failed", "error", err)
		return fmt.Errorf("restart failed: %w", err)
	}
	fmt.Println(resp.Msg.Message)
	return nil
}
