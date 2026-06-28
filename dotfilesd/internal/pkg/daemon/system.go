package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

type systemServer struct {
	mu        sync.Mutex
	startedAt time.Time
	sessions  *SessionStore
	daemon    *Daemon
}

func (s *systemServer) Ping(ctx context.Context, req *connect.Request[dotfilesdv1.PingRequest]) (*connect.Response[dotfilesdv1.PingResponse], error) {
	slog.Log(ctx, levelTrace, "Ping", "request", req.Msg)
	s.sessions.ResolveSession(req.Msg.GetSession())
	resp := connect.NewResponse(&dotfilesdv1.PingResponse{
		Version:    "0.1.0",
		Pid:        int64(os.Getpid()),
		UptimeSecs: int64(time.Since(s.startedAt).Seconds()),
	})
	return resp, nil
}

func (s *systemServer) RuntimeInfo(ctx context.Context, req *connect.Request[dotfilesdv1.RuntimeInfoRequest]) (*connect.Response[dotfilesdv1.RuntimeInfoResponse], error) {
	slog.Log(ctx, levelTrace, "RuntimeInfo", "request", req.Msg)
	s.sessions.ResolveSession(req.Msg.GetSession())

	kernel, _ := runCmd("uname", "-r")
	shell := os.Getenv("SHELL")
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	hostname, _ := os.Hostname()
	uptimeRaw, _ := runCmd("uptime", "-p")

	var tools []string
	for _, name := range []string{"sudo", "pkexec", "tmux", "i3", "kitty"} {
		if _, err := exec.LookPath(name); err == nil {
			tools = append(tools, name)
		}
	}

	resp := connect.NewResponse(&dotfilesdv1.RuntimeInfoResponse{
		Os:             "linux",
		Kernel:         kernel,
		Shell:          shell,
		Desktop:        desktop,
		Hostname:       hostname,
		Uptime:         uptimeRaw,
		AvailableTools: tools,
	})
	return resp, nil
}

func (s *systemServer) SudoMethods(ctx context.Context, req *connect.Request[dotfilesdv1.SudoMethodsRequest]) (*connect.Response[dotfilesdv1.SudoMethodsResponse], error) {
	slog.Log(ctx, levelTrace, "SudoMethods", "request", req.Msg)
	s.sessions.ResolveSession(req.Msg.GetSession())

	var available []string
	for _, name := range []string{"pkexec", "sudo"} {
		if _, err := exec.LookPath(name); err == nil {
			available = append(available, name)
		}
	}
	current := "auto"
	if _, err := exec.LookPath("pkexec"); err == nil {
		current = "pkexec"
	}

	resp := connect.NewResponse(&dotfilesdv1.SudoMethodsResponse{
		AvailableMethods: available,
		CurrentMethod:    current,
		HasElevation:     len(available) > 0,
	})
	return resp, nil
}

