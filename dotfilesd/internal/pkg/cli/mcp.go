package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
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
		InputSchema: toolSchema{Type: "object"},
	},
	{
		Name:        "system_info",
		Description: "Detailed system information",
		InputSchema: toolSchema{Type: "object"},
	},
	{
		Name:        "system_sudo",
		Description: "Show available sudo methods",
		InputSchema: toolSchema{Type: "object"},
	},
	{
		Name:        "dotfiles_status",
		Description: "Show dotfiles repo status",
		InputSchema: toolSchema{Type: "object"},
	},
	{
		Name:        "dotfiles_git",
		Description: "Git operations on the dotfiles repo",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"action":  {Type: "string", Enum: []string{"status", "diff", "add", "commit", "push", "log"}},
			"message": {Type: "string"},
			"paths":   {Type: "string"},
		}, Required: []string{"action"}},
	},
	{
		Name:        "exec_run",
		Description: "Execute a shell command",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"command": {Type: "string"},
			"sudo":    {Type: "string", Enum: []string{"true", "false"}},
		}, Required: []string{"command"}},
	},
	{
		Name:        "config_reload",
		Description: "Reload dotfiles configs (tmux, i3, kitty)",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"target": {Type: "string", Enum: []string{"tmux", "i3", "kitty", "all"}},
		}},
	},
	{
		Name:        "config_reconfigure",
		Description: "Change daemon runtime configuration (e.g. log level)",
		InputSchema: toolSchema{Type: "object", Properties: map[string]propSchema{
			"log_level": {Type: "string", Description: "new log level", Enum: []string{"trace", "debug", "info", "warn", "error"}},
		}, Required: []string{"log_level"}},
	},
	{
		Name:        "config_restart",
		Description: "Gracefully restart the dotfilesd daemon",
		InputSchema: toolSchema{Type: "object"},
	},
}

func RunMCP(clients *Clients) {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	reader := bufio.NewReader(os.Stdin)
	for {
		raw, err := readMCPFrame(reader)
		if err != nil {
			if err == io.EOF {
				return
			}
			slog.Error("read frame", "error", err)
			return
		}

		var req mcpRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			slog.Error("parse request", "error", err)
			continue
		}

		if req.ID == nil || len(req.ID) == 0 {
			continue
		}

		resp := dispatchMCP(clients, req)
		if resp != nil {
			writeMCPFrame(os.Stdout, resp)
		}
	}
}

func readMCPFrame(reader *bufio.Reader) ([]byte, error) {
	var contentLength int
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			contentLength, _ = strconv.Atoi(strings.TrimPrefix(line, "Content-Length: "))
		}
	}
	if contentLength == 0 {
		return nil, fmt.Errorf("no Content-Length header")
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func writeMCPFrame(w io.Writer, resp *mcpResponse) {
	data, _ := json.Marshal(resp)
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	w.Write([]byte(header))
	w.Write(data)
}

func dispatchMCP(clients *Clients, req mcpRequest) *mcpResponse {
	switch req.Method {
	case "initialize":
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

func callTool(clients *Clients, id json.RawMessage, name string, args json.RawMessage) *mcpResponse {
	switch name {
	case "system_ping":
		resp, err := clients.Sys.Ping(context.Background(), connect.NewRequest(&dotfilesdv1.PingRequest{}))
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		s := resp.Msg
		text := fmt.Sprintf("dotfilesd v%s (pid %d, up %ds)", s.Version, s.Pid, s.UptimeSecs)
		return mcpToolResult(id, text)

	case "system_info":
		resp, err := clients.Sys.SystemInfo(context.Background(), connect.NewRequest(&dotfilesdv1.SystemInfoRequest{}))
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
		resp, err := clients.Sys.SudoMethods(context.Background(), connect.NewRequest(&dotfilesdv1.SudoMethodsRequest{}))
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		text := fmt.Sprintf("current: %s\nhas sudo: %v\navailable: %s",
			resp.Msg.CurrentMethod, resp.Msg.HasElevation,
			strings.Join(resp.Msg.AvailableMethods, ", "))
		return mcpToolResult(id, text)

	case "dotfiles_status":
		resp, err := clients.Dot.Status(context.Background(), connect.NewRequest(&dotfilesdv1.StatusRequest{}))
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
		resp, err := clients.Dot.Git(context.Background(), connect.NewRequest(&dotfilesdv1.GitRequest{Action: action, Message: p.Message, Paths: p.Paths}))
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
			Sudo    string `json:"sudo"`
		}
		json.Unmarshal(args, &p)

		if p.Sudo != "true" {
			resp, err := clients.Exec.Exec(context.Background(), connect.NewRequest(&dotfilesdv1.ExecRequest{Command: p.Command}))
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
		}

		resp, err := clients.Exec.SudoExec(context.Background(), connect.NewRequest(&dotfilesdv1.SudoExecRequest{
			Command: p.Command, PreferredMethod: dotfilesdv1.SudoMethod_SUDO_METHOD_GRAPHICAL,
		}))
		if err != nil {
			return mcpErr(id, -32603, err.Error())
		}
		result := resp.Msg.GetResult()
		if result == nil {
			challenge := resp.Msg.GetAuthChallenge()
			if challenge != nil {
				return mcpErr(id, -32000, "auth required: cannot prompt in MCP context, use terminal")
			}
			return mcpErr(id, -32603, "unexpected response from daemon")
		}
		if result.AuthCancelled {
			return mcpResp(id, map[string]any{
				"content": []map[string]any{{"type": "text", "text": result.Stderr}},
				"isError": true,
			})
		}
		text := result.Stdout
		if result.Stderr != "" {
			text += "\nstderr:\n" + result.Stderr
		}
		return mcpResp(id, map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": result.ExitCode != 0,
		})

	case "config_reload":
		var p struct{ Target string `json:"target"` }
		json.Unmarshal(args, &p)
		target := dotfilesdv1.ReloadTarget_RELOAD_TARGET_ALL
		if p.Target != "" {
			target = ParseReloadTarget(p.Target)
			if target == dotfilesdv1.ReloadTarget_RELOAD_TARGET_UNSPECIFIED {
				return mcpErr(id, -32602, fmt.Sprintf("unknown target: %s", p.Target))
			}
		}
		resp, err := clients.Cfg.Reload(context.Background(), connect.NewRequest(&dotfilesdv1.ReloadRequest{Target: target}))
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
		var p struct{ LogLevel string `json:"log_level"` }
		json.Unmarshal(args, &p)
		if p.LogLevel == "" {
			return mcpErr(id, -32602, "log_level is required")
		}
		logLevel := ParseLogLevel(p.LogLevel)
		if logLevel == dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED {
			return mcpErr(id, -32602, fmt.Sprintf("invalid log level: %s", p.LogLevel))
		}
		resp, err := clients.Cfg.Reconfigure(context.Background(), connect.NewRequest(&dotfilesdv1.ReconfigureRequest{LogLevel: logLevel}))
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
		resp, err := clients.Cfg.Restart(context.Background(), connect.NewRequest(&dotfilesdv1.RestartRequest{}))
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
