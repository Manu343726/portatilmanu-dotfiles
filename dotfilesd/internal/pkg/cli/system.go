package cli

import (
	"context"
	"fmt"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

func RunPing(clients *Clients, sessionID string) error {
	req := connect.NewRequest(&dotfilesdv1.PingRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Sys.Ping(context.Background(), req)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	s := resp.Msg
	fmt.Printf("dotfilesd v%s (pid %d, up %ds)\n", s.Version, s.Pid, s.UptimeSecs)
	return nil
}

func RunInfo(clients *Clients, sessionID string) error {
	req := connect.NewRequest(&dotfilesdv1.SystemInfoRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Sys.SystemInfo(context.Background(), req)
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
}

func RunSudoMethods(clients *Clients, sessionID string) error {
	req := connect.NewRequest(&dotfilesdv1.SudoMethodsRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Sys.SudoMethods(context.Background(), req)
	if err != nil {
		return fmt.Errorf("sudo methods failed: %w", err)
	}
	fmt.Printf("current:  %s\n", resp.Msg.CurrentMethod)
	fmt.Printf("has sudo: %v\n", resp.Msg.HasElevation)
	fmt.Printf("available: %s\n", strings.Join(resp.Msg.AvailableMethods, ", "))
	return nil
}
