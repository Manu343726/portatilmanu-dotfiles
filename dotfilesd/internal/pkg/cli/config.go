package cli

import (
	"context"
	"fmt"
	"os"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

func RunReload(clients *Clients, sessionID, targetStr string) error {
	target := dotfilesdv1.ReloadTarget_RELOAD_TARGET_ALL
	if targetStr != "" {
		target = ParseReloadTarget(targetStr)
		if target == dotfilesdv1.ReloadTarget_RELOAD_TARGET_UNSPECIFIED {
			return fmt.Errorf("unknown target: %s (valid: tmux, i3, kitty, all)", targetStr)
		}
	}
	req := connect.NewRequest(&dotfilesdv1.ReloadRequest{Target: target, Session: sessionProto(sessionID)})
	resp, err := clients.Cfg.Reload(context.Background(), req)
	if err != nil {
		return fmt.Errorf("reload failed: %w", err)
	}
	for _, r := range resp.Msg.Results {
		status := "ok"
		if !r.Success {
			status = "error"
		}
		fmt.Printf("%-6s %s: %s\n", status, r.Target, r.Message)
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
	req := connect.NewRequest(&dotfilesdv1.ReconfigureRequest{
		LogLevel: logLevel,
		Session:  sessionProto(sessionID),
	})
	resp, err := clients.Cfg.Reconfigure(context.Background(), req)
	if err != nil {
		return fmt.Errorf("reconfigure failed: %w", err)
	}
	fmt.Println(resp.Msg.Message)
	if !resp.Msg.Success {
		os.Exit(1)
	}
	return nil
}

func RunRestart(clients *Clients, sessionID string) error {
	req := connect.NewRequest(&dotfilesdv1.RestartRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Cfg.Restart(context.Background(), req)
	if err != nil {
		return fmt.Errorf("restart failed: %w", err)
	}
	fmt.Println(resp.Msg.Message)
	return nil
}
