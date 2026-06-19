package main

import (
	"context"
	"fmt"
	"os"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"github.com/spf13/cobra"
)

func newDotfilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dotfiles",
		Short: "dotfiles repository management",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "show dotfiles repo status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := dotClient.Status(context.Background(), connect.NewRequest(&dotfilesdv1.StatusRequest{}))
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
		},
	})
	cmd.AddCommand(newGitCmd())
	return cmd
}

func newGitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git <action> [-- <paths>]",
		Short: "git operations (status|diff|add|commit|push|log)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := parseGitAction(args[0])
			if action == dotfilesdv1.GitAction_GIT_ACTION_UNSPECIFIED {
				return fmt.Errorf("unknown git action: %s (valid: status, diff, add, commit, push, log)", args[0])
			}
			message, _ := cmd.Flags().GetString("message")
			paths, _ := cmd.Flags().GetString("paths")

			resp, err := dotClient.Git(context.Background(), connect.NewRequest(&dotfilesdv1.GitRequest{
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
		},
	}

	cmd.Flags().StringP("message", "m", "", "commit message")
	cmd.Flags().String("paths", "", "files to stage")
	return cmd
}
