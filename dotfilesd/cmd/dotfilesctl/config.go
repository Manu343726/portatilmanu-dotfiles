package main

import (
	"dotfilesd/internal/pkg/cli"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "dotfiles configuration reload, daemon reconfiguration, restart",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "reload [target]",
		Short: "reload configs (tmux, i3, kitty, all)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetStr := ""
			if len(args) > 0 {
				targetStr = args[0]
			}
			return cli.RunReload(clients, targetStr)
		},
	})
	var reconfigureCmd = &cobra.Command{
		Use:   "reconfigure --log-level <level>",
		Short: "change daemon runtime configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			levelStr, _ := cmd.Flags().GetString("log-level")
			return cli.RunReconfigure(clients, levelStr)
		},
	}
	reconfigureCmd.Flags().String("log-level", "", "new log level (trace, debug, info, warn, error)")
	cmd.AddCommand(reconfigureCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "restart",
		Short: "gracefully restart the daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunRestart(clients)
		},
	})
	return cmd
}
