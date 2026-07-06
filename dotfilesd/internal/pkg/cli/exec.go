package cli

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

func execCommand(clients *Clients, sessionID, command string, sudo, noNewline bool) error {
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

	var lastChunk []byte
	for stream.Receive() {
		chunk := stream.Msg()
		if len(chunk.StdoutChunk) > 0 {
			if noNewline {
				lastChunk = chunk.StdoutChunk
			} else {
				fmt.Print(string(chunk.StdoutChunk))
			}
		}
		if chunk.Done {
			if chunk.ErrorMessage != "" {
				fmt.Fprintln(os.Stderr, chunk.ErrorMessage)
				os.Exit(1)
			}
			if noNewline && len(lastChunk) > 0 {
				// Strip trailing newline from the last chunk.
				lastChunk = bytes.TrimRight(lastChunk, "\n\r")
				fmt.Print(string(lastChunk))
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

func RunExec(clients *Clients, sessionID, command string, noNewline bool) error {
	return execCommand(clients, sessionID, command, false, noNewline)
}

func RunSudoExec(clients *Clients, sessionID, command string, noNewline bool) error {
	return execCommand(clients, sessionID, command, true, noNewline)
}
