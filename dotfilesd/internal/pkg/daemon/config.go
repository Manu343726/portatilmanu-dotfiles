package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

type configServer struct {
	sessions *SessionStore
}

func (s *configServer) Reconfigure(ctx context.Context, req *connect.Request[dotfilesdv1.ReconfigureRequest]) (*connect.Response[dotfilesdv1.ReconfigureResponse], error) {
	r := req.Msg
	slog.Log(ctx, levelTrace, "Config.Reconfigure", "log_level", r.LogLevel)
	s.sessions.ResolveSession(req.Msg.GetSession())

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
	s.sessions.ResolveSession(req.Msg.GetSession())

	go restartDaemon(500 * time.Millisecond)

	return connect.NewResponse(&dotfilesdv1.RestartResponse{
		Message: "daemon restarting in 500ms, reconnect after ~3s",
	}), nil
}
