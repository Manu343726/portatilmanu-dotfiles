package cli

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
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
	req := connect.NewRequest(&dotfilesdv1.QueryTreeRequest{})
	resp, err := clients.DiagQuery.QueryTree(context.Background(), req)
	if err != nil {
		slog.Error("diagnostics failed", "error", err)
		return fmt.Errorf("diagnostics failed: %w", err)
	}

	printTree(resp.Msg.Root, "", true)
	return nil
}

func printTree(n *dotfilesdv1.DiagNode, prefix string, isLast bool) {
	// Build the line prefix.
	branch := "├── "
	if isLast {
		branch = "└── "
	}

	// Build a compact type tag.
	typeTag := typeLabel(n.Type)

	label := n.Label
	if n.Status != "" {
		label = fmt.Sprintf("%s (%s)", label, n.Status)
	}
	fmt.Printf("%s%s [%s] %s\n", prefix, branch, typeTag, label)

	// Print attributes indented, with stable key order.
	childPrefix := prefix
	if isLast {
		childPrefix += "   "
	} else {
		childPrefix += "│  "
	}
	keys := make([]string, 0, len(n.Attrs))
	for k := range n.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s%s: %s\n", childPrefix, k, n.Attrs[k])
	}

	// Print children.
	for i, child := range n.Children {
		printTree(child, childPrefix, i == len(n.Children)-1)
	}
}

// typeLabel returns a human-readable label for a node type.
func typeLabel(t string) string {
	switch t {
	case "root":
		return "runtime"
	case "daemon":
		return "daemon"
	case "plugin":
		return "plugin"
	case "session":
		return "session"
	case "client":
		return "client"
	case "executor":
		return "executor"
	case "shell":
		return "shell"
	case "bg_task":
		return "bgtask"
	default:
		return t
	}
}
