package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

func execCommand(clients *Clients, sessionID, command string, sudo bool) error {
	slog.Info("exec", "command", command, "sudo", sudo, "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.ExecRequest{
		Command: command,
		Sudo:    sudo,
		Session: sessionProto(sessionID),
	})
	resp, err := clients.Exec.Exec(context.Background(), req)
	if err != nil {
		slog.Error("exec failed", "command", command, "error", err)
		return fmt.Errorf("exec failed: %w", err)
	}
	slog.Debug("exec result", "command", command, "exit_code", resp.Msg.ExitCode,
		"stdout_len", len(resp.Msg.Stdout), "stderr_len", len(resp.Msg.Stderr))
	if resp.Msg.Stdout != "" {
		fmt.Print(resp.Msg.Stdout)
	}
	if resp.Msg.Stderr != "" {
		fmt.Fprint(os.Stderr, resp.Msg.Stderr)
	}
	if resp.Msg.ExitCode != 0 {
		os.Exit(int(resp.Msg.ExitCode))
	}
	return nil
}

func RunExec(clients *Clients, sessionID, command string) error {
	return execCommand(clients, sessionID, command, false)
}

func RunSudoExec(clients *Clients, sessionID, command string) error {
	return execCommand(clients, sessionID, command, true)
}
