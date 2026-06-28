package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
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

// DiagParams holds optional filter flags for the diagnostics query.
type DiagParams struct {
	TimeWindow time.Duration
	ShowIdle   bool
	Types      []string
	Status     string
	Label      string
	Attrs      []string // "key=value" pairs
	Fields     []string // attrs to show; empty = concise one-line summary per type
}

// RunDiagnostics queries the daemon for full diagnostic state and prints a tree.
func RunDiagnostics(clients *Clients, sessionID string, params DiagParams) error {
	slog.Debug("diagnostics requested", "session_id", sessionID, "params", params)

	req := connect.NewRequest(&dotfilesdv1.QueryTreeRequest{
		ShowIdle:     params.ShowIdle,
		IncludeTypes: params.Types,
		StatusFilter: params.Status,
		LabelRegex:   params.Label,
	})

	// Parse attrs.
	if len(params.Attrs) > 0 {
		req.Msg.AttrFilters = make(map[string]string, len(params.Attrs))
		for _, pair := range params.Attrs {
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				return fmt.Errorf("invalid attr filter %q: expected key=value", pair)
			}
			req.Msg.AttrFilters[k] = v
		}
	}

	// Convert time window to proto Duration.
	if params.TimeWindow > 0 {
		req.Msg.TimeWindow = durationpb.New(params.TimeWindow)
	}

	resp, err := clients.DiagQuery.QueryTree(context.Background(), req)
	if err != nil {
		slog.Error("diagnostics failed", "error", err)
		return fmt.Errorf("diagnostics failed: %w", err)
	}

	printTree(resp.Msg.Root, "", true, params.Fields)
	return nil
}

func printTree(n *dotfilesdv1.DiagNode, prefix string, isLast bool, fields []string) {
	branch := "├── "
	if isLast {
		branch = "└── "
	}

	typeTag := typeLabel(n.Type)

	// Node header line: [type] label (status)
	label := n.Label
	if n.Status != "" {
		label = fmt.Sprintf("%s (%s)", label, n.Status)
	}
	header := fmt.Sprintf("%s%s [%s] %s", prefix, branch, typeTag, label)

	// Build per-node summary when no --fields are given.
	if len(fields) == 0 {
		summary := conciseSummary(n, typeTag)
		if summary != "" {
			header += " " + summary
		}
		fmt.Println(header)
	} else {
		fmt.Println(header)
		childPrefix := prefix
		if isLast {
			childPrefix += "   "
		} else {
			childPrefix += "│  "
		}
		// Show only requested fields, in order given.
		for _, f := range fields {
			if v, ok := n.Attrs[f]; ok {
				fmt.Printf("%s%s: %s\n", childPrefix, f, v)
			}
		}
	}

	// Children.
	childPrefix := prefix
	if isLast {
		childPrefix += "   "
	} else {
		childPrefix += "│  "
	}
	for i, child := range n.Children {
		printTree(child, childPrefix, i == len(n.Children)-1, fields)
	}
}

// conciseSummary builds a one-line suffix for a node when no --fields are given.
func conciseSummary(n *dotfilesdv1.DiagNode, typeTag string) string {
	a := n.Attrs
	switch n.Type {
	case "daemon", "plugin":
		// Version/pid/port are already in the label, nothing extra needed.
		return ""

	case "client":
		ct := nullget(a, "client_type", "?")
		switch ct {
		case "mcp":
			return fmt.Sprintf("mcp:%s", nullget(a, "agent_id", "?"))
		default:
			cmd := nullget(a, "command", "")
			if cmd != "" {
				return fmt.Sprintf("`%s`", cmd)
			}
			// Fallback: show age instead of a long unreadable ID.
			if age := nullget(a, "started_ago", ""); age != "" {
				return fmt.Sprintf("(up %s)", age)
			}
			return "(no command)"
		}

	case "session":
		return ""

	case "exec":
		if n.Status == "finished" {
			dur := nullget(a, "duration", "")
			s := fmt.Sprintf("exit=%s", nullget(a, "exit_code", "?"))
			if dur != "" {
				s += " dur:" + dur
			}
			return "[" + s + "]"
		}
		return "(running)"

	case "executor":
		return ""

	case "bg_task":
		return ""

	default:
		return ""
	}
}

// nullget returns the value for key in m, or fallback if missing/empty.
func nullget(m map[string]string, key, fallback string) string {
	if m == nil {
		return fallback
	}
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return fallback
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
	case "executor", "plugin-rpc":
		return "plugin"
	case "shell":
		return "shell"
	case "bg_task":
		return "bgtask"
	default:
		return t
	}
}
