package cli

import (
	"context"
	"fmt"
	"os"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

func RunStatus(clients *Clients) error {
	resp, err := clients.Dot.Status(context.Background(), connect.NewRequest(&dotfilesdv1.StatusRequest{}))
	if err != nil {
		return fmt.Errorf("status failed: %w", err)
	}
	s := resp.Msg
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

func RunGit(clients *Clients, actionStr, message, paths string) error {
	action := ParseGitAction(actionStr)
	if action == dotfilesdv1.GitAction_GIT_ACTION_UNSPECIFIED {
		return fmt.Errorf("unknown git action: %s (valid: status, diff, add, commit, push, log)", actionStr)
	}

	resp, err := clients.Dot.Git(context.Background(), connect.NewRequest(&dotfilesdv1.GitRequest{
		Action: action, Message: message, Paths: paths,
	}))
	if err != nil {
		return fmt.Errorf("git failed: %w", err)
	}
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
