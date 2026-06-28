package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

func RunPing(clients *Clients, sessionID string) error {
	slog.Debug("ping requested", "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.PingRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Sys.Ping(context.Background(), req)
	if err != nil {
		slog.Error("ping failed", "error", err)
		return fmt.Errorf("ping failed: %w", err)
	}
	s := resp.Msg
	slog.Debug("ping response", "version", s.Version, "pid", s.Pid, "uptime_secs", s.UptimeSecs)
	fmt.Printf("dotfilesd v%s (pid %d, up %ds)\n", s.Version, s.Pid, s.UptimeSecs)
	return nil
}

func RunInfo(clients *Clients, sessionID string) error {
	slog.Debug("runtime info requested", "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.RuntimeInfoRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Sys.RuntimeInfo(context.Background(), req)
	if err != nil {
		slog.Error("runtime info failed", "error", err)
		return fmt.Errorf("runtime info failed: %w", err)
	}
	s := resp.Msg
	slog.Debug("runtime info response", "os", s.Os, "kernel", s.Kernel)
	fmt.Printf("OS:      %s\n", s.Os)
	fmt.Printf("Kernel:  %s\n", s.Kernel)
	fmt.Printf("Shell:   %s\n", s.Shell)
	fmt.Printf("Desktop: %s\n", s.Desktop)
	fmt.Printf("Host:    %s\n", s.Hostname)
	fmt.Printf("Uptime:  %s\n", s.Uptime)
	fmt.Printf("Tools:   %s\n", strings.Join(s.AvailableTools, ", "))
	return nil
}

func RunSudoMethods(clients *Clients, sessionID string) error {
	slog.Debug("sudo methods requested", "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.SudoMethodsRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Sys.SudoMethods(context.Background(), req)
	if err != nil {
		slog.Error("sudo methods failed", "error", err)
		return fmt.Errorf("sudo methods failed: %w", err)
	}
	slog.Debug("sudo methods", "current", resp.Msg.CurrentMethod, "has_elevation", resp.Msg.HasElevation)
	fmt.Printf("current:  %s\n", resp.Msg.CurrentMethod)
	fmt.Printf("has sudo: %v\n", resp.Msg.HasElevation)
	fmt.Printf("available: %s\n", strings.Join(resp.Msg.AvailableMethods, ", "))
	return nil
}

// RunDiagnostics queries the daemon for full diagnostic state and prints a tree.
func RunDiagnostics(clients *Clients, sessionID string) error {
	slog.Debug("diagnostics requested", "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.DiagnosticsRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Sys.Diagnostics(context.Background(), req)
	if err != nil {
		slog.Error("diagnostics failed", "error", err)
		return fmt.Errorf("diagnostics failed: %w", err)
	}
	d := resp.Msg

	fmt.Printf("Daemon v%s (pid %d, up %ds)\n", d.Version, d.Pid, d.UptimeSecs)
	fmt.Println()

	// Sessions tree.
	fmt.Printf("═══ Sessions (%d)\n", len(d.Sessions))
	for _, s := range d.Sessions {
		status := "active"
		if s.Finalized {
			status = "finalized"
		}
		fmt.Printf("  ├─ %s (%s)\n", s.Id, status)
		if s.CallbackUrl != "" {
			fmt.Printf("  │  callback: %s\n", s.CallbackUrl)
		}
		fmt.Printf("  │  created: %s\n", s.CreatedAt)
	}
	fmt.Println()

	// Plugins tree.
	fmt.Printf("═══ Plugins (%d)\n", len(d.Plugins))
	for _, p := range d.Plugins {
		fmt.Printf("  ├─ %s v%s (%s)\n", p.DisplayName, p.Version, p.Url)
		for _, svc := range p.Services {
			fmt.Printf("  │  service: %s\n", svc)
		}
	}
	fmt.Println()

	// Active executor streams.
	if len(d.Executors) > 0 {
		fmt.Printf("═══ Active executor streams (%d)\n", len(d.Executors))
		for _, e := range d.Executors {
			fmt.Printf("  ├─ %s → %s\n", e.ClientId, e.PluginName)
		}
		fmt.Println()
	}

	// Background tasks.
	if len(d.BackgroundTasks) > 0 {
		fmt.Printf("═══ Background tasks (%d)\n", len(d.BackgroundTasks))
		for _, t := range d.BackgroundTasks {
			fmt.Printf("  ├─ [%s] %s\n", t.Id, t.Command)
		}
		fmt.Println()
	}

	return nil
}
