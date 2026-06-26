package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

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
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema toolSchema      `json:"inputSchema"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
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
			"sudo":       {Type: "boolean", Description: "run with sudo (prompts for password securely via feedback or MCP Apps webview)"},
			"session_id": {Type: "string", Description: "optional session ID for grouping"},
		}, Required: []string{"command"}},
		Meta: json.RawMessage(`{"ui":{"resourceUri":"ui://dotfilesd/sudo-prompt"}}`),
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
	{
		Name:        "script_run",
		Description: "Run a multi-step script with shell commands and feedback directives (@confirm, @input, @choose). Scripts execute in a persistent session shell so variables set by @input/@choose are available to subsequent commands.",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"script":      {Type: "string", Description: "inline script content"},
			"script_path": {Type: "string", Description: "path to script file on the daemon host"},
			"session_id":  {Type: "string", Description: "optional session ID for grouping"},
		}},
	},
	{
		Name:        "script_list",
		Description: "List all registered scripts available on the daemon, organized hierarchically by directory.",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"session_id": {Type: "string", Description: "optional session ID for grouping"},
		}},
	},

	{
		Name:        "_sudo_submit_password",
		Description: "Internal: Submit sudo password from the MCP Apps webview. Only callable from within the UI view (visibility: app).",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"request_id": {Type: "string", Description: "optional request ID returned by exec_run(sudo=true)"},
			"password":   {Type: "string", Description: "sudo password"},
		}, Required: []string{"password"}},
		Meta: json.RawMessage(`{"ui":{"visibility":["app"]}}`),
	},
}

// pendingSudoRequest represents a sudo execution waiting for the user to
// enter their password via the MCP Apps webview. The exec_run goroutine
// blocks on passwordCh until _sudo_submit_password sends the password.
type pendingSudoRequest struct {
	command    string
	createdAt  time.Time
	passwordCh chan string // buffered(1) channel: _sudo_submit_password sends here, exec_run receives
}

// pendingRequests holds all active sudo requests awaiting password submission,
// keyed by request ID (hex string). Entries are cleaned up after 10 minutes.
var pendingRequests sync.Map

var stdoutMu sync.Mutex

// mcpClientCaps tracks which MCP protocol capabilities the connected client
// declared during initialization. Used to determine whether standard features
// like elicitation and MCP Apps (_meta/ui) are available.
type mcpClientCaps struct {
	hasElicitation bool
	clientName     string
	clientVersion  string
	hasMcpApps     bool
}

var clientCaps mcpClientCaps

func init() {
	// Periodically sweep expired pending sudo requests.
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			pendingRequests.Range(func(key, value any) bool {
				req := value.(*pendingSudoRequest)
				if time.Since(req.createdAt) > 10*time.Minute {
					pendingRequests.Delete(key)
					slog.Debug("cleaned up expired sudo request", "request_id", key)
				}
				return true
			})
		}
	}()
}

func writeJSONLine(w io.Writer, v any) {
	data, _ := json.Marshal(v)
	stdoutMu.Lock()
	w.Write(data)
	w.Write([]byte("\n"))
	stdoutMu.Unlock()
}

func RunMCP(clients *Clients) {
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

		// Tool calls that can trigger elicitation feedback (exec_run with sudo,
		// script_run with @input/@confirm) need to run in a background goroutine
		// so the main loop keeps reading stdin to route the elicitation response
		// back to the bridge. When MCP Apps is available, exec_run(sudo=true)
		// blocks in the goroutine waiting for the password from the webview via
		// a channel; the main goroutine handles _sudo_submit_password to unblock
		// it. Other tool calls run synchronously for simplicity.
		if req.Method == "tools/call" {
			// Peek at the tool name to decide execution strategy.
			var toolInfo struct {
				Name string `json:"name"`
			}
			json.Unmarshal(req.Params, &toolInfo)

			if toolInfo.Name == "exec_run" || toolInfo.Name == "script_run" {
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
		// Capture client identity and capabilities from the initialize request.
		var initParams struct {
			ClientInfo *struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"clientInfo"`
			Capabilities *struct {
				Elicitation json.RawMessage `json:"elicitation"`
				Extensions  *struct {
					IoMcpUi json.RawMessage `json:"io.modelcontextprotocol/ui"`
				} `json:"extensions"`
				Experimental *struct {
					Meta json.RawMessage `json:"_meta"`
				} `json:"_experimental"`
			} `json:"capabilities"`
		}
		if err := json.Unmarshal(req.Params, &initParams); err == nil {
			if initParams.ClientInfo != nil {
				clientCaps.clientName = initParams.ClientInfo.Name
				clientCaps.clientVersion = initParams.ClientInfo.Version
				slog.Debug("MCP client", "name", clientCaps.clientName, "version", clientCaps.clientVersion)
			}
			clientCaps.hasElicitation = initParams.Capabilities != nil && initParams.Capabilities.Elicitation != nil
			if clientCaps.hasElicitation {
				slog.Debug("client supports elicitation")
			}
			// Detect MCP Apps support: check both the spec-compliant
			// extensions.io.modelcontextprotocol/ui field and the legacy
			// _experimental._meta field (pre-spec VS Code).
			if initParams.Capabilities != nil {
				if initParams.Capabilities.Extensions != nil && initParams.Capabilities.Extensions.IoMcpUi != nil {
					clientCaps.hasMcpApps = true
					slog.Debug("client supports MCP Apps (extensions.io.modelcontextprotocol/ui)")
				}
				if initParams.Capabilities.Experimental != nil && initParams.Capabilities.Experimental.Meta != nil {
					clientCaps.hasMcpApps = true
					slog.Debug("client supports MCP Apps (_experimental._meta)")
				}
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

	case "resources/list":
		return mcpResp(req.ID, map[string]any{
			"resources": []map[string]any{
				{
					"uri":         "ui://dotfilesd/sudo-prompt",
					"name":        "Sudo Password Prompt",
					"description": "Password input form for sudo command authentication. The form receives the command and request_id from the tool call result.",
					"mimeType":    "text/html;profile=mcp-app",
				},
			},
		})

	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return mcpErr(req.ID, -32602, "invalid params")
		}
		if params.URI == "ui://dotfilesd/sudo-prompt" {
			htmlContent := generateSudoPromptHTML()
			return mcpResp(req.ID, map[string]any{
				"contents": []map[string]any{
					{
						"uri":      params.URI,
						"mimeType": "text/html;profile=mcp-app",
						"text":     htmlContent,
					},
				},
			})
		}
		return mcpErr(req.ID, -32602, fmt.Sprintf("unknown resource: %s", params.URI))

	default:
		return mcpErr(req.ID, -32601, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func sessionFromArgs(args json.RawMessage, defaultSessionID string) *dotfilesdv1.Session {
	var p struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return sessionProto(defaultSessionID)
	}
	id := p.SessionID
	if id == "" {
		id = defaultSessionID
	}
	return sessionProto(id)
}

func callTool(clients *Clients, id json.RawMessage, name string, args json.RawMessage) *mcpResponse {
	// Connect lazily on first tool call so MCP mode starts without
	// requiring the daemon to be reachable at launch time.
	if err := clients.Connect(context.Background()); err != nil {
		return mcpErr(id, -32000, fmt.Sprintf("daemon connection failed: %v", err))
	}
	switch name {
	case "system_ping":
		req := connect.NewRequest(&dotfilesdv1.PingRequest{Session: sessionFromArgs(args, clients.SessionID)})
		resp, err := clients.Sys.Ping(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		s := resp.Msg
		text := fmt.Sprintf("dotfilesd v%s (pid %d, up %ds)", s.Version, s.Pid, s.UptimeSecs)
		return mcpToolResult(id, text)

	case "system_info":
		req := connect.NewRequest(&dotfilesdv1.SystemInfoRequest{Session: sessionFromArgs(args, clients.SessionID)})
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
		req := connect.NewRequest(&dotfilesdv1.SudoMethodsRequest{Session: sessionFromArgs(args, clients.SessionID)})
		resp, err := clients.Sys.SudoMethods(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		text := fmt.Sprintf("current: %s\nhas sudo: %v\navailable: %s",
			resp.Msg.CurrentMethod, resp.Msg.HasElevation,
			strings.Join(resp.Msg.AvailableMethods, ", "))
		return mcpToolResult(id, text)

	case "dotfiles_status":
		req := connect.NewRequest(&dotfilesdv1.StatusRequest{Session: sessionFromArgs(args, clients.SessionID)})
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
		req := connect.NewRequest(&dotfilesdv1.GitRequest{Action: action, Message: p.Message, Paths: p.Paths, Session: sessionFromArgs(args, clients.SessionID)})
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
			Command string `json:"command"`
			Sudo    bool   `json:"sudo"`
		}
		json.Unmarshal(args, &p)

		// When sudo=true and the client supports MCP Apps, we need to:
		// 1. Send a ui/notifications/tool-input notification so VS Code shows
		//    the webview with the password form.
		// 2. Block on a channel waiting for the password from the webview
		//    (via _sudo_submit_password).
		// 3. Execute sudo via SudoExec RPC.
		// 4. Return the result — this becomes the tools/call response, which
		//    VS Code forwards as tool-result to the webview AND returns to
		//    the agent as the tool call result.
		if p.Sudo && clientCaps.hasMcpApps {
			requestID := generateRequestID()
			passwordCh := make(chan string, 1)
			pending := &pendingSudoRequest{
				command:    p.Command,
				createdAt:  time.Now(),
				passwordCh: passwordCh,
			}
			pendingRequests.Store(requestID, pending)
			slog.Debug("blocking exec_run for sudo password", "request_id", requestID, "command", p.Command)

			// Send a tool-input notification BEFORE blocking. This tells
			// VS Code to show the webview and forward the tool arguments
			// so the password form can display the command to authorize.
			writeJSONLine(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"method":  "ui/notifications/tool-input",
				"params": map[string]any{
					"arguments": map[string]any{
						"command": p.Command,
						"sudo":    true,
					},
				},
			})

			// Block until password arrives or timeout (5 min).
			var password string
			select {
			case password = <-passwordCh:
				pendingRequests.Delete(requestID)
			case <-time.After(5 * time.Minute):
				pendingRequests.Delete(requestID)
				return mcpErr(id, -32602, "sudo password request timed out")
			}

			// Execute sudo via daemon's SudoExec RPC with the password.
			resp, err := clients.Exec.SudoExec(context.Background(), connect.NewRequest(&dotfilesdv1.SudoExecRequest{
				Command:  p.Command,
				Password: password,
			}))
			if err != nil {
				return mcpErr(id, -32603, fmt.Sprintf("sudo exec failed: %v", err))
			}
			result := resp.Msg.GetResult()
			if result == nil {
				return mcpErr(id, -32603, "unexpected response from daemon")
			}
			text := result.Stdout
			if result.Stderr != "" {
				text += "\nstderr:\n" + result.Stderr
			}
			return mcpResp(id, map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
				"isError": result.ExitCode != 0,
			})
		}

		// Non-sudo or no MCP Apps: use normal Exec RPC.
		// For sudo without MCP Apps, the daemon will use the session
		// callback URL to prompt for the password via elicitation feedback.
		req := connect.NewRequest(&dotfilesdv1.ExecRequest{
			Command: p.Command,
			Sudo:    p.Sudo,
			Session: sessionFromArgs(args, clients.SessionID),
		})
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
		req := connect.NewRequest(&dotfilesdv1.ReloadRequest{Target: target, Session: sessionFromArgs(args, clients.SessionID)})
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
		req := connect.NewRequest(&dotfilesdv1.ReconfigureRequest{LogLevel: logLevel, Session: sessionFromArgs(args, clients.SessionID)})
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
		req := connect.NewRequest(&dotfilesdv1.RestartRequest{Session: sessionFromArgs(args, clients.SessionID)})
		resp, err := clients.Cfg.Restart(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		return mcpToolResult(id, resp.Msg.Message)

	case "script_run":
		var p struct {
			Script     string `json:"script"`
			ScriptPath string `json:"script_path"`
		}
		json.Unmarshal(args, &p)

		if p.Script == "" && p.ScriptPath == "" {
			return mcpErr(id, -32602, "either 'script' or 'script_path' is required")
		}

		var req *connect.Request[dotfilesdv1.RunScriptRequest]
		session := sessionFromArgs(args, clients.SessionID)
		if p.ScriptPath != "" {
			req = connect.NewRequest(&dotfilesdv1.RunScriptRequest{
				Source:  &dotfilesdv1.RunScriptRequest_ScriptPath{ScriptPath: p.ScriptPath},
				Session: session,
			})
		} else {
			req = connect.NewRequest(&dotfilesdv1.RunScriptRequest{
				Source:  &dotfilesdv1.RunScriptRequest_Script{Script: p.Script},
				Session: session,
			})
		}
		resp, err := clients.Script.RunScript(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}

		var lines []string
		for _, step := range resp.Msg.Steps {
			switch step.StepKind {
			case "exec":
				l := fmt.Sprintf("[%d] $ %s", step.StepNumber, step.SourceLine)
				lines = append(lines, l)
				if step.Stdout != "" {
					lines = append(lines, step.Stdout)
				}
				if step.Stderr != "" {
					lines = append(lines, "stderr: "+step.Stderr)
				}
				if step.ExitCode != 0 {
					lines = append(lines, fmt.Sprintf("→ exit code %d", step.ExitCode))
				}
			case "confirm", "input", "choose":
				lines = append(lines, fmt.Sprintf("[%d] @%s → %s", step.StepNumber, step.StepKind, step.FeedbackValue))
			}
		}
		text := strings.Join(lines, "\n")
		if !resp.Msg.AllSucceeded {
			return mcpResp(id, map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
				"isError": true,
			})
		}
		return mcpToolResult(id, text)

	case "script_list":
		req := connect.NewRequest(&dotfilesdv1.ListScriptsRequest{Session: sessionFromArgs(args, clients.SessionID)})
		resp, err := clients.Script.ListScripts(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		var lines []string
		var printEntries func(entries []*dotfilesdv1.ScriptEntry, indent string)
		printEntries = func(entries []*dotfilesdv1.ScriptEntry, indent string) {
			for _, e := range entries {
				desc := e.Description
				if desc == "" {
					desc = e.Name
				}
				suffix := ""
				if !e.Enabled {
					suffix = " [disabled]"
				}
				if e.IsDirectory {
					lines = append(lines, fmt.Sprintf("%s%s/  %s", indent, e.Name, desc))
					printEntries(e.Children, indent+"  ")
				} else {
					lines = append(lines, fmt.Sprintf("%s%s  %s%s", indent, e.Name, desc, suffix))
				}
			}
		}
		printEntries(resp.Msg.Entries, "")
		if len(lines) == 0 {
			return mcpToolResult(id, "no registered scripts found")
		}
		return mcpToolResult(id, strings.Join(lines, "\n"))

	case "_sudo_submit_password":
		var p struct {
			RequestID string `json:"request_id"`
			Password  string `json:"password"`
		}
		json.Unmarshal(args, &p)
		if p.Password == "" {
			return mcpErr(id, -32602, "password is required")
		}

		// Find the pending request. If request_id is provided, look it up;
		// otherwise use the first (and only) pending request.
		var pending *pendingSudoRequest
		if p.RequestID != "" {
			val, ok := pendingRequests.Load(p.RequestID)
			if !ok {
				return mcpErr(id, -32602, "invalid or expired request_id")
			}
			pending = val.(*pendingSudoRequest)
		} else {
			// No request_id — find any pending request (there's at most one).
			var found bool
			pendingRequests.Range(func(key, value any) bool {
				pending = value.(*pendingSudoRequest)
				found = true
				return false
			})
			if !found {
				return mcpErr(id, -32602, "no pending sudo request")
			}
		}

		// Send password to the blocked exec_run goroutine.
		// The channel is buffered(1) so this never blocks.
		select {
		case pending.passwordCh <- p.Password:
			return mcpResp(id, map[string]any{
				"content": []map[string]any{{"type": "text", "text": "✅ Password submitted, executing command..."}},
			})
		default:
			return mcpErr(id, -32602, "password already submitted for this request")
		}

	default:
		return mcpErr(id, -32601, fmt.Sprintf("unknown tool: %s", name))
	}
}

func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateSudoPromptHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: #1e1f1c; color: #f8f8f2; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; padding: 16px; font-size: 14px; }
  .header { color: #a6e22e; font-weight: 600; margin-bottom: 8px; font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px; }
  .command { background: #272822; border: 1px solid #3e3d32; border-radius: 6px; padding: 10px; margin: 12px 0; font-family: 'SF Mono', 'Fira Code', monospace; color: #f8f8f2; word-break: break-all; font-size: 13px; }
  label { display: block; margin-bottom: 4px; color: #f8f8f2; font-size: 13px; }
  input[type="password"] { width: 100%; background: #272822; border: 1px solid #75715e; border-radius: 6px; padding: 10px 12px; color: #f8f8f2; font-size: 14px; font-family: inherit; outline: none; }
  input[type="password"]:focus { border-color: #a6e22e; }
  button { margin-top: 12px; background: #a6e22e; color: #272822; border: none; border-radius: 6px; padding: 10px 24px; font-size: 14px; font-weight: 600; cursor: pointer; }
  button:hover { background: #b6f23e; }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
  .status { margin-top: 10px; color: #75715e; font-size: 12px; }
  .hidden { display: none; }
  .success { color: #a6e22e; }
  .error { color: #f92672; }
  #result { margin-top: 12px; white-space: pre-wrap; font-family: 'SF Mono', 'Fira Code', monospace; font-size: 12px; max-height: 200px; overflow-y: auto; }
</style>
</head>
<body>
  <div id="loading">
    <div class="header">Sudo Password Required</div>
    <div class="status">Waiting for command...</div>
  </div>

  <div id="form" class="hidden">
    <div class="header">Sudo Password Required</div>
    <div class="command" id="commandDisplay">$ </div>
    <label for="password">Password:</label>
    <input type="password" id="password" placeholder="Enter sudo password" autofocus>
    <button id="submitBtn">Authenticate</button>
    <div class="status" id="status">Enter your password to authorize this command.</div>
  </div>

  <div id="result" class="hidden"></div>

<script>
(function() {
  let pendingCommand = '';
  const loading = document.getElementById('loading');
  const form = document.getElementById('form');
  const result = document.getElementById('result');
  const cmdDisplay = document.getElementById('commandDisplay');
  const passwordInput = document.getElementById('password');
  const submitBtn = document.getElementById('submitBtn');
  const statusEl = document.getElementById('status');
  let initSent = false;

  // MCP Apps: send ui/initialize when the page loads
  function sendInitialize() {
    if (initSent) return;
    initSent = true;
    window.parent.postMessage({
      jsonrpc: '2.0',
      id: 'init-1',
      method: 'ui/initialize',
      params: {
        appCapabilities: {},
        appInfo: { name: 'dotfilesd-sudo-prompt', version: '0.1.0' },
        protocolVersion: '2024-11-05'
      }
    }, '*');
  }

  // Call a tool via MCP JSON-RPC over postMessage
  let toolCallId = 100;
  function callTool(name, args) {
    const id = toolCallId++;
    window.parent.postMessage({
      jsonrpc: '2.0',
      id: id,
      method: 'tools/call',
      params: { name: name, arguments: args }
    }, '*');
    return id;
  }

  // Handle messages from the host
  window.addEventListener('message', function(event) {
    const data = event.data;
    if (!data || typeof data !== 'object') return;

    // Handle ui/initialize result
    if (data.id === 'init-1' && data.result) {
      loading.classList.add('hidden');
      form.classList.remove('hidden');
      statusEl.textContent = 'Waiting for command...';
      return;
    }

    // Handle ui/notifications/tool-input (contains the command arguments)
    if (data.method === 'ui/notifications/tool-input') {
      const args = data.params.arguments || {};
      pendingCommand = args.command || '';
      cmdDisplay.textContent = '$ ' + pendingCommand;
      statusEl.textContent = 'Enter your sudo password to authorize this command.';
      passwordInput.disabled = false;
      passwordInput.focus();
      return;
    }

    // Handle ui/notifications/tool-result — the exec_run goroutine has
    // unblocked and this IS the actual sudo command output.
    if (data.method === 'ui/notifications/tool-result') {
      const params = data.params || {};
      const text = (params.content || [])
        .map(function(c) { return c.text || ''; })
        .filter(Boolean)
        .join('\n');
      loading.classList.add('hidden');
      form.classList.add('hidden');
      result.classList.remove('hidden');
      result.textContent = text || '[empty result]';
      return;
    }

    // Handle response to _sudo_submit_password call
    if (typeof data.id === 'number' && data.id >= 100) {
      if (data.result) {
        statusEl.textContent = '✅ Password submitted, waiting for result...';
        submitBtn.disabled = true;
        passwordInput.disabled = true;
      } else if (data.error) {
        var errMsg = data.error.message || 'unknown error';
        statusEl.textContent = '❌ Error: ' + errMsg;
        submitBtn.disabled = false;
        passwordInput.disabled = false;
      }
      return;
    }
  });

  // Submit button handler
  submitBtn.addEventListener('click', function() {
    const pwd = passwordInput.value;
    if (!pwd) return;

    submitBtn.disabled = true;
    passwordInput.disabled = true;
    statusEl.textContent = 'Authenticating...';

    // Call _sudo_submit_password via MCP tools/call
    callTool('_sudo_submit_password', {
      request_id: '',
      password: pwd
    });

    passwordInput.value = '';
  });

  // Allow Enter to submit
  passwordInput.addEventListener('keydown', function(e) {
    if (e.key === 'Enter') submitBtn.click();
  });

  // Send initialize after a short delay to ensure DOM is ready
  setTimeout(sendInitialize, 100);
})();
</script>
</body>
</html>`
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
