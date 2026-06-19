package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

type MCPSession struct {
	ID     string
	events chan []byte
	mu     sync.Mutex
	closed bool
}

type MCPServer struct {
	svc      *dotfilesServer
	sessions map[string]*MCPSession
	mu       sync.RWMutex
	port     int
}

func NewMCPServer(svc *dotfilesServer, port int) *MCPServer {
	return &MCPServer{
		svc:      svc,
		sessions: make(map[string]*MCPSession),
		port:     port,
	}
}

func (m *MCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/sse":
		m.handleSSE(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/message":
		m.handleMessage(w, r)
	default:
		http.NotFound(w, r)
	}
}

type ToolDefinition struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema ToolSchema `json:"inputSchema"`
}

type ToolSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

type PropertySchema struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

func (m *MCPServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	session := &MCPSession{
		ID:     fmt.Sprintf("%d", time.Now().UnixNano()),
		events: make(chan []byte, 64),
	}

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.sessions, session.ID)
		session.mu.Lock()
		session.closed = true
		close(session.events)
		session.mu.Unlock()
		m.mu.Unlock()
	}()

	w.Write(m.sse("session_id", []byte(session.ID)))
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-session.events:
			if !ok {
				return
			}
			if _, err := w.Write(data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (m *MCPServer) handleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}

	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		http.Error(w, "invalid session", http.StatusNotFound)
		return
	}

	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		session.events <- m.sse("message", m.jsonErr(nil, -32700, "parse error"))
		w.WriteHeader(http.StatusOK)
		return
	}

	isNotify := req.ID == nil || len(req.ID) == 0

	if isNotify {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	resp := m.dispatch(req)
	session.events <- m.sse("message", resp)
	w.WriteHeader(http.StatusOK)
}

