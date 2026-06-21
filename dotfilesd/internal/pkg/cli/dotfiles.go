package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

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
	fmt.Printf("host:   %s\n", s.Hostname)
	fmt.Printf("uptime: %s\n", s.Uptime)
	return nil
}

func RunGit(clients *Clients, sessionID, actionStr, message, paths string) error {
	action := ParseGitAction(actionStr)
	if action == dotfilesdv1.GitAction_GIT_ACTION_UNSPECIFIED {
		slog.Warn("unknown git action", "action", actionStr)
		return fmt.Errorf("unknown git action: %s (valid: status, diff, add, commit, push, log)", actionStr)
	}

	slog.Info("git", "action", actionStr, "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.GitRequest{
		Action: action, Message: message, Paths: paths,
		Session: sessionProto(sessionID),
	})
	resp, err := clients.Dot.Git(context.Background(), req)
	if err != nil {
		slog.Error("git failed", "action", actionStr, "error", err)
		return fmt.Errorf("git failed: %w", err)
	}
	slog.Debug("git result", "action", actionStr, "exit_code", resp.Msg.ExitCode)
	if resp.Msg.Stderr != "" {
		fmt.Fprint(os.Stderr, resp.Msg.Stderr)
	}
	if resp.Msg.Stdout != "" {
		fmt.Print(resp.Msg.Stdout)
	}
	if resp.Msg.ExitCode != 0 {
		os.Exit(int(resp.Msg.ExitCode))
	}
	return nil
}
