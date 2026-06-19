package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

type configServer struct{}

func (s *configServer) Reload(ctx context.Context, req *connect.Request[dotfilesdv1.ReloadRequest]) (*connect.Response[dotfilesdv1.ReloadResponse], error) {
	slog.Log(ctx, levelTrace, "Config.Reload", "target", req.Msg.Target)

	target := req.Msg.Target

	type result struct {
		target  string
		success bool
		message string
	}
	var results []result

	do := func(t, cmd string, args ...string) {
		out, err := runCmd(cmd, args...)
		msg := strings.TrimSpace(out)
		if err != nil {
			msg = fmt.Sprintf("%s (non-fatal)", err)
		}
		results = append(results, result{target: t, success: err == nil, message: msg})
	}

	switch target {
	case dotfilesdv1.ReloadTarget_RELOAD_TARGET_TMUX:
		do("tmux", "tmux", "source-file", os.Getenv("HOME")+"/.tmux.conf")
	case dotfilesdv1.ReloadTarget_RELOAD_TARGET_I3:
		do("i3", "i3-msg", "reload")
	case dotfilesdv1.ReloadTarget_RELOAD_TARGET_KITTY:
		do("kitty", "kitty", "@", "load-config")
	case dotfilesdv1.ReloadTarget_RELOAD_TARGET_ALL:
		do("tmux", "tmux", "source-file", os.Getenv("HOME")+"/.tmux.conf")
		do("i3", "i3-msg", "reload")
		do("kitty", "kitty", "@", "load-config")
	default:
		results = append(results, result{target: target.String(), success: false, message: fmt.Sprintf("unknown target: %s", target)})
	}

	resp := &dotfilesdv1.ReloadResponse{}
	for _, r := range results {
		resp.Results = append(resp.Results, &dotfilesdv1.ReloadResponse_ReloadResult{
			Target:  r.target,
			Success: r.success,
			Message: r.message,
		})
	}

	slog.Log(ctx, levelTrace, "Config.Reload done", "results", resp.Results)
	return connect.NewResponse(resp), nil
}

func (s *configServer) Reconfigure(ctx context.Context, req *connect.Request[dotfilesdv1.ReconfigureRequest]) (*connect.Response[dotfilesdv1.ReconfigureResponse], error) {
	r := req.Msg
	slog.Log(ctx, levelTrace, "Config.Reconfigure", "log_level", r.LogLevel)

	newLevel := logLevelToSlog(r.LogLevel)
	if r.LogLevel == dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED {
		msg := "invalid log level (valid: trace, debug, info, warn, error)"
		slog.Warn("Reconfigure: invalid log level", "log_level", r.LogLevel)
		return connect.NewResponse(&dotfilesdv1.ReconfigureResponse{
			Success: false,
			Message: msg,
		}), nil
	}

	logLevelVar.Set(newLevel)
	msg := fmt.Sprintf("log level changed to %s", r.LogLevel)
	slog.Warn("Reconfigure applied", "log_level", r.LogLevel)

	return connect.NewResponse(&dotfilesdv1.ReconfigureResponse{
		Success: true,
		Message: msg,
	}), nil
}

func (s *configServer) Restart(ctx context.Context, req *connect.Request[dotfilesdv1.RestartRequest]) (*connect.Response[dotfilesdv1.RestartResponse], error) {
	slog.Warn("Restart requested")

	go gracefulRestart(500 * time.Millisecond)

	return connect.NewResponse(&dotfilesdv1.RestartResponse{
		Message: "daemon restarting in 500ms, reconnect after ~3s",
	}), nil
}
