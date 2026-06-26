package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"dotfilesd/internal/pkg/plugin"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

type systemServer struct {
	mu        sync.Mutex
	startedAt time.Time
	sessions  *SessionStore
	daemon    *Daemon // for plugin access
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

func (s *systemServer) SystemInfo(ctx context.Context, req *connect.Request[dotfilesdv1.SystemInfoRequest]) (*connect.Response[dotfilesdv1.SystemInfoResponse], error) {
	slog.Log(ctx, levelTrace, "SystemInfo", "request", req.Msg)
	s.sessions.ResolveSession(req.Msg.GetSession())

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

func (s *systemServer) ListPlugins(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ListPluginsRequest],
) (*connect.Response[dotfilesdv1.ListPluginsResponse], error) {
	slog.Log(ctx, levelTrace, "ListPlugins", "request", req.Msg)
	s.sessions.ResolveSession(req.Msg.GetSession())

	if s.daemon == nil || s.daemon.pluginMgr == nil {
		return connect.NewResponse(&dotfilesdv1.ListPluginsResponse{}), nil
	}

	plugins := s.daemon.pluginMgr.ListPlugins()
	protoPlugins := make([]*dotfilesdv1.ExtensionDescriptor, len(plugins))
	for i, p := range plugins {
		protoPlugins[i] = plugin.ToProtoDescriptor(&p)
	}

	resp := connect.NewResponse(&dotfilesdv1.ListPluginsResponse{
		Plugins: protoPlugins,
	})
	slog.Log(ctx, levelTrace, "ListPlugins done", "count", len(protoPlugins))
	return resp, nil
}

func (s *systemServer) ListPluginTree(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ListPluginTreeRequest],
) (*connect.Response[dotfilesdv1.ListPluginTreeResponse], error) {
	slog.Log(ctx, levelTrace, "ListPluginTree", "request", req.Msg)
	s.sessions.ResolveSession(req.Msg.GetSession())

	if s.daemon == nil || s.daemon.pluginMgr == nil {
		return connect.NewResponse(&dotfilesdv1.ListPluginTreeResponse{}), nil
	}

	tree := s.daemon.pluginMgr.ListPluginTree()
	protoEntries := make([]*dotfilesdv1.PluginTreeEntry, len(tree))
	for i := range tree {
		protoEntries[i] = plugin.ToProtoPluginTree(&tree[i])
	}

	resp := connect.NewResponse(&dotfilesdv1.ListPluginTreeResponse{
		Entries: protoEntries,
	})
	slog.Log(ctx, levelTrace, "ListPluginTree done", "count", len(protoEntries))
	return resp, nil
}

func (s *systemServer) CallPluginTool(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.CallPluginToolRequest],
) (*connect.Response[dotfilesdv1.CallPluginToolResponse], error) {
	slog.Log(ctx, levelTrace, "CallPluginTool", "request", req.Msg)
	s.sessions.ResolveSession(req.Msg.GetSession())

	if s.daemon == nil || s.daemon.pluginMgr == nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("plugin system not initialized"))
	}

	text, isErr, structured, err := s.daemon.pluginMgr.CallTool(ctx, req.Msg.PluginName, req.Msg.ToolName, req.Msg.Arguments)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("call plugin tool: %w", err))
	}

	resp := connect.NewResponse(&dotfilesdv1.CallPluginToolResponse{
		Text:           text,
		IsError:        isErr,
		StructuredData: structured,
	})
	slog.Log(ctx, levelTrace, "CallPluginTool done", "plugin", req.Msg.PluginName, "tool", req.Msg.ToolName)
	return resp, nil
}
