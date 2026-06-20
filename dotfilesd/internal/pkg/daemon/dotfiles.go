package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

type dotfilesServer struct {
	sessions *SessionStore
}

func (s *dotfilesServer) Status(ctx context.Context, req *connect.Request[dotfilesdv1.StatusRequest]) (*connect.Response[dotfilesdv1.StatusResponse], error) {
	slog.Log(ctx, levelTrace, "Dotfiles.Status", "request", req.Msg)
	s.sessions.Resolve(GetSessionID(req))

	home := os.Getenv("HOME")
	hostname, _ := os.Hostname()
	uptimeRaw, _ := runCmd("uptime", "-p")

	gitBranch, _ := runCmd("git", "-C", home, "rev-parse", "--abbrev-ref", "HEAD")
	gitLog, _ := runCmd("git", "-C", home, "log", "--oneline", "-1")
	gitStatus, _ := runCmd("git", "-C", home, "status", "--porcelain")

	gitClean := strings.TrimSpace(gitStatus) == ""
	gitBranch = strings.TrimSpace(gitBranch)
	gitLog = strings.TrimSpace(gitLog)

	resp := connect.NewResponse(&dotfilesdv1.StatusResponse{
		GitClean:   gitClean,
		GitBranch:  gitBranch,
		LastCommit: gitLog,
		Uptime:     strings.TrimSpace(uptimeRaw),
		Hostname:   strings.TrimSpace(hostname),
	})

	slog.Log(ctx, levelTrace, "Dotfiles.Status done", "response", resp.Msg)
	return resp, nil
}

func (s *dotfilesServer) Git(ctx context.Context, req *connect.Request[dotfilesdv1.GitRequest]) (*connect.Response[dotfilesdv1.GitResponse], error) {
	slog.Log(ctx, levelTrace, "Dotfiles.Git", "action", req.Msg.Action, "paths", req.Msg.Paths)
	s.sessions.Resolve(GetSessionID(req))

	home := os.Getenv("HOME")
	action := req.Msg.Action

	var args []string
	switch action {
	case dotfilesdv1.GitAction_GIT_ACTION_STATUS:
		args = []string{"-C", home, "status"}
	case dotfilesdv1.GitAction_GIT_ACTION_DIFF:
		args = []string{"-C", home, "diff"}
	case dotfilesdv1.GitAction_GIT_ACTION_ADD:
		if req.Msg.Paths != "" {
			args = append([]string{"-C", home, "add"}, strings.Fields(req.Msg.Paths)...)
		} else {
			args = []string{"-C", home, "add", "-A"}
		}
	case dotfilesdv1.GitAction_GIT_ACTION_COMMIT:
		if req.Msg.Message == "" {
			resp := connect.NewResponse(&dotfilesdv1.GitResponse{ExitCode: 1, Stderr: "commit message required"})
			slog.Log(ctx, levelTrace, "Dotfiles.Git done", "response", resp.Msg)
			return resp, nil
		}
		args = []string{"-C", home, "commit", "-m", req.Msg.Message}
	case dotfilesdv1.GitAction_GIT_ACTION_PUSH:
		args = []string{"-C", home, "push"}
	case dotfilesdv1.GitAction_GIT_ACTION_LOG:
		args = []string{"-C", home, "log", "--oneline", "-10"}
	default:
		resp := connect.NewResponse(&dotfilesdv1.GitResponse{
			ExitCode: 1,
			Stderr:   fmt.Sprintf("unknown action: %s", action),
		})
		slog.Log(ctx, levelTrace, "Dotfiles.Git done", "response", resp.Msg)
		return resp, nil
	}

	stdout, stderr, code := runCmdFull("git", args...)
	resp := connect.NewResponse(&dotfilesdv1.GitResponse{
		ExitCode: int32(code),
		Stdout:   stdout,
		Stderr:   stderr,
	})

	slog.Log(ctx, levelTrace, "Dotfiles.Git done", "action", action, "exit_code", code, "stderr_truncated", truncate(stderr, 200))
	return resp, nil
}
