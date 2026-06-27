package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

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

	slog.Log(ctx, levelTrace, "Ping done", "response", resp.Msg)
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

	// Detect tools available on PATH.
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

	slog.Log(ctx, levelTrace, "RuntimeInfo done", "response", resp.Msg)
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

	slog.Log(ctx, levelTrace, "SudoMethods done", "response", resp.Msg)
	return resp, nil
}
