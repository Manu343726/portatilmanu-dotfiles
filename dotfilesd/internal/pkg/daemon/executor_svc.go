package daemon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

type activePluginCall struct {
	clientID   string
	pluginName string
	service    string
	method     string
	stdoutChan chan []byte
	stderrChan chan []byte
	stdinBuf   bytes.Buffer
	stdinEOF   bool
	mu         sync.Mutex
	stdinCond  *sync.Cond
	done       chan struct{}
}

var (
	activeCallsMu       sync.RWMutex
	activeCallsByClient = make(map[string]*activePluginCall)
	activeCallsByPlugin = make(map[string]*activePluginCall)
)

func registerCall(clientID, pluginName, service, method string) *activePluginCall {
	call := &activePluginCall{
		clientID:   clientID,
		pluginName: pluginName,
		service:    service,
		method:     method,
		stdoutChan: make(chan []byte, 256),
		stderrChan: make(chan []byte, 256),
		done:       make(chan struct{}),
	}
	call.stdinCond = sync.NewCond(&call.mu)
	activeCallsMu.Lock()
	activeCallsByClient[clientID] = call
	activeCallsByPlugin[pluginName] = call
	activeCallsMu.Unlock()
	return call
}

func unregisterCall(clientID, pluginName string) {
	activeCallsMu.Lock()
	delete(activeCallsByClient, clientID)
	delete(activeCallsByPlugin, pluginName)
	activeCallsMu.Unlock()
}

func PushPluginOutput(pluginName, source, line, clientID string) {
	activeCallsMu.RLock()
	defer activeCallsMu.RUnlock()

	var call *activePluginCall
	if cid, ok := activeCallsByPlugin[pluginName]; ok {
		call = cid
	} else if c, ok := activeCallsByClient[clientID]; ok {
		call = c
	}
	if call == nil {
		return
	}

	chunk := []byte(line + "\n")
	if strings.HasSuffix(source, "/stdout") {
		select {
		case call.stdoutChan <- chunk:
		default:
			select {
			case <-call.stdoutChan:
			default:
			}
			select {
			case call.stdoutChan <- chunk:
			default:
			}
		}
	} else if strings.HasSuffix(source, "/stderr") {
		select {
		case call.stderrChan <- chunk:
		default:
			select {
			case <-call.stderrChan:
			default:
			}
			select {
			case call.stderrChan <- chunk:
			default:
			}
		}
	}
}

// StoreStdin stores stdin data from the executor bidi stream for a call.
func StoreStdin(clientID string, data []byte) {
	activeCallsMu.RLock()
	call := activeCallsByClient[clientID]
	activeCallsMu.RUnlock()
	if call == nil {
		return
	}
	call.mu.Lock()
	call.stdinBuf.Write(data)
	call.mu.Unlock()
	call.stdinCond.Broadcast()
}

// CloseStdin marks stdin as closed for a call.
func CloseStdin(clientID string) {
	activeCallsMu.RLock()
	call := activeCallsByClient[clientID]
	activeCallsMu.RUnlock()
	if call == nil {
		return
	}
	call.mu.Lock()
	call.stdinEOF = true
	call.mu.Unlock()
	call.stdinCond.Broadcast()
}

