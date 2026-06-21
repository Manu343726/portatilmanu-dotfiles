package cli

import (
	"context"
	"fmt"
	"os"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

func execCommand(clients *Clients, sessionID, command string, sudo bool) error {
	req := connect.NewRequest(&dotfilesdv1.ExecRequest{
		Command: command,
		Sudo:    sudo,
		Session: sessionProto(sessionID),
	})
	resp, err := clients.Exec.Exec(context.Background(), req)
	if err != nil {
		return fmt.Errorf("exec failed: %w", err)
	}
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
