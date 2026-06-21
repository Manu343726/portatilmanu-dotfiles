package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDef struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema toolSchema `json:"inputSchema"`
}

type toolSchema struct {
	Type       string                `json:"type"`
	Properties map[string]propSchema `json:"properties"`
	Required   []string              `json:"required,omitempty"`
}

type propSchema struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

var mcpTools = []toolDef{
	{
		Name:        "system_ping",
		Description: "Check daemon health",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"session_id": {Type: "string", Description: "optional session ID for grouping"},
		}},
	},
	{
		Name:        "system_info",
		Description: "Detailed system information",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"session_id": {Type: "string", Description: "optional session ID for grouping"},
		}},
	},
	{
		Name:        "system_sudo",
		Description: "Show available sudo methods",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"session_id": {Type: "string", Description: "optional session ID for grouping"},
		}},
	},
	{
		Name:        "dotfiles_status",
		Description: "Show dotfiles repo status",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"session_id": {Type: "string", Description: "optional session ID for grouping"},
		}},
	},
	{
		Name:        "dotfiles_git",
		Description: "Git operations on the dotfiles repo",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"action":     {Type: "string", Enum: []string{"status", "diff", "add", "commit", "push", "log"}},
			"message":    {Type: "string"},
			"paths":      {Type: "string"},
			"session_id": {Type: "string", Description: "optional session ID for grouping"},
		}, Required: []string{"action"}},
	},
	{
		Name:        "exec_run",
		Description: "Execute a shell command",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"command":    {Type: "string"},
			"sudo":       {Type: "boolean"},
			"password":   {Type: "string", Description: "sudo password (omit to try passwordless first)"},
			"session_id": {Type: "string", Description: "optional session ID for grouping"},
		}, Required: []string{"command"}},
	},
	{
		Name:        "config_reload",
		Description: "Reload dotfiles configs (tmux, i3, kitty)",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"target":     {Type: "string", Enum: []string{"tmux", "i3", "kitty", "all"}},
			"session_id": {Type: "string", Description: "optional session ID for grouping"},
		}},
	},
	{
		Name:        "config_reconfigure",
		Description: "Change daemon runtime configuration (e.g. log level)",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"log_level":  {Type: "string", Description: "new log level", Enum: []string{"trace", "debug", "info", "warn", "error"}},
			"session_id": {Type: "string", Description: "optional session ID for grouping"},
		}, Required: []string{"log_level"}},
	},
	{
		Name:        "config_restart",
		Description: "Gracefully restart the dotfilesd daemon",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"session_id": {Type: "string", Description: "optional session ID for grouping"},
		}},
	},
}

var stdoutMu sync.Mutex

// clientCaps tracks which MCP protocol capabilities the connected client declared
// during initialization. Used to determine whether standard features like
// elicitation are available before attempting to use them.
var clientCaps struct {
	hasElicitation bool
}

func writeJSONLine(w io.Writer, v any) {
	data, _ := json.Marshal(v)
	stdoutMu.Lock()
	w.Write(data)
	w.Write([]byte("\n"))
	stdoutMu.Unlock()
}

func RunMCP(clients *Clients) {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	bridge := NewMCPBridge(os.Stdout)
	mcpBridge = bridge

	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			slog.Error("read stdin", "error", err)
			continue
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}

		raw := json.RawMessage(line)

		// Check if this is a response to a server-initiated request (has ID, no method).
		var msgType struct {
			ID     json.RawMessage `json:"id,omitempty"`
			Method string          `json:"method,omitempty"`
		}
		if err := json.Unmarshal(raw, &msgType); err != nil {
			slog.Error("parse msg type", "error", err)
			continue
		}

		if msgType.ID != nil && msgType.Method == "" {
			var idStr string
			json.Unmarshal(msgType.ID, &idStr)
			if idStr != "" && bridge.HandleResponse(idStr, raw) {
				continue
			}
		}

		// Parse as a request (must have method + id).
		var req mcpRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			slog.Error("parse request", "error", err)
			continue
		}
		if req.ID == nil || len(req.ID) == 0 {
			continue
		}

		// Tool calls run in a background goroutine so the main loop keeps
		// reading stdin. This is critical for the MCP bridge: when a tool
		// call triggers feedback, the bridge sends a request to the client
		// via stdout and blocks waiting for a response on stdin. The main
		// goroutine reads that response and routes it to the bridge.
		if req.Method == "tools/call" {
			go func() {
				resp := dispatchMCP(clients, req)
				if resp != nil {
					writeJSONLine(os.Stdout, resp)
				}
			}()
		} else {
			resp := dispatchMCP(clients, req)
			if resp != nil {
				writeJSONLine(os.Stdout, resp)
			}
		}
	}
}

