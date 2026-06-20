package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

var mcpBridge *MCPBridge

type MCPBridge struct {
	mu      sync.Mutex
	pending map[string]chan json.RawMessage
	out     io.Writer
	idSeq   atomic.Int64
}

func NewMCPBridge(out io.Writer) *MCPBridge {
	return &MCPBridge{
		pending: make(map[string]chan json.RawMessage),
		out:     out,
	}
}

func (b *MCPBridge) SendRequest(method string, params any) (json.RawMessage, error) {
	id := fmt.Sprintf("srv_%d", b.idSeq.Add(1))
	ch := make(chan json.RawMessage, 1)

	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()

	frame := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}

	b.mu.Lock()
	writeJSONFrame(b.out, frame)
	b.mu.Unlock()

	slog.Debug("mcp bridge sent request", "id", id, "method", method)

	select {
	case data := <-ch:
		return data, nil
	case <-time.After(5 * time.Minute):
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return nil, fmt.Errorf("MCP request %s timed out", id)
	}
}

func (b *MCPBridge) HandleResponse(id string, data json.RawMessage) bool {
	b.mu.Lock()
	ch, ok := b.pending[id]
	if ok {
		delete(b.pending, id)
	}
	b.mu.Unlock()
	if !ok {
		return false
	}
	slog.Debug("mcp bridge handled response", "id", id)
	ch <- data
	return true
}

func (b *MCPBridge) WriteResp(resp *mcpResponse) {
	b.mu.Lock()
	writeMCPFrame(b.out, resp)
	b.mu.Unlock()
}

func writeJSONFrame(w io.Writer, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("json marshal frame", "error", err)
		return
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	w.Write([]byte(header))
	w.Write(data)
}
