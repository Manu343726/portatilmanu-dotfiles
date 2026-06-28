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

// activePluginCall tracks a plugin execution call for output capture.
type activePluginCall struct {
	pluginName string
	stdoutBuf  strings.Builder
	stderrBuf  strings.Builder
	mu         sync.Mutex
}

var (
	activeCallsMu sync.RWMutex
	activeCalls   = make(map[string]*activePluginCall)
)

func registerPluginCall(pluginName string) (string, *activePluginCall) {
	id := generateCallID()
	call := &activePluginCall{pluginName: pluginName}
	activeCallsMu.Lock()
	activeCalls[id] = call
	activeCallsMu.Unlock()
	return id, call
}

func unregisterPluginCall(id string) {
	activeCallsMu.Lock()
	delete(activeCalls, id)
	activeCallsMu.Unlock()
}

// PushPluginOutput is called by the LogService handler. It buffers
// plugin stdout/stderr so the executor can return it in responses.
func PushPluginOutput(pluginName, source, line string) {
	activeCallsMu.RLock()
	defer activeCallsMu.RUnlock()

	for _, call := range activeCalls {
		if call.pluginName != pluginName {
			continue
		}
		call.mu.Lock()
		if strings.HasSuffix(source, "/stdout") {
			call.stdoutBuf.WriteString(line)
			call.stdoutBuf.WriteByte('\n')
		} else if strings.HasSuffix(source, "/stderr") {
			call.stderrBuf.WriteString(line)
			call.stderrBuf.WriteByte('\n')
		}
		call.mu.Unlock()
	}
}

var callIDCounter int64
var callIDMu sync.Mutex

func generateCallID() string {
	callIDMu.Lock()
	callIDCounter++
	id := fmt.Sprintf("call_%d", callIDCounter)
	callIDMu.Unlock()
	return id
}

type executorServer struct {
	daemon *Daemon
}

func newExecutorServer(d *Daemon) *executorServer {
	return &executorServer{daemon: d}
}

func (s *executorServer) CallPlugin(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.CallPluginRequest],
) (*connect.Response[dotfilesdv1.CallPluginResponse], error) {
	pluginName := req.Msg.PluginName
	svcName := req.Msg.Service
	methodName := req.Msg.Method
	reqBody := req.Msg.RequestBody

	if pluginName == "" || svcName == "" || methodName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("plugin_name, service, and method are required"))
	}

	info, ok := s.daemon.pluginMgr.GetPlugin(pluginName)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("plugin %q not found", pluginName))
	}

	// Make the HTTP call to the plugin.
	rpcURL := fmt.Sprintf("%s/%s/%s", info.URL, svcName, methodName)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Dotfiles-Context-Token", s.daemon.pluginToken)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("plugin call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("plugin returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Return response (captured stdout/stderr is logged by LogService).
	return connect.NewResponse(&dotfilesdv1.CallPluginResponse{
		ResponseBody: respBody,
	}), nil
}