func dispatchMCP(clients *Clients, req mcpRequest) *mcpResponse {
	switch req.Method {
	case "initialize":
		// Capture client capabilities from the initialize request.
		var initParams struct {
			Capabilities *struct {
				Elicitation json.RawMessage `json:"elicitation"`
			} `json:"capabilities"`
		}
		if err := json.Unmarshal(req.Params, &initParams); err == nil {
			clientCaps.hasElicitation = initParams.Capabilities != nil && initParams.Capabilities.Elicitation != nil
			if clientCaps.hasElicitation {
				slog.Debug("client supports elicitation")
			}
		}
		return mcpResp(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]bool{"listChanged": false},
			},
			"serverInfo": map[string]string{
				"name":    "dotfilesctl",
				"version": "0.1.0",
			},
		})

	case "tools/list":
		return mcpResp(req.ID, map[string]any{"tools": mcpTools})

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return mcpErr(req.ID, -32602, "invalid params")
		}
		return callTool(clients, req.ID, params.Name, params.Arguments)

	default:
		return mcpErr(req.ID, -32601, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func addSessionHeader[T any](req *connect.Request[T], args json.RawMessage, defaultSessionID string) {
	var p struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		if defaultSessionID != "" {
			req.Header().Set("Session-Id", defaultSessionID)
		}
		return
	}
	id := p.SessionID
	if id == "" {
		id = defaultSessionID
	}
	if id != "" {
		req.Header().Set("Session-Id", id)
	}
}

