package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// ioServer implements the IOService RPC.
// Both CLI tools and plugins use this service to submit structured log
// entries to the daemon's logging system.
type ioServer struct {
	daemon *Daemon
}

func newIOServer(daemon *Daemon) *ioServer {
	return &ioServer{daemon: daemon}
}

func (s *ioServer) Log(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.LogRequest],
) (*connect.Response[dotfilesdv1.LogResponse], error) {
	slog.Log(ctx, levelTrace, "IOService.Log", "source", req.Msg.Source)

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

func (s *ioServer) ReadStdin(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.StdinRequest],
) (*connect.Response[dotfilesdv1.StdinResponse], error) {
	clientID := req.Msg.ClientId
	if clientID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("client_id is required"))
	}

	data, eof := ReadStdinFromCall(clientID, int(req.Msg.MaxBytes))
	return connect.NewResponse(&dotfilesdv1.StdinResponse{
		Data: data,
		Eof:  eof,
	}), nil
}

// TtySession opens a raw bidirectional TTY stream between a plugin and the
// CLI's terminal via the executor bidi stream. Unlike Log (line-buffered),
// every byte is delivered immediately — suitable for tview/tcell.
//
// Protocol:
//  1. Plugin sends the first TtyPacket containing its client_id.
//  2. Daemon reads stdin from the executor buffer and sends TtyPacket.Data
//     to the plugin.
//  3. Plugin sends stdout TtyPacket.Data to the daemon, which forwards to
//     the executor's stdout channel (reaching the CLI's terminal).
//  4. Either side signals EOF to close.
func (s *ioServer) TtySession(
	ctx context.Context,
	stream *connect.BidiStream[dotfilesdv1.TtyPacket, dotfilesdv1.TtyPacket],
) error {
	first, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("receive first tty packet: %w", err)
	}
	clientID := first.ClientId
	if clientID == "" {
		return fmt.Errorf("client_id is required in first TtyPacket")
	}

	// Use a done channel to signal the stdin goroutine when the handler
	// returns. Without this, the goroutine may call stream.Send() after
	// the Connect handler has finished, causing a panic.
	done := make(chan struct{})

	// Forward stdin from executor buffer → plugin via stream.
	// Also forward pending resize events (sent by CLI via executor WindowSize).
	go func() {
		// Log panics with full stack trace before crashing. The recover
		// captures the value, logs it with a stack trace, then re-panics
		// so the daemon crashes with the original panic value.
		defer func() {
			if rec := recover(); rec != nil {
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, false)
				slog.Error("panic in TtySession stdin goroutine",
					"client_id", clientID,
					"panic", rec,
					"stack", string(buf[:n]),
				)
				panic(rec)
			}
		}()

		for {
			select {
			case <-done:
				return
			default:
			}

			// Check for pending resize first; the CLI's SIGWINCH handler
			// sends WindowSize through the executor, which is stored via
			// StoreResize. We pick it up here and forward as TtyPacket.
			if w, h, ok := ReadResizeFromCall(clientID); ok {
				if err := stream.Send(&dotfilesdv1.TtyPacket{
					Data:         nil,
					WindowWidth:  w,
					WindowHeight: h,
				}); err != nil {
					return
				}
				continue
			}

			data, eof := ReadStdinFromCall(clientID, 4096)
			if len(data) > 0 {
				if err := stream.Send(&dotfilesdv1.TtyPacket{Data: data}); err != nil {
					return
				}
			}
			if eof {
				return
			}
		}
	}()

	// Forward stdout from plugin → executor stdout channel → CLI.
	defer close(done)
	for {
		pkt, err := stream.Receive()
		if err != nil {
			return err
		}
		if pkt.Eof {
			return nil
		}
		if len(pkt.Data) == 0 {
			continue
		}

		activeCallsMu.RLock()
		call := activeCallsByClient[clientID]
		activeCallsMu.RUnlock()
		if call != nil {
			select {
			case call.stdoutChan <- pkt.Data:
			default:
				select {
				case <-call.stdoutChan:
				default:
				}
				select {
				case call.stdoutChan <- pkt.Data:
				default:
				}
			}
		}
	}
}
