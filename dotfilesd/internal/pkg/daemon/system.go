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
	root := &dotfilesdv1.DiagNode{
		Type:  "daemon",
		Label: "dotfilesd",
		Attrs: map[string]string{
			"pid":     fmt.Sprintf("%d", os.Getpid()),
			"uptime":  fmt.Sprintf("%ds", int64(time.Since(s.startedAt).Seconds())),
			"port":    d.config.Port,
			"version": "0.1.0",
		},
	}

	// ─── Clients & Executor streams ───
	execCalls := ListActiveCalls()
	clientNode := &dotfilesdv1.DiagNode{Type: "clients", Label: fmt.Sprintf("Clients (%d)", len(execCalls))}
	seenClients := make(map[string]bool)
	for _, call := range execCalls {
		clientID := call.clientID
		if !seenClients[clientID] {
			seenClients[clientID] = true
			cl := &dotfilesdv1.DiagNode{Type: "client", Label: clientID}
			clientNode.Children = append(clientNode.Children, cl)
		}
		// Find parent and add executor child.
		for _, cl := range clientNode.Children {
			if cl.Label == clientID {
				cl.Children = append(cl.Children, &dotfilesdv1.DiagNode{
					Type:   "executor",
					Label:  fmt.Sprintf("%s.%s", call.service, call.method),
					Attrs:  map[string]string{"plugin": call.pluginName},
				})
				break
			}
		}
	}
	root.Children = append(root.Children, clientNode)

	// ─── Sessions ───
	sessions := s.sessions.List()
	sessNode := &dotfilesdv1.DiagNode{Type: "sessions", Label: fmt.Sprintf("Sessions (%d)", len(sessions))}
	for i := range sessions {
		sess := sessions[i]
		status := "active"
		if sess.finalized {
			status = "finalized"
		}
		sn := &dotfilesdv1.DiagNode{
			Type:   "session",
			Label:  sess.id,
			Status: status,
			Attrs: map[string]string{
				"created":  sess.createdAt.Format(time.RFC3339),
				"callback": sess.callbackURL,
			},
		}
		if sess.shell != nil {
			sn.Children = append(sn.Children, &dotfilesdv1.DiagNode{
				Type: "shell", Label: "bash",
				Attrs: map[string]string{"cwd": sess.shell.cwd},
			})
		}
		sessNode.Children = append(sessNode.Children, sn)
	}
	root.Children = append(root.Children, sessNode)

	// ─── Plugins ───
	if d.pluginMgr != nil {
		plugins := d.pluginMgr.ListPlugins()
		pn := &dotfilesdv1.DiagNode{Type: "plugins", Label: fmt.Sprintf("Plugins (%d)", len(plugins))}
		for _, info := range plugins {
			pl := &dotfilesdv1.DiagNode{
				Type:   "plugin",
				Label:  fmt.Sprintf("%s v%s", info.DisplayName, info.Version),
				Status: "running",
				Attrs: map[string]string{
					"url": info.URL,
					"pid": fmt.Sprintf("%d", info.Process.Pid),
				},
			}
			for _, svc := range info.Services {
				pl.Children = append(pl.Children, &dotfilesdv1.DiagNode{
					Type: "service", Label: svc, Status: "available",
				})
			}
			for _, call := range execCalls {
				if call.pluginName == info.Name {
					pl.Children = append(pl.Children, &dotfilesdv1.DiagNode{
						Type: "caller", Label: call.clientID,
						Attrs: map[string]string{"svc": call.service, "method": call.method},
					})
				}
			}
			pn.Children = append(pn.Children, pl)
		}
		root.Children = append(root.Children, pn)
	}

	// ─── Background Tasks ───
	if d.bgTasks != nil {
		tasks := d.bgTasks.ListTasks()
		bn := &dotfilesdv1.DiagNode{Type: "bg_tasks", Label: fmt.Sprintf("Background tasks (%d)", len(tasks))}
		for _, t := range tasks {
			bn.Children = append(bn.Children, &dotfilesdv1.DiagNode{
				Type: "bg_task", Label: t.id,
				Attrs: map[string]string{"command": t.cmd.String()},
			})
		}
		root.Children = append(root.Children, bn)
	}

	// ─── Scripts ───
	scripts, err := d.scripts.ListScripts()
	if err == nil && len(scripts) > 0 {
		sn := &dotfilesdv1.DiagNode{Type: "scripts", Label: fmt.Sprintf("Scripts (%d)", len(scripts))}
		sn.Children = buildScriptNodes(scripts)
		root.Children = append(root.Children, sn)
	}

	slog.Log(ctx, levelTrace, "Diagnostics done", "tree_size", countNodes(root))
	return connect.NewResponse(&dotfilesdv1.DiagnosticsResponse{Root: root}), nil
}

func buildScriptNodes(entries []*dotfilesdv1.ScriptEntry) []*dotfilesdv1.DiagNode {
	var nodes []*dotfilesdv1.DiagNode
	for _, e := range entries {
		n := &dotfilesdv1.DiagNode{Type: "script", Label: e.Name, Status: "available"}
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

func countNodes(n *dotfilesdv1.DiagNode) int {
	c := 1
	for _, ch := range n.Children {
		c += countNodes(ch)
	}
	return c
}
