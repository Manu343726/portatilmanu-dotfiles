package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

type systemServer struct {
	mu        sync.Mutex
	startedAt time.Time
	sessions  *SessionStore
}

func (s *systemServer) Ping(ctx context.Context, req *connect.Request[dotfilesdv1.PingRequest]) (*connect.Response[dotfilesdv1.PingResponse], error) {
	slog.Log(ctx, levelTrace, "Ping", "request", req.Msg)
	s.sessions.Resolve(GetSessionID(req))

	resp := connect.NewResponse(&dotfilesdv1.PingResponse{
		Version:    "0.1.0",
		Pid:        int64(os.Getpid()),
		UptimeSecs: int64(time.Since(s.startedAt).Seconds()),
	})

	slog.Log(ctx, levelTrace, "Ping done", "response", resp.Msg)
	return resp, nil
}

func (s *systemServer) SystemInfo(ctx context.Context, req *connect.Request[dotfilesdv1.SystemInfoRequest]) (*connect.Response[dotfilesdv1.SystemInfoResponse], error) {
	slog.Log(ctx, levelTrace, "SystemInfo", "request", req.Msg)
	s.sessions.Resolve(GetSessionID(req))

	kernel, _ := runCmd("uname", "-r")
	shell := os.Getenv("SHELL")
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	tmuxVer, _ := runCmd("tmux", "-V")
	kittyVer, _ := runCmd("kitty", "--version")
	i3Ver, _ := runCmd("i3", "--version")

	memTotal, _ := runCmd("awk", "/^MemTotal:/ {print $2}", "/proc/meminfo")
	memAvail, _ := runCmd("awk", "/^MemAvailable:/ {print $2}", "/proc/meminfo")
	load1, _ := runCmd("awk", "{print $1}", "/proc/loadavg")

	var memTotalKb, memAvailKb int64
	var cpuLoad float64
	parseFloats := func(s string, v any) {
		_, _ = fmtSscanf(s, v)
	}
	parseFloats(memTotal, &memTotalKb)
	parseFloats(memAvail, &memAvailKb)
	parseFloats(load1, &cpuLoad)

	resp := connect.NewResponse(&dotfilesdv1.SystemInfoResponse{
		Os:            "linux",
		Kernel:        kernel,
		Shell:         shell,
		Desktop:       desktop,
		MemoryTotalKb: memTotalKb,
		MemoryAvailKb: memAvailKb,
		CpuLoad_1M:    cpuLoad,
		TmuxVersion:   tmuxVer,
		KittyVersion:  kittyVer,
		I3Version:     i3Ver,
	})

	slog.Log(ctx, levelTrace, "SystemInfo done", "response", resp.Msg)
	return resp, nil
}

func (s *systemServer) SudoMethods(ctx context.Context, req *connect.Request[dotfilesdv1.SudoMethodsRequest]) (*connect.Response[dotfilesdv1.SudoMethodsResponse], error) {
	slog.Log(ctx, levelTrace, "SudoMethods", "request", req.Msg)
	s.sessions.Resolve(GetSessionID(req))

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
