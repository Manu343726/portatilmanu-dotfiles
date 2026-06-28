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

// activePluginCall tracks a bidi streaming plugin execution call.
// The daemon matches LogService output from plugins to the correct
// client stream using client_id.
type activePluginCall struct {
	clientID   string
	pluginName string
	stdout     func([]byte) // sends stdout chunk back to client
	stderr     func([]byte) // sends stderr chunk back to client
}

var (
	activeCallsMu sync.RWMutex
	activeCallsByClient = make(map[string]*activePluginCall)
	activeCallsByPlugin = make(map[string]*activePluginCall)
)

func registerCall(clientID, pluginName string, stdoutFn, stderrFn func([]byte)) *activePluginCall {
	call := &activePluginCall{
		clientID:   clientID,
		pluginName: pluginName,
		stdout:     stdoutFn,
		stderr:     stderrFn,
	}
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

// PushPluginOutput is called by LogService handler when a plugin writes
// stdout/stderr. It forwards the output to the client's bidi stream.
func PushPluginOutput(pluginName, source, line, clientID string) {
	activeCallsMu.RLock()
	defer activeCallsMu.RUnlock()

	// Try matching by plugin name first, then by client ID.
	var call *activePluginCall
	if cid, ok := activeCallsByPlugin[pluginName]; ok {
		call = cid
	} else if c, ok := activeCallsByClient[clientID]; ok {
		call = c
	}
	if call == nil {
		return
	}

	if strings.HasSuffix(source, "/stdout") {
		call.stdout([]byte(line + "\n"))
	} else if strings.HasSuffix(source, "/stderr") {
		call.stderr([]byte(line + "\n"))
	}
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
	// 1. Receive the request header from the client.
	req, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("receive request: %w", err)
	}

	pluginName := req.PluginName
	svcName := req.Service
	methodName := req.Method
	reqBody := req.RequestBody
	clientID := req.ClientId

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

	// 2. Register this call so PushPluginOutput can route to us.
	registerCall(clientID, pluginName,
		func(chunk []byte) {
			stream.Send(&dotfilesdv1.CallPluginMessage{StdoutChunk: chunk})
		},
		func(chunk []byte) {
			stream.Send(&dotfilesdv1.CallPluginMessage{StderrChunk: chunk})
		},
	)
	defer unregisterCall(clientID, pluginName)

	// 3. Forward stdin chunks from client to plugin (in background).
	//    The plugin receives stdin through a separate mechanism, so we
	//    just drain stdin chunks if the client sends any.
	go func() {
		for {
			msg, err := stream.Receive()
			if err != nil {
				return
			}
			_ = msg // stdin chunks could be processed here
		}
	}()

	// 4. Make the HTTP call to the plugin.
	rpcURL := fmt.Sprintf("%s/%s/%s", info.URL, svcName, methodName)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(reqBody))
	if err != nil {
		return stream.Send(&dotfilesdv1.CallPluginMessage{
			Error: fmt.Sprintf("create request: %v", err),
		})
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Dotfiles-Context-Token", s.daemon.pluginToken)
	httpReq.Header.Set("X-Client-ID", clientID)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return stream.Send(&dotfilesdv1.CallPluginMessage{
			Error: fmt.Sprintf("plugin call: %v", err),
		})
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return stream.Send(&dotfilesdv1.CallPluginMessage{
			Error: fmt.Sprintf("read response: %v", err),
		})
	}
	if resp.StatusCode >= 400 {
		return stream.Send(&dotfilesdv1.CallPluginMessage{
			Error: fmt.Sprintf("plugin returned HTTP %d: %s", resp.StatusCode, string(respBody)),
		})
	}

	// 5. Send final response.
	return stream.Send(&dotfilesdv1.CallPluginMessage{
		ResponseBody: respBody,
	})
}