func callTool(clients *Clients, id json.RawMessage, name string, args json.RawMessage) *mcpResponse {
	// Connect lazily on first tool call so MCP mode starts without
	// requiring the daemon to be reachable at launch time.
	if err := clients.Connect(context.Background()); err != nil {
		return mcpErr(id, -32000, fmt.Sprintf("daemon connection failed: %v", err))
	}
	switch name {
	case "system_ping":
		req := connect.NewRequest(&dotfilesdv1.PingRequest{})
		addSessionHeader(req, args, clients.SessionID)
		resp, err := clients.Sys.Ping(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		s := resp.Msg
		text := fmt.Sprintf("dotfilesd v%s (pid %d, up %ds)", s.Version, s.Pid, s.UptimeSecs)
		return mcpToolResult(id, text)

	case "system_info":
		req := connect.NewRequest(&dotfilesdv1.SystemInfoRequest{})
		addSessionHeader(req, args, clients.SessionID)
		resp, err := clients.Sys.SystemInfo(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		s := resp.Msg
		text := fmt.Sprintf("os: %s\nkernel: %s\nshell: %s\ndesktop: %s\nmemory: %d MB total / %d MB avail\ncpu: %.2f load\n%s\n%s\n%s",
			s.Os, s.Kernel, s.Shell, s.Desktop,
			s.MemoryTotalKb/1024, s.MemoryAvailKb/1024,
			s.CpuLoad_1M,
			s.TmuxVersion, s.KittyVersion, s.I3Version)
		return mcpToolResult(id, text)

	case "system_sudo":
		req := connect.NewRequest(&dotfilesdv1.SudoMethodsRequest{})
		addSessionHeader(req, args, clients.SessionID)
		resp, err := clients.Sys.SudoMethods(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		text := fmt.Sprintf("current: %s\nhas sudo: %v\navailable: %s",
			resp.Msg.CurrentMethod, resp.Msg.HasElevation,
			strings.Join(resp.Msg.AvailableMethods, ", "))
		return mcpToolResult(id, text)

	case "dotfiles_status":
		req := connect.NewRequest(&dotfilesdv1.StatusRequest{})
		addSessionHeader(req, args, clients.SessionID)
		resp, err := clients.Dot.Status(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		s := resp.Msg
		text := fmt.Sprintf("branch: %s\nclean: %v\nlast: %s\nhost: %s\nuptime: %s",
			s.GitBranch, s.GitClean, s.LastCommit, s.Hostname, s.Uptime)
		return mcpToolResult(id, text)

	case "dotfiles_git":
		var p struct {
			Action  string `json:"action"`
			Message string `json:"message"`
			Paths   string `json:"paths"`
		}
		json.Unmarshal(args, &p)
		action := ParseGitAction(p.Action)
		if action == dotfilesdv1.GitAction_GIT_ACTION_UNSPECIFIED {
			return mcpErr(id, -32602, fmt.Sprintf("unknown action: %s", p.Action))
		}
		req := connect.NewRequest(&dotfilesdv1.GitRequest{Action: action, Message: p.Message, Paths: p.Paths})
		addSessionHeader(req, args, clients.SessionID)
		resp, err := clients.Dot.Git(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		if resp.Msg.ExitCode != 0 {
			return mcpResp(id, map[string]any{
				"content": []map[string]any{{"type": "text", "text": resp.Msg.Stderr}},
				"isError": true,
			})
		}
		return mcpToolResult(id, resp.Msg.Stdout)

	case "exec_run":
		var p struct {
			Command  string `json:"command"`
			Sudo     bool   `json:"sudo"`
			Password string `json:"password"`
		}
		json.Unmarshal(args, &p)

		if p.Sudo {
			sudoReq := connect.NewRequest(&dotfilesdv1.SudoExecRequest{
				Command: p.Command,
			})
			if p.Password != "" {
				sudoReq.Msg.Password = p.Password
			} else {
				sudoReq.Msg.PreferredMethod = dotfilesdv1.SudoMethod_SUDO_METHOD_NOPASS
			}
			addSessionHeader(sudoReq, args, clients.SessionID)
			resp, err := clients.Exec.SudoExec(context.Background(), sudoReq)
			if err != nil {
				return mcpErr(id, -32603, err.Error())
			}

			switch outcome := resp.Msg.Outcome.(type) {
			case *dotfilesdv1.SudoExecResponse_AuthChallenge:
				ch := outcome.AuthChallenge
				text := fmt.Sprintf("sudo requires authentication\nprompt: %s\navailable methods: %s\n\nTo retry, call exec_run with the sudo password set.",
					ch.Prompt, strings.Join(ch.Methods, ", "))
				return mcpResp(id, map[string]any{
					"content": []map[string]any{{"type": "text", "text": text}},
					"isError": true,
				})

			case *dotfilesdv1.SudoExecResponse_Result:
				r := outcome.Result
				text := r.Stdout
				if r.Stderr != "" {
					text += "\nstderr:\n" + r.Stderr
				}
				if r.AuthCancelled {
					text = "sudo authentication cancelled"
				}
				return mcpResp(id, map[string]any{
					"content": []map[string]any{{"type": "text", "text": text}},
					"isError": r.ExitCode != 0,
				})
			}
		}

		// Non-sudo path: use Exec RPC directly.
		req := connect.NewRequest(&dotfilesdv1.ExecRequest{
			Command: p.Command,
			Sudo:    false,
		})
		addSessionHeader(req, args, clients.SessionID)
		resp, err := clients.Exec.Exec(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		text := resp.Msg.Stdout
		if resp.Msg.Stderr != "" {
			text += "\nstderr:\n" + resp.Msg.Stderr
		}
		return mcpResp(id, map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": resp.Msg.ExitCode != 0,
		})

	case "config_reload":
		var p struct {
			Target string `json:"target"`
		}
		json.Unmarshal(args, &p)
		target := dotfilesdv1.ReloadTarget_RELOAD_TARGET_ALL
		if p.Target != "" {
			target = ParseReloadTarget(p.Target)
			if target == dotfilesdv1.ReloadTarget_RELOAD_TARGET_UNSPECIFIED {
				return mcpErr(id, -32602, fmt.Sprintf("unknown target: %s", p.Target))
			}
		}
		req := connect.NewRequest(&dotfilesdv1.ReloadRequest{Target: target})
		addSessionHeader(req, args, clients.SessionID)
		resp, err := clients.Cfg.Reload(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		var lines []string
		for _, r := range resp.Msg.Results {
			s := "ok"
			if !r.Success {
				s = "err"
			}
			lines = append(lines, fmt.Sprintf("%s: %s (%s)", r.Target, s, r.Message))
		}
		return mcpToolResult(id, linesJoin(lines, "\n"))

	case "config_reconfigure":
		var p struct {
			LogLevel string `json:"log_level"`
		}
		json.Unmarshal(args, &p)
		if p.LogLevel == "" {
			return mcpErr(id, -32602, "log_level is required")
		}
		logLevel := ParseLogLevel(p.LogLevel)
		if logLevel == dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED {
			return mcpErr(id, -32602, fmt.Sprintf("invalid log level: %s", p.LogLevel))
		}
		req := connect.NewRequest(&dotfilesdv1.ReconfigureRequest{LogLevel: logLevel})
		addSessionHeader(req, args, clients.SessionID)
		resp, err := clients.Cfg.Reconfigure(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		if !resp.Msg.Success {
			return mcpResp(id, map[string]any{
				"content": []map[string]any{{"type": "text", "text": resp.Msg.Message}},
				"isError": true,
			})
		}
		return mcpToolResult(id, resp.Msg.Message)

	case "config_restart":
		req := connect.NewRequest(&dotfilesdv1.RestartRequest{})
		addSessionHeader(req, args, clients.SessionID)
		resp, err := clients.Cfg.Restart(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		return mcpToolResult(id, resp.Msg.Message)

	default:
		return mcpErr(id, -32601, fmt.Sprintf("unknown tool: %s", name))
	}
}

func mcpToolResult(id json.RawMessage, text string) *mcpResponse {
	return mcpResp(id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

func mcpResp(id json.RawMessage, result any) *mcpResponse {
	data, _ := json.Marshal(result)
	return &mcpResponse{JSONRPC: "2.0", ID: id, Result: data}
}

func mcpErr(id json.RawMessage, code int, msg string) *mcpResponse {
	return &mcpResponse{JSONRPC: "2.0", ID: id, Error: &mcpError{Code: code, Message: msg}}
}

func linesJoin(elems []string, sep string) string {
	if len(elems) == 0 {
		return ""
	}
	n := len(sep) * (len(elems) - 1)
	for _, e := range elems {
		n += len(e)
	}
	b := make([]byte, n)
	i := 0
	for idx, e := range elems {
		if idx > 0 {
			i += copy(b[i:], sep)
		}
		i += copy(b[i:], e)
	}
	return string(b)
}
