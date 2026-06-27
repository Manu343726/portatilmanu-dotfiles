package daemon

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

type dotfilesServer struct {
	sessions *SessionStore
}

func (s *dotfilesServer) Status(ctx context.Context, req *connect.Request[dotfilesdv1.StatusRequest]) (*connect.Response[dotfilesdv1.StatusResponse], error) {
	slog.Log(ctx, levelTrace, "Dotfiles.Status", "request", req.Msg)
	s.sessions.ResolveSession(req.Msg.GetSession())

	home := os.Getenv("HOME")

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
	})

	slog.Log(ctx, levelTrace, "Dotfiles.Status done", "response", resp.Msg)
	return resp, nil
}
