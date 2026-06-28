package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// logServer implements the LogService usage-level RPC.
// Both CLI tools and plugins use this service to submit structured log
// entries to the daemon's logging system.
type logServer struct {
	daemon *Daemon
}

func newLogServer(daemon *Daemon) *logServer {
	return &logServer{daemon: daemon}
}

func (s *logServer) Log(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.LogRequest],
) (*connect.Response[dotfilesdv1.LogResponse], error) {
	slog.Log(ctx, levelTrace, "LogService.Log", "source", req.Msg.Source)

	entry := req.Msg.Entry
	if entry == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("log entry is required"))
	}

	// Use the daemon's logging manager with the source as the logger module.
	if s.daemon.logger != nil {
		log := s.daemon.logger.Logger(req.Msg.Source)

		kv := make([]any, 0, len(entry.Attributes)*2)
		for k, v := range entry.Attributes {
			kv = append(kv, k, v)
		}

		switch entry.Level {
		case dotfilesdv1.LogLevel_LOG_LEVEL_TRACE:
			log.Trace(entry.Message, kv...)
		case dotfilesdv1.LogLevel_LOG_LEVEL_DEBUG:
			log.Debug(entry.Message, kv...)
		case dotfilesdv1.LogLevel_LOG_LEVEL_INFO:
			log.Info(entry.Message, kv...)
		case dotfilesdv1.LogLevel_LOG_LEVEL_WARN:
			log.Warn(entry.Message, kv...)
		case dotfilesdv1.LogLevel_LOG_LEVEL_ERROR:
			log.Error(entry.Message, kv...)
		default:
			log.Info(entry.Message, kv...)
		}
	} else {
		slog.Log(ctx, logLevelToSlog(entry.Level), entry.Message, "source", req.Msg.Source, "attrs", entry.Attributes)
	}

	// If this is stdout/stderr from a plugin, forward to any active
	// CallPlugin bidi stream for that client.
	source := req.Msg.Source
	if slash := strings.LastIndexByte(source, '/'); slash > 0 {
		pluginName := source[:slash]
		suffix := source[slash:]
		if suffix == "/stdout" || suffix == "/stderr" {
			clientID := ""
			if entry.Attributes != nil {
				clientID = entry.Attributes["client_id"]
			}
			PushPluginOutput(pluginName, source, entry.Message, clientID)
		}
	}

	return connect.NewResponse(&dotfilesdv1.LogResponse{}), nil
}
