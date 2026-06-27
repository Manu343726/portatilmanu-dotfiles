package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	_ "embed"
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

// sudoPromptHTML is the MCP Apps webview for sudo password entry.
//
//go:embed sudo_prompt.html
var sudoPromptHTML string

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
		Name:        "system_runtime",
		Description: "Show daemon runtime environment (OS, kernel, shell, desktop, hostname, uptime, available tools)",
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

// Plugin tool cache for MCP dynamic registration.
var (
	pluginTools   []toolDef
	pluginToolsMu sync.Mutex
)

// getPluginTools returns cached plugin tool definitions, fetching from daemon if needed.
func getPluginTools(clients *Clients) []toolDef {
	pluginToolsMu.Lock()
	defer pluginToolsMu.Unlock()
	if pluginTools != nil {
		return pluginTools
	}
	if err := clients.Connect(context.Background()); err != nil {
		return nil
	}
	tools, err := ListPluginTools(clients, "")
	if err != nil {
		slog.Debug("failed to fetch plugin tools", "error", err)
		return nil
	}
	pluginTools = tools
	return pluginTools
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
		allTools := append([]toolDef{}, mcpTools...)
		allTools = append(allTools, getPluginTools(clients)...)
		return mcpResp(req.ID, map[string]any{"tools": allTools})

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

	case "system_runtime":
		req := connect.NewRequest(&dotfilesdv1.RuntimeInfoRequest{Session: sessionFromArgs(args, clients.SessionID)})
		resp, err := clients.Sys.RuntimeInfo(context.Background(), req)
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		s := resp.Msg
		text := fmt.Sprintf("os: %s\nkernel: %s\nshell: %s\ndesktop: %s\nhost: %s\nuptime: %s\ntools: %s",
			s.Os, s.Kernel, s.Shell, s.Desktop,
			s.Hostname, s.Uptime,
			strings.Join(s.AvailableTools, ", "))
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
		text := fmt.Sprintf("branch: %s\nclean: %v\nlast: %s",
			s.GitBranch, s.GitClean, s.LastCommit)
		return mcpToolResult(id, text)

	case "dotfiles_git":
		var p struct {
			Action  string `json:"action"`
			Message string `json:"message"`
			Paths   string `json:"paths"`
		}
		json.Unmarshal(args, &p)
		if p.Action == "" {
			return mcpErr(id, -32602, "action is required (status, diff, add, commit, push, log)")
		}
		// Run via scripts/git/<action>.dsh
		return runMCPToolViaScript(id, clients, "git/"+p.Action, args)

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
		target := p.Target
		if target == "" {
			target = "all"
		}
		return runMCPToolViaScript(id, clients, "reload/"+target, args)

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
		// Try plugin tool dispatch for qualified names (format: "<plugin>_<tool>").
		if !strings.Contains(name, "_") {
			return mcpErr(id, -32601, fmt.Sprintf("unknown tool: %s", name))
		}
		parts := splitQualifiedName(name)
		if len(parts) < 2 {
			return mcpErr(id, -32601, fmt.Sprintf("unknown tool: %s", name))
		}

		// Parse arguments into flat string map.
		var rawArgs map[string]json.RawMessage
		strArgs := make(map[string]string)
		sessionID := ""
		if err := json.Unmarshal(args, &rawArgs); err == nil {
			for k, v := range rawArgs {
				if k == "session_id" {
					json.Unmarshal(v, &sessionID)
					continue
				}
				var s string
				if err := json.Unmarshal(v, &s); err == nil {
					strArgs[k] = s
				} else {
					// Non-string value: pass as JSON.
					strArgs[k] = string(v)
				}
			}
		}

		text, err := CallPluginToolViaMCP(clients, sessionID, name, strArgs)
		if err != nil {
			return mcpErr(id, -32603, fmt.Sprintf("plugin tool call failed: %v", err))
		}
		return mcpToolResult(id, text)
	}
}

// runMCPToolViaScript dispatches a tool call to a registered script.
func runMCPToolViaScript(id json.RawMessage, clients *Clients, scriptName string, args json.RawMessage) *mcpResponse {
	req := connect.NewRequest(&dotfilesdv1.RunScriptRequest{
		Session: sessionFromArgs(args, clients.SessionID),
		Source:  &dotfilesdv1.RunScriptRequest_RegisteredScript{RegisteredScript: scriptName},
	})
	resp, err := clients.Script.RunScript(context.Background(), req)
	if err != nil {
		return mcpErr(id, -32603, fmt.Sprintf("script %s: %v", scriptName, err))
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
}

func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateSudoPromptHTML() string {
	return sudoPromptHTML
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