func (m *MCPServer) dispatch(req jsonRPCRequest) []byte {
	switch req.Method {
	case "initialize":
		return m.jsonResp(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]bool{"listChanged": false},
			},
			"serverInfo": map[string]string{
				"name":    "dotfilesd",
				"version": "0.1.0",
			},
		})

	case "tools/list":
		tools := []ToolDefinition{
			{Name: "dotfiles_status", Description: "Show dotfiles repo status and system info", InputSchema: ToolSchema{Type: "object"}},
			{Name: "dotfiles_reload", Description: "Reload dotfiles configs (tmux, i3, kitty)", InputSchema: ToolSchema{Type: "object", Properties: map[string]PropertySchema{"target": {Type: "string", Enum: []string{"tmux", "i3", "kitty", "all"}}}}},
			{Name: "dotfiles_git", Description: "Git operations on the dotfiles repo", InputSchema: ToolSchema{Type: "object", Properties: map[string]PropertySchema{"action": {Type: "string", Enum: []string{"status", "diff", "add", "commit", "push", "log"}}, "message": {Type: "string"}}, Required: []string{"action"}}},
			{Name: "system_exec", Description: "Execute a shell command", InputSchema: ToolSchema{Type: "object", Properties: map[string]PropertySchema{"command": {Type: "string"}, "sudo": {Type: "string", Enum: []string{"true", "false"}}}, Required: []string{"command"}}},
			{Name: "system_info", Description: "Detailed system information", InputSchema: ToolSchema{Type: "object"}},
		}
		return m.jsonResp(req.ID, map[string]any{"tools": tools})

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return m.jsonErr(req.ID, -32602, "invalid params")
		}
		return m.callTool(req.ID, params.Name, params.Arguments)

	default:
		return m.jsonErr(req.ID, -32601, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func (m *MCPServer) callTool(id json.RawMessage, name string, args json.RawMessage) []byte {
	svc := m.svc

	switch name {
	case "dotfiles_status":
		resp, err := svc.Status(context.Background(), connect.NewRequest(&dotfilesdv1.StatusRequest{}))
		if err != nil {
			return m.jsonErr(id, -32603, err.Error())
		}
		s := resp.Msg
		text := fmt.Sprintf("branch: %s\nclean: %v\nlast: %s\nhost: %s\nuptime: %s",
			s.GitBranch, s.GitClean, s.LastCommit, s.Hostname, s.Uptime)
		return m.jsonResp(id, map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
		})

	case "dotfiles_reload":
		var p struct{ Target string `json:"target"` }
		json.Unmarshal(args, &p)
		if p.Target == "" {
			p.Target = "all"
		}
		resp, err := svc.Reload(context.Background(), connect.NewRequest(&dotfilesdv1.ReloadRequest{Target: p.Target}))
		if err != nil {
			return m.jsonErr(id, -32603, err.Error())
		}
		var lines []string
		for _, r := range resp.Msg.Results {
			s := "ok"
			if !r.Success {
				s = "err"
			}
			lines = append(lines, fmt.Sprintf("%s: %s (%s)", r.Target, s, r.Message))
		}
		return m.jsonResp(id, map[string]any{
			"content": []map[string]any{{"type": "text", "text": stringsJoin(lines, "\n")}},
		})

	case "dotfiles_git":
		var p struct {
			Action  string `json:"action"`
			Message string `json:"message"`
			Paths   string `json:"paths"`
		}
		json.Unmarshal(args, &p)
		resp, err := svc.Git(context.Background(), connect.NewRequest(&dotfilesdv1.GitRequest{Action: p.Action, Message: p.Message, Paths: p.Paths}))
		if err != nil {
			return m.jsonErr(id, -32603, err.Error())
		}
		if resp.Msg.ExitCode != 0 {
			return m.jsonResp(id, map[string]any{
				"content": []map[string]any{{"type": "text", "text": resp.Msg.Stderr}},
				"isError": true,
			})
		}
		return m.jsonResp(id, map[string]any{
			"content": []map[string]any{{"type": "text", "text": resp.Msg.Stdout}},
		})

	case "system_exec":
		var p struct {
			Command string `json:"command"`
			Sudo    string `json:"sudo"`
		}
		json.Unmarshal(args, &p)
		resp, err := svc.Exec(context.Background(), connect.NewRequest(&dotfilesdv1.ExecRequest{Command: p.Command, Sudo: p.Sudo == "true"}))
		if err != nil {
			return m.jsonErr(id, -32603, err.Error())
		}
		text := resp.Msg.Stdout
		if resp.Msg.Stderr != "" {
			text += "\nstderr:\n" + resp.Msg.Stderr
		}
		isErr := resp.Msg.ExitCode != 0
		return m.jsonResp(id, map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": isErr,
		})

	case "system_info":
		resp, err := svc.SystemInfo(context.Background(), connect.NewRequest(&dotfilesdv1.SystemInfoRequest{}))
		if err != nil {
			return m.jsonErr(id, -32603, err.Error())
		}
		s := resp.Msg
		text := fmt.Sprintf("os: %s\nkernel: %s\nshell: %s\ndesktop: %s\nmemory: %d MB total / %d MB avail\ncpu: %.2f load\n%s\n%s\n%s",
			s.Os, s.Kernel, s.Shell, s.Desktop,
			s.MemoryTotalKb/1024, s.MemoryAvailKb/1024,
			s.CpuLoad_1M,
			s.TmuxVersion, s.KittyVersion, s.I3Version)
		return m.jsonResp(id, map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
		})

	default:
		return m.jsonErr(id, -32601, fmt.Sprintf("unknown tool: %s", name))
	}
}

func (m *MCPServer) ListenAndServe() error {
	addr := fmt.Sprintf("127.0.0.1:%d", m.port)
	slog.Info("mcp listening", "addr", addr)
	return http.ListenAndServe(addr, m)
}

func (m *MCPServer) sse(event string, data []byte) []byte {
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, data))
}

func (m *MCPServer) jsonResp(id json.RawMessage, result any) []byte {
	d, _ := json.Marshal(result)
	r := jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: d}
	b, _ := json.Marshal(r)
	return b
}

func (m *MCPServer) jsonErr(id json.RawMessage, code int, msg string) []byte {
	r := jsonRPCResponse{JSONRPC: "2.0", ID: id, Error: &jsonRPCError{Code: code, Message: msg}}
	b, _ := json.Marshal(r)
	return b
}

func stringsJoin(elems []string, sep string) string {
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
