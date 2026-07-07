package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"dotfilesd/internal/pkg/cli/color"
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
	fmt.Printf("%s v%s %s%s\n",
		color.Greenf("dotfilesd"),
		color.Styled(s.Version, color.Bold),
		color.Dimf("(pid %d, up %ds)", s.Pid, s.UptimeSecs),
		color.Reset())
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
	fmt.Printf("%s %s\n", color.Bluef("OS:"), s.Os)
	fmt.Printf("%s %s\n", color.Bluef("Kernel:"), s.Kernel)
	fmt.Printf("%s %s\n", color.Bluef("Shell:"), s.Shell)
	fmt.Printf("%s %s\n", color.Bluef("Desktop:"), s.Desktop)
	fmt.Printf("%s %s\n", color.Bluef("Host:"), s.Hostname)
	fmt.Printf("%s %s\n", color.Bluef("Uptime:"), s.Uptime)
	fmt.Printf("%s %s\n", color.Bluef("Tools:"), strings.Join(s.AvailableTools, ", "))
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
	fmt.Printf("%s %s\n", color.Yellowf("current:"), resp.Msg.CurrentMethod)
	fmt.Printf("%s %v\n", color.Yellowf("has sudo:"), resp.Msg.HasElevation)
	fmt.Printf("%s %s\n", color.Yellowf("available:"), strings.Join(resp.Msg.AvailableMethods, ", "))
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
		ShowIdle: params.ShowIdle,
	})

	// Populate filter sub-message if any filter params are set.
	if len(params.Types) > 0 || params.Status != "" || params.Label != "" || len(params.Attrs) > 0 || params.TimeWindow > 0 {
		f := &dotfilesdv1.DiagnosticsFilter{}
		for _, t := range params.Types {
			f.IncludeTypes = append(f.IncludeTypes, nodeTypeFromShort(t))
		}
		if params.Status != "" {
			f.StatusFilter = nodeStatusFromString(params.Status)
		}
		if params.Label != "" {
			f.LabelRegex = params.Label
		}
		if len(params.Attrs) > 0 {
			f.AttrFilters = make(map[string]string, len(params.Attrs))
			for _, pair := range params.Attrs {
				k, v, ok := strings.Cut(pair, "=")
				if !ok {
					return fmt.Errorf("invalid attr filter %q: expected key=value", pair)
				}
				f.AttrFilters[k] = v
			}
		}
		if params.TimeWindow > 0 {
			f.TimeWindow = durationpb.New(params.TimeWindow)
		}
		req.Msg.Filter = f
	}

	resp, err := clients.DiagQuery.QueryTree(context.Background(), req)
	if err != nil {
		slog.Error("diagnostics failed", "error", err)
		return fmt.Errorf("diagnostics failed: %w", err)
	}

	selfClientID := "client:" + clients.ClientID
	printTree(resp.Msg.Root, "", true, params.Fields, selfClientID)
	return nil
}

func printTree(n *dotfilesdv1.DiagNode, prefix string, isLast bool, fields []string, selfClientID string) {
	branch := "├── "
	if isLast {
		branch = "└── "
	}

	typeTag := nodeTypeName(n.Type)

	// Node header line: [type] label (status)
	label := n.Label
	statusLabel := nodeStatusName(n.Status)

	// Mark this client as "yourself" if it matches the current CLI client ID.
	selfMarker := ""
	if n.Type == dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_CLIENT && selfClientID != "" && n.Label == stripClientPrefix(selfClientID) {
		selfMarker = " " + color.Greenf("← you")
		selfMarker = " " + color.Greenf("← you")
	}

	// Colour the branch lines dim.
	coloredBranch := color.Styled(branch, color.Dim)

	// Colour the type tag.
	coloredType := color.Styled("["+typeTag+"]", color.TypeColor(typeTag))

	// Colour the status label.
	coloredStatus := ""
	if statusLabel != "" {
		coloredStatus = color.Styled("("+statusLabel+")", color.StatusColor(statusLabel))
	}

	header := fmt.Sprintf("%s%s %s %s %s%s", color.Styled(prefix, color.Dim), coloredBranch, coloredType, label, coloredStatus, selfMarker)

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
				fmt.Printf("%s%s%s: %s\n", color.Styled(childPrefix, color.Dim), color.Styled("│  ", color.Dim), f, v)
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
		printTree(child, childPrefix, i == len(n.Children)-1, fields, selfClientID)
	}
}

// conciseSummary builds a one-line suffix for a node when no --fields are given.
func conciseSummary(n *dotfilesdv1.DiagNode, typeTag string) string {
	a := n.Attrs
	switch n.Type {
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_DAEMON, dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_PLUGIN:
		return ""

	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_CLIENT:
		ct := nullget(a, "client_type", "?")
		switch ct {
		case "mcp":
			return color.Dimf("mcp:%s", nullget(a, "agent_id", "?"))
		default:
			cmd := nullget(a, "command", "")
			if cmd != "" {
				return color.Yellowf("`%s`", cmd)
			}
			if age := nullget(a, "started_ago", ""); age != "" {
				return color.Dimf("(up %s)", age)
			}
			return color.Dimf("(no command)")
		}

	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_SESSION:
		return ""

	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_EXECUTOR:
		return ""

	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_BG_TASK:
		return ""

	default:
		// For "exec" (old-style string type) — check via attrs or fallback.
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

// stripClientPrefix strips the "client:" prefix from a resource ID.
func stripClientPrefix(id string) string {
	if len(id) > 7 && id[:7] == "client:" {
		return id[7:]
	}
	return id
}

// nodeTypeName returns a human-readable label for a node type.
func nodeTypeName(t dotfilesdv1.DiagNodeType) string {
	switch t {
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_ROOT:
		return "runtime"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_DAEMON:
		return "daemon"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_PLUGIN:
		return "plugin"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_SESSION:
		return "session"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_CLIENT:
		return "client"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_EXECUTOR:
		return "plugin"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_SHELL:
		return "shell"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_BG_TASK:
		return "bgtask"
	default:
		return "unknown"
	}
}

func nodeTypeFromShort(s string) dotfilesdv1.DiagNodeType {
	switch s {
	case "root":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_ROOT
	case "daemon":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_DAEMON
	case "plugin":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_PLUGIN
	case "session":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_SESSION
	case "client":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_CLIENT
	case "executor", "exec", "plugin-rpc":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_EXECUTOR
	case "shell":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_SHELL
	case "bg_task":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_BG_TASK
	default:
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_UNSPECIFIED
	}
}

func nodeStatusName(s dotfilesdv1.DiagNodeStatus) string {
	switch s {
	case dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_ACTIVE:
		return "active"
	case dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_PENDING:
		return "pending"
	case dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_FINISHED:
		return "finished"
	case dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_CRASHED:
		return "crashed"
	default:
		return ""
	}
}

func nodeStatusFromString(s string) dotfilesdv1.DiagNodeStatus {
	switch s {
	case "active":
		return dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_ACTIVE
	case "pending":
		return dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_PENDING
	case "finished":
		return dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_FINISHED
	case "crashed":
		return dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_CRASHED
	default:
		return dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_UNSPECIFIED
	}
}
