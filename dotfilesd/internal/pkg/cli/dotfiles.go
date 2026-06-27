package cli

import (
	"context"
	"fmt"
	"log/slog"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

func RunStatus(clients *Clients, sessionID string) error {
	slog.Debug("dotfiles status requested", "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.StatusRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Dot.Status(context.Background(), req)
	if err != nil {
		slog.Error("status failed", "error", err)
		return fmt.Errorf("status failed: %w", err)
	}
	s := resp.Msg
	slog.Debug("status response", "branch", s.GitBranch, "clean", s.GitClean)
	clean := "clean"
	if !s.GitClean {
		clean = "dirty"
	}
	fmt.Printf("branch: %s (%s)\n", s.GitBranch, clean)
	fmt.Printf("last:   %s\n", s.LastCommit)

	// Hostname and uptime come from RuntimeInfo now.
	runtimeReq := connect.NewRequest(&dotfilesdv1.RuntimeInfoRequest{Session: sessionProto(sessionID)})
	runtimeResp, err := clients.Sys.RuntimeInfo(context.Background(), runtimeReq)
	if err == nil {
		fmt.Printf("host:   %s\n", runtimeResp.Msg.Hostname)
		fmt.Printf("uptime: %s\n", runtimeResp.Msg.Uptime)
	}
	return nil
}

// RunGit runs a git operation via the scripts/git/ script tree.
func RunGit(clients *Clients, sessionID, action, message, paths string) error {
	slog.Info("git via script", "action", action, "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.RunScriptRequest{
		Session: sessionProto(sessionID),
		Source: &dotfilesdv1.RunScriptRequest_RegisteredScript{
			RegisteredScript: "git/" + action,
		},
	})
	resp, err := clients.Script.RunScript(context.Background(), req)
	if err != nil {
		slog.Error("git script failed", "action", action, "error", err)
		return fmt.Errorf("git %s: %w", action, err)
	}
	allOK := true
	for _, step := range resp.Msg.Steps {
		if step.Stdout != "" {
			fmt.Print(step.Stdout)
		}
		if step.Stderr != "" {
			fmt.Print(step.Stderr)
		}
		if step.ExitCode != 0 {
			allOK = false
		}
	}
	if !allOK || !resp.Msg.AllSucceeded {
		return fmt.Errorf("git %s failed: %s", action, resp.Msg.Error)
	}
	return nil
}
