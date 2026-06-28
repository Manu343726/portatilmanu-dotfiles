package main

import (
	"time"

	"github.com/spf13/cobra"

	"dotfilesd/internal/pkg/cli"
)

func newSystemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "daemon health and system information",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "ping",
		Short: "check daemon is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunPing(clients, sessionID)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "detailed system information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunInfo(clients, sessionID)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "sudo",
		Short: "show available sudo methods",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunSudoMethods(clients, sessionID)
		},
	})

	// Diag sub-command with optional filter flags.
	var (
		diagTimeWindow time.Duration
		diagShowIdle   bool
		diagTypes      []string
		diagStatus     string
		diagLabel      string
		diagAttrs      []string
	)
	diagCmd := &cobra.Command{
		Use:   "diag",
		Short: "diagnostic tree of daemon state",
		Long: `Show the full diagnostic state tree of the daemon.

By default only active resources are shown. Use --time-window or
--show-idle to include finished/crashed nodes, and --type, --status,
--label, --attr to filter the tree.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunDiagnostics(clients, sessionID, cli.DiagParams{
				TimeWindow: diagTimeWindow,
				ShowIdle:   diagShowIdle,
				Types:      diagTypes,
				Status:     diagStatus,
				Label:      diagLabel,
				Attrs:      diagAttrs,
			})
		},
	}
	diagCmd.Flags().DurationVarP(&diagTimeWindow, "time-window", "t", 0,
		"Include finished nodes within this duration (e.g. 5m, 1h). 0 = active-only")
	diagCmd.Flags().BoolVarP(&diagShowIdle, "show-idle", "i", false,
		"Show all finished/crashed nodes (alias for --time-window inf)")
	diagCmd.Flags().StringArrayVarP(&diagTypes, "type", "T", nil,
		"Filter by resource type (e.g. --type plugin --type client; can repeat)")
	diagCmd.Flags().StringVarP(&diagStatus, "status", "s", "",
		"Filter by status (active, finished, crashed)")
	diagCmd.Flags().StringVarP(&diagLabel, "label", "L", "",
		"Filter by label regex")
	diagCmd.Flags().StringArrayVarP(&diagAttrs, "attr", "a", nil,
		"Filter by attr key=value (e.g. --attr client_type=cli; can repeat)")
	cmd.AddCommand(diagCmd)

	return cmd
}
