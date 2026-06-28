package daemon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// activePluginCall tracks a streaming plugin execution call.
type activePluginCall struct {
	pluginName string
	stdoutCh   chan []byte
	stderrCh   chan []byte
}

var (
	activeCallsMu sync.RWMutex
	activeCalls   = make(map[string]*activePluginCall) // callID → call
)

// registerPluginCall registers a streaming call and returns a call ID.
func registerPluginCall(pluginName string) (string, *activePluginCall) {
	id := generateCallID()
	call := &activePluginCall{
		pluginName: pluginName,
		stdoutCh:   make(chan []byte, 128),
		stderrCh:   make(chan []byte, 128),
	}
	activeCallsMu.Lock()
	activeCalls[id] = call
	activeCallsMu.Unlock()
	return id, call
}

// unregisterPluginCall removes a streaming call from the active map.
func unregisterPluginCall(id string) {
	activeCallsMu.Lock()
	delete(activeCalls, id)
	activeCallsMu.Unlock()
}

// getPluginCall looks up an active streaming call by ID.
func getPluginCall(id string) *activePluginCall {
	activeCallsMu.RLock()
	defer activeCallsMu.RUnlock()
	return activeCalls[id]
}

// PluginOutputChunk is sent from the LogService handler when it receives
// a log entry from a plugin that has an active CallPlugin call.
func PushPluginOutput(pluginName, source, line string) {
	activeCallsMu.RLock()
	defer activeCallsMu.RUnlock()

	for _, call := range activeCalls {
		if call.pluginName != pluginName {
			continue
		}
		chunk := []byte(line + "\n")
		if strings.HasSuffix(source, "/stdout") {
			select {
			case call.stdoutCh <- chunk:
			default:
			}
		} else if strings.HasSuffix(source, "/stderr") {
			select {
			case call.stderrCh <- chunk:
			default:
			}
		}
	}
}

// generateCallID creates a unique call ID.
var callIDCounter int64
var callIDMu sync.Mutex

func generateCallID() string {
	callIDMu.Lock()
	callIDCounter++
	id := fmt.Sprintf("call_%d", callIDCounter)
	callIDMu.Unlock()
	return id
}

// executorServer implements PluginExecutorService.
type executorServer struct {
	daemon *Daemon
}

func newExecutorServer(d *Daemon) *executorServer {
	return &executorServer{daemon: d}
}

// CallPlugin implements the bidi streaming CallPlugin RPC.
// The CLI sends the request metadata, then the daemon streams back
// stdout/stderr chunks from the plugin followed by the final response.
func (s *executorServer) CallPlugin(
	ctx context.Context,
	stream *connect.BidiStream[dotfilesdv1.CallPluginMessage, dotfilesdv1.CallPluginMessage],
) error {
	// Receive the request from the CLI.
	req, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("receive request: %w", err)
	}

	pluginName := req.PluginName
	svcName := req.Service
	methodName := req.Method
	reqBody := req.RequestBody

	if pluginName == "" || svcName == "" || methodName == "" {
		return stream.Send(&dotfilesdv1.CallPluginMessage{
			Error: "plugin_name, service, and method are required",
		})
	}

	// Look up plugin info from the manager.
	if s.daemon.pluginMgr == nil {
		return stream.Send(&dotfilesdv1.CallPluginMessage{
			Error: "plugin manager not available",
		})
	}
	info, ok := s.daemon.pluginMgr.GetPlugin(pluginName)
	if !ok {
		return stream.Send(&dotfilesdv1.CallPluginMessage{
			Error: fmt.Sprintf("plugin %q not found", pluginName),
		})
	}

	// Register active call for output forwarding.
	callID, call := registerPluginCall(pluginName)
	defer unregisterPluginCall(callID)

	// Build the RPC URL to the plugin.
	rpcURL := fmt.Sprintf("%s/%s/%s", info.URL, svcName, methodName)

	// Make the HTTP request to the plugin.
	httpReq, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(reqBody))
	if err != nil {
		return stream.Send(&dotfilesdv1.CallPluginMessage{
			Error: fmt.Sprintf("create request: %v", err),
		})
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Dotfiles-Context-Token", s.daemon.pluginToken)
	httpReq.Header.Set("X-Plugin-Call-ID", callID)

	// Stream output chunks while the HTTP request is in-flight.
	type httpResult struct {
		resp *http.Response
		err  error
	}
	resultCh := make(chan httpResult, 1)
	go func() {
		resp, err := http.DefaultClient.Do(httpReq)
		resultCh <- httpResult{resp, err}
	}()

	// Loop: send stdout/stderr chunks until done, then send response.
	for {
		select {
		case chunk := <-call.stdoutCh:
			if err := stream.Send(&dotfilesdv1.CallPluginMessage{StdoutChunk: chunk}); err != nil {
				return err
			}
		case chunk := <-call.stderrCh:
			if err := stream.Send(&dotfilesdv1.CallPluginMessage{StderrChunk: chunk}); err != nil {
				return err
			}
		case result := <-resultCh:
			// HTTP request completed. Drain remaining output.
			if result.err != nil {
				return stream.Send(&dotfilesdv1.CallPluginMessage{
					Error: fmt.Sprintf("plugin call: %v", result.err),
				})
			}
			defer result.resp.Body.Close()

			respBody, err := io.ReadAll(result.resp.Body)
			if err != nil {
				return stream.Send(&dotfilesdv1.CallPluginMessage{
					Error: fmt.Sprintf("read response: %v", err),
				})
			}
			if result.resp.StatusCode >= 400 {
				return stream.Send(&dotfilesdv1.CallPluginMessage{
					Error: fmt.Sprintf("plugin returned HTTP %d: %s", result.resp.StatusCode, string(respBody)),
				})
			}

			// Flush any remaining output chunks.
		drainLoop:
			for {
				select {
				case chunk := <-call.stdoutCh:
					if err := stream.Send(&dotfilesdv1.CallPluginMessage{StdoutChunk: chunk}); err != nil {
						return err
					}
				case chunk := <-call.stderrCh:
					if err := stream.Send(&dotfilesdv1.CallPluginMessage{StderrChunk: chunk}); err != nil {
						return err
					}
				default:
					break drainLoop
				}
			}

			return stream.Send(&dotfilesdv1.CallPluginMessage{
				ResponseBody: respBody,
			})
		}
	}
}
