package main

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"github.com/spf13/cobra"
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
			resp, err := sysClient.Ping(context.Background(), connect.NewRequest(&dotfilesdv1.PingRequest{}))
			if err != nil {
				return fmt.Errorf("ping failed: %w", err)
			}
			s := resp.Msg
			fmt.Printf("dotfilesd v%s (pid %d, up %ds)\n", s.Version, s.Pid, s.UptimeSecs)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "detailed system information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := sysClient.SystemInfo(context.Background(), connect.NewRequest(&dotfilesdv1.SystemInfoRequest{}))
			if err != nil {
				return fmt.Errorf("info failed: %w", err)
			}
			s := resp.Msg
			fmt.Printf("OS:      %s\n", s.Os)
			fmt.Printf("Kernel:  %s\n", s.Kernel)
			fmt.Printf("Shell:   %s\n", s.Shell)
			fmt.Printf("Desktop: %s\n", s.Desktop)
			fmt.Printf("Memory:  %d MB total / %d MB avail\n", s.MemoryTotalKb/1024, s.MemoryAvailKb/1024)
			fmt.Printf("CPU:     %.2f load\n", s.CpuLoad_1M)
			fmt.Printf("Tmux:    %s\n", s.TmuxVersion)
			fmt.Printf("Kitty:   %s\n", s.KittyVersion)
			fmt.Printf("I3:      %s\n", s.I3Version)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "sudo",
		Short: "show available sudo methods",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := sysClient.SudoMethods(context.Background(), connect.NewRequest(&dotfilesdv1.SudoMethodsRequest{}))
			if err != nil {
				return fmt.Errorf("sudo methods failed: %w", err)
			}
			fmt.Printf("current:  %s\n", resp.Msg.CurrentMethod)
			fmt.Printf("has sudo: %v\n", resp.Msg.HasElevation)
			fmt.Printf("available: %s\n", strings.Join(resp.Msg.AvailableMethods, ", "))
			return nil
		},
	})
	return cmd
}
