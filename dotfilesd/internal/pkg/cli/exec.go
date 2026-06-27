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
	req := connect.NewRequest(&dotfilesdv1.ExecStreamRequest{
		Command: command,
		Sudo:    sudo,
		Session: sessionProto(sessionID),
	})
	stream, err := clients.Exec.ExecStream(context.Background(), req)
	if err != nil {
		slog.Error("exec stream failed to start", "command", command, "error", err)
		return fmt.Errorf("exec failed: %w", err)
	}

	for stream.Receive() {
		chunk := stream.Msg()
		if len(chunk.StdoutChunk) > 0 {
			fmt.Print(string(chunk.StdoutChunk))
		}
		if chunk.Done {
			if chunk.ErrorMessage != "" {
				fmt.Fprintln(os.Stderr, chunk.ErrorMessage)
				os.Exit(1)
			}
			if chunk.ExitCode != 0 {
				os.Exit(int(chunk.ExitCode))
			}
			return nil
		}
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("exec stream: %w", err)
	}
	return nil
}

func RunExec(clients *Clients, sessionID, command string) error {
	return execCommand(clients, sessionID, command, false)
}

func RunSudoExec(clients *Clients, sessionID, command string) error {
	return execCommand(clients, sessionID, command, true)
}
