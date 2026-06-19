package main

import (
	"context"
	"fmt"
	"os"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
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
			target := dotfilesdv1.ReloadTarget_RELOAD_TARGET_ALL
			if len(args) > 0 {
				target = parseReloadTarget(args[0])
				if target == dotfilesdv1.ReloadTarget_RELOAD_TARGET_UNSPECIFIED {
					return fmt.Errorf("unknown target: %s (valid: tmux, i3, kitty, all)", args[0])
				}
			}
			resp, err := cfgClient.Reload(context.Background(), connect.NewRequest(&dotfilesdv1.ReloadRequest{Target: target}))
			if err != nil {
				return fmt.Errorf("reload failed: %w", err)
			}
			for _, r := range resp.Msg.Results {
				status := "ok"
				if !r.Success {
					status = "error"
				}
				fmt.Printf("%-6s %s: %s\n", status, r.Target, r.Message)
			}
			return nil
		},
	})
	var reconfigureCmd = &cobra.Command{
		Use:   "reconfigure --log-level <level>",
		Short: "change daemon runtime configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			levelStr, _ := cmd.Flags().GetString("log-level")
			if levelStr == "" {
				return fmt.Errorf("--log-level is required (trace, debug, info, warn, error)")
			}
			logLevel := parseLogLevel(levelStr)
			if logLevel == dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED {
				return fmt.Errorf("invalid log level: %s (valid: trace, debug, info, warn, error)", levelStr)
			}
			resp, err := cfgClient.Reconfigure(context.Background(), connect.NewRequest(&dotfilesdv1.ReconfigureRequest{
				LogLevel: logLevel,
			}))
			if err != nil {
				return fmt.Errorf("reconfigure failed: %w", err)
			}
			fmt.Println(resp.Msg.Message)
			if !resp.Msg.Success {
				os.Exit(1)
			}
			return nil
		},
	}
	reconfigureCmd.Flags().String("log-level", "", "new log level (trace, debug, info, warn, error)")
	cmd.AddCommand(reconfigureCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "restart",
		Short: "gracefully restart the daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := cfgClient.Restart(context.Background(), connect.NewRequest(&dotfilesdv1.RestartRequest{}))
			if err != nil {
				return fmt.Errorf("restart failed: %w", err)
			}
			fmt.Println(resp.Msg.Message)
			return nil
		},
	})
	return cmd
}