// ReadStdinFromCall reads buffered stdin data for a call.
// Blocks until data is available or stdin is closed.
func ReadStdinFromCall(clientID string, maxBytes int) ([]byte, bool) {
	activeCallsMu.RLock()
	call := activeCallsByClient[clientID]
	activeCallsMu.RUnlock()
	if call == nil {
		return nil, true
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	for call.stdinBuf.Len() == 0 {
		if call.stdinEOF {
			return nil, true
		}
		call.stdinCond.Wait() // releases mu, blocks, reacquires mu on signal
	}
	n := maxBytes
	if n <= 0 || n > call.stdinBuf.Len() {
		n = call.stdinBuf.Len()
	}
	buf := make([]byte, n)
	call.stdinBuf.Read(buf)
	return buf, call.stdinEOF
}

type executorServer struct {
	daemon *Daemon
}

func newExecutorServer(d *Daemon) *executorServer {
	return &executorServer{daemon: d}
}

func (s *executorServer) CallPlugin(
	ctx context.Context,
	stream *connect.BidiStream[dotfilesdv1.CallPluginMessage, dotfilesdv1.CallPluginMessage],
) error {
	req, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("receive request: %w", err)
	}

	pluginName := req.PluginName
	svcName := req.Service
	methodName := req.Method
	reqBody := req.RequestBody
	clientID := req.ClientId
	renderOutput := req.RenderOutput

	if clientID == "" {
		clientID = fmt.Sprintf("cli_%d", time.Now().UnixNano())
	}
	if pluginName == "" || svcName == "" || methodName == "" {
		return stream.Send(&dotfilesdv1.CallPluginMessage{
			Error: "plugin_name, service, and method are required",
		})
	}

	info, ok := s.daemon.pluginMgr.GetPlugin(pluginName)
	if !ok {
		return stream.Send(&dotfilesdv1.CallPluginMessage{
			Error: fmt.Sprintf("plugin %q not found", pluginName),
		})
	}

	call := registerCall(clientID, pluginName, svcName, methodName)
	defer func() {
		CloseStdin(clientID)
		unregisterCall(clientID, pluginName)
	}()

	// Determine parent for the diagnostics tree: prefer the caller's session
	// when available (CLI context), fall back to the plugin.
	execParent := "plugin:" + pluginName
	if sessionID := extractSessionFromClientID(clientID); sessionID != "" {
		execParent = "session:" + sessionID
	}

	pluginSessionID := "session:plugin-" + pluginName

	// Temporarily reparent the plugin session to the caller so exec calls
	// from the plugin show under the right place. The middleware inside the
	// plugin will create a proper sub-node via plugin_rpc_open events.
	if s.daemon.diag != nil {
		s.daemon.diag.UpdateParent(pluginSessionID, execParent)
	}
	defer func() {
		// Restore the plugin session's parent back to the plugin so
		// background exec calls appear under the plugin node.
		if s.daemon.diag != nil {
			s.daemon.diag.UpdateParent(pluginSessionID, "plugin:"+pluginName)
		}
	}()

	// Stream stdin chunks from client in background — buffer for ReadStdin RPC.
	go func() {
		defer CloseStdin(clientID)
		for {
			m, err := stream.Receive()
			if err != nil {
				return
			}
			if len(m.StdinChunk) > 0 {
				StoreStdin(clientID, m.StdinChunk)
			}
		}
	}()

	// Launch HTTP call in background — plugin RPC may take time.
	type httpResult struct {
		respBody []byte
		err      string
	}
	resultCh := make(chan httpResult, 1)

	go func() {
		rpcURL := fmt.Sprintf("%s/%s/%s", info.URL, svcName, methodName)
		reqReader := bytes.NewReader(reqBody)
		httpReq, err := http.NewRequestWithContext(ctx, "POST", rpcURL, reqReader)
		if err != nil {
			resultCh <- httpResult{err: fmt.Sprintf("create request: %v", err)}
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-Dotfiles-Context-Token", s.daemon.pluginToken)
		httpReq.Header.Set("X-Client-ID", clientID)
		httpReq.Header.Set("X-Dotfiles-Diag-Parent", execParent)
		if renderOutput {
			httpReq.Header.Set("X-Dotfiles-Render-Output", "true")
		}

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			resultCh <- httpResult{err: fmt.Sprintf("plugin call: %v", err)}
			return
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			resultCh <- httpResult{err: fmt.Sprintf("read response: %v", err)}
			return
		}
		if resp.StatusCode >= 400 {
			resultCh <- httpResult{err: fmt.Sprintf("plugin returned HTTP %d: %s", resp.StatusCode, string(respBody))}
			return
		}
		resultCh <- httpResult{respBody: respBody}
	}()

	// Main loop: drain I/O chunks while HTTP runs, then send final response.
	for {
		select {
		case <-call.done:
			return nil
		case chunk := <-call.stdoutChan:
			if err := stream.Send(&dotfilesdv1.CallPluginMessage{StdoutChunk: chunk}); err != nil {
				return err
			}
		case chunk := <-call.stderrChan:
			if err := stream.Send(&dotfilesdv1.CallPluginMessage{StderrChunk: chunk}); err != nil {
				return err
			}
		case result := <-resultCh:
			close(call.done)
			if result.err != "" {
				return stream.Send(&dotfilesdv1.CallPluginMessage{Error: result.err})
			}
			// Drain any remaining I/O chunks before final response.
		drainLoop:
			for {
				select {
				case chunk := <-call.stdoutChan:
					if err := stream.Send(&dotfilesdv1.CallPluginMessage{StdoutChunk: chunk}); err != nil {
						return err
					}
				case chunk := <-call.stderrChan:
					if err := stream.Send(&dotfilesdv1.CallPluginMessage{StderrChunk: chunk}); err != nil {
						return err
					}
				default:
					break drainLoop
				}
			}
			return stream.Send(&dotfilesdv1.CallPluginMessage{ResponseBody: result.respBody})
		}
	}
}

// extractSessionFromClientID extracts a session ID from a client ID that was
// formatted as "cli_<timestamp>|ses_<id>" by the CLI. Returns empty string
// if no session is encoded (e.g. for background executor calls from plugins).
func extractSessionFromClientID(clientID string) string {
	if idx := strings.Index(clientID, "|"); idx >= 0 && idx+1 < len(clientID) {
		return clientID[idx+1:]
	}
	return ""
}