func (s *systemServer) Diagnostics(ctx context.Context, req *connect.Request[dotfilesdv1.DiagnosticsRequest]) (*connect.Response[dotfilesdv1.DiagnosticsResponse], error) {
	slog.Log(ctx, levelTrace, "Diagnostics", "request", req.Msg)
	s.sessions.ResolveSession(req.Msg.GetSession())

	d := s.daemon
	execCalls := ListActiveCalls()
	allSessions := s.sessions.List()

	// Index sessions by ID for quick lookup.
	sessByID := make(map[string]*Session)
	for _, sess := range allSessions {
		sessByID[sess.id] = sess
	}

	// Determine which plugins have background workers.
	// A plugin has a background worker if its session (plugin-{name}) has a shell.
	plugins := d.pluginMgr.ListPlugins()
	pluginHasBG := make(map[string]bool)
	pluginHasActiveCall := make(map[string]bool)
	pluginPIDs := make(map[string]int)
	for _, info := range plugins {
		pluginPIDs[info.Name] = info.Process.Pid
		if sess, ok := sessByID["plugin-"+info.Name]; ok && sess.shell != nil {
			pluginHasBG[info.Name] = true
		}
		for _, call := range execCalls {
			if call.pluginName == info.Name {
				pluginHasActiveCall[info.Name] = true
				break
			}
		}
	}

	// Build list of active clients (clients with active executor streams).
	clientIDs := make(map[string]bool)
	for _, call := range execCalls {
		if call.clientID != "" {
			clientIDs[call.clientID] = true
		}
	}

	// ─── Tree 1: Daemon root (plugins with bg workers + scripts) ───
	root := &dotfilesdv1.DiagNode{
		Type:  "daemon",
		Label: fmt.Sprintf("dotfilesd (pid %d, port %s, up %ds)", os.Getpid(), d.config.Port, int64(time.Since(s.startedAt).Seconds())),
	}

	// Plugins with background workers under daemon.
	for _, info := range plugins {
		if !pluginHasBG[info.Name] {
			continue
		}
		pl := &dotfilesdv1.DiagNode{
			Type:   "plugin",
			Label:  fmt.Sprintf("%s v%s", info.DisplayName, info.Version),
			Status: "bg_worker",
			Attrs:  map[string]string{"pid": fmt.Sprintf("%d", info.Process.Pid), "url": info.URL},
		}
		// Add the background shell session.
		if sess, ok := sessByID["plugin-"+info.Name]; ok && sess.shell != nil {
			pl.Children = append(pl.Children, &dotfilesdv1.DiagNode{
				Type: "shell", Label: "bash", Status: "running",
				Attrs: map[string]string{"cwd": sess.shell.cwd},
			})
		}
		root.Children = append(root.Children, pl)
	}

	// Background tasks under daemon.
	if d.bgTasks != nil {
		tasks := d.bgTasks.ListTasks()
		for _, t := range tasks {
			root.Children = append(root.Children, &dotfilesdv1.DiagNode{
				Type: "bg_task", Label: t.id,
				Attrs: map[string]string{"command": t.cmd.String()},
			})
		}
	}

	// Scripts (always shown, counted as "available").
	scripts, err := d.scripts.ListScripts()
	if err == nil && len(scripts) > 0 {
		sn := &dotfilesdv1.DiagNode{Type: "scripts", Label: "Available scripts"}
		sn.Children = buildScriptNodes(scripts)
		root.Children = append(root.Children, sn)
	}

	// ─── Tree 2: Per-client trees ───
	// Build a list of response trees: first is daemon, rest are clients.
	var trees []*dotfilesdv1.DiagNode
	trees = append(trees, root)

	for cid := range clientIDs {
		ct := &dotfilesdv1.DiagNode{
			Type:  "client",
			Label: cid,
		}

		// Active executor streams for this client.
		for _, call := range execCalls {
			if call.clientID != cid {
				continue
			}
			ct.Children = append(ct.Children, &dotfilesdv1.DiagNode{
				Type:   "executor",
				Label:  fmt.Sprintf("%s.%s", call.service, call.method),
				Attrs:  map[string]string{"plugin": call.pluginName},
			})
		}

		trees = append(trees, ct)
	}

	// Combine trees under a synthetic root for the response.
	combined := &dotfilesdv1.DiagNode{
		Type:  "root",
		Label: fmt.Sprintf("dotfilesd diagnostics — %d tree(s)", len(trees)),
	}
	combined.Children = trees

	slog.Log(ctx, levelTrace, "Diagnostics done", "trees", len(trees))
	return connect.NewResponse(&dotfilesdv1.DiagnosticsResponse{Root: combined}), nil
}

func buildScriptNodes(entries []*dotfilesdv1.ScriptEntry) []*dotfilesdv1.DiagNode {
	var nodes []*dotfilesdv1.DiagNode
	for _, e := range entries {
		n := &dotfilesdv1.DiagNode{Type: "script", Label: e.Name}
		if e.Path != "" {
			n.Attrs = map[string]string{"path": e.Path}
		}
		if len(e.Children) > 0 {
			n.Children = buildScriptNodes(e.Children)
		}
		nodes = append(nodes, n)
	}
	return nodes
}
