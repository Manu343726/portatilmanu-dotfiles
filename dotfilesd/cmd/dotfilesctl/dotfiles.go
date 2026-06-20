package main

import (
	"github.com/spf13/cobra"

	"dotfilesd/internal/pkg/cli"
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
			return cli.RunStatus(clients)
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
			message, _ := cmd.Flags().GetString("message")
			paths, _ := cmd.Flags().GetString("paths")
			return cli.RunGit(clients, args[0], message, paths)
		},
	}
	cmd.Flags().StringP("message", "m", "", "commit message")
	cmd.Flags().String("paths", "", "files to stage")
	return cmd
}
