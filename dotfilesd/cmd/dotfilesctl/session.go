package main

import (
	"github.com/spf13/cobra"

	"dotfilesd/internal/pkg/cli"
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "session management",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "create",
		Short: "create a new session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunCreateSession(clients)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "finalize <session-id>",
		Short: "finalize an active session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunFinalizeSession(clients, args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "list active sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunListSessions(clients)
		},
	})
	return cmd
}
