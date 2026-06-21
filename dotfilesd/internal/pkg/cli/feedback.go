package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
)

type FeedbackServer struct {
	server     *http.Server
	port       int
	inputSvc   *inputHandler
	confirmSvc *confirmHandler
}

func NewFeedbackServer() (*FeedbackServer, error) {
	mux := http.NewServeMux()

	inputSvc := &inputHandler{}
	p, h := dotfilesdv1connect.NewInputServiceHandler(inputSvc)
	mux.Handle(p, h)

	confirmSvc := &confirmHandler{}
	p, h = dotfilesdv1connect.NewConfirmServiceHandler(confirmSvc)
	mux.Handle(p, h)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("feedback listen: %w", err)
	}

	fs := &FeedbackServer{
		server:     &http.Server{Handler: mux},
		port:       listener.Addr().(*net.TCPAddr).Port,
		inputSvc:   inputSvc,
		confirmSvc: confirmSvc,
	}

	go func() {
		slog.Debug("feedback server listening", "port", fs.port)
		if err := fs.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("feedback server error", "error", err)
		}
	}()

	return fs, nil
}

func (fs *FeedbackServer) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", fs.port)
}

func (fs *FeedbackServer) Close() error {
	return fs.server.Close()
}

func (fs *FeedbackServer) SetInputHandler(fn func(context.Context, *dotfilesdv1.InputRequest) (string, error)) {
	fs.inputSvc.handler = fn
}

func (fs *FeedbackServer) SetConfirmHandler(fn func(context.Context, *dotfilesdv1.ConfirmRequest) (bool, error)) {
	fs.confirmSvc.handler = fn
}

type inputHandler struct {
	dotfilesdv1connect.UnimplementedInputServiceHandler
	handler func(context.Context, *dotfilesdv1.InputRequest) (string, error)
}

func (h *inputHandler) RequestInput(ctx context.Context, req *connect.Request[dotfilesdv1.InputRequest]) (*connect.Response[dotfilesdv1.InputResponse], error) {
	slog.Debug("input requested", "prompt", req.Msg.Prompt, "default", req.Msg.Default)

	if mcpBridge != nil {
		if req.Msg.Sensitive {
			return nil, connect.NewError(connect.CodeUnimplemented,
				fmt.Errorf("sensitive input not available via MCP elicitation form mode; use exec_run with password field instead"))
		}
		if !clientCaps.hasElicitation {
			return nil, connect.NewError(connect.CodeUnavailable,
				fmt.Errorf("MCP client does not support elicitation"))
		}

		raw, err := mcpBridge.SendRequest("elicitation/create", map[string]any{
			"message": req.Msg.Prompt,
			"mode":    "form",
			"requestedSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"input": map[string]any{
						"type":        "string",
						"description": req.Msg.Prompt,
						"default":     req.Msg.Default,
					},
				},
				"required": []string{"input"},
			},
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("MCP elicitation request: %w", err))
		}

		pbResp, err := parseElicitationInputResponse(raw)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(pbResp), nil
	}

	if h.handler == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("input handler not set"))
	}
	value, err := h.handler(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	resp := connect.NewResponse(&dotfilesdv1.InputResponse{Value: value})
	resp.Header().Set("Session-Id", req.Msg.SessionId)
	return resp, nil
}

// parseElicitationInputResponse parses the JSON-RPC response from an
// elicitation/create form request and maps the standard elicitation response
// (action + content) into an InputResponse protobuf.
func parseElicitationInputResponse(raw json.RawMessage) (*dotfilesdv1.InputResponse, error) {
	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parse MCP response: %w", err))
	}
	if rpcResp.Error != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("MCP elicitation rejected by client: %s", rpcResp.Error.Message))
	}
	if rpcResp.Result == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("MCP response missing result and error"))
	}

	var elicitationResp struct {
		Action  string          `json:"action"`
		Content json.RawMessage `json:"content,omitempty"`
	}
	if err := json.Unmarshal(rpcResp.Result, &elicitationResp); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parse elicitation response: %w", err))
	}

	switch elicitationResp.Action {
	case "accept":
		var content struct {
			Input string `json:"input"`
		}
		if err := json.Unmarshal(elicitationResp.Content, &content); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parse elicitation content: %w", err))
		}
		return &dotfilesdv1.InputResponse{Value: content.Input}, nil
	case "decline":
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("user declined input request"))
	case "cancel":
		return nil, connect.NewError(connect.CodeCanceled, fmt.Errorf("user cancelled input request"))
	default:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("unknown elicitation action: %s", elicitationResp.Action))
	}
}

type confirmHandler struct {
	dotfilesdv1connect.UnimplementedConfirmServiceHandler
	handler func(context.Context, *dotfilesdv1.ConfirmRequest) (bool, error)
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (h *confirmHandler) RequestConfirm(ctx context.Context, req *connect.Request[dotfilesdv1.ConfirmRequest]) (*connect.Response[dotfilesdv1.ConfirmResponse], error) {
	slog.Debug("confirm requested", "message", req.Msg.Message, "default", req.Msg.DefaultConfirm)

	if mcpBridge != nil {
		if !clientCaps.hasElicitation {
			return nil, connect.NewError(connect.CodeUnavailable,
				fmt.Errorf("MCP client does not support elicitation"))
		}

		raw, err := mcpBridge.SendRequest("elicitation/create", map[string]any{
			"message": req.Msg.Message,
			"mode":    "form",
			"requestedSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"confirmed": map[string]any{
						"type":        "boolean",
						"description": "Confirm action",
						"default":     req.Msg.DefaultConfirm,
					},
				},
				"required": []string{"confirmed"},
			},
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("MCP elicitation request: %w", err))
		}

		pbResp, err := parseElicitationConfirmResponse(raw)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(pbResp), nil
	}

	if h.handler == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("confirm handler not set"))
	}
	confirmed, err := h.handler(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	resp := connect.NewResponse(&dotfilesdv1.ConfirmResponse{Confirmed: confirmed})
	resp.Header().Set("Session-Id", req.Msg.SessionId)
	return resp, nil
}

// parseElicitationConfirmResponse parses the JSON-RPC response from an
// elicitation/create form request and maps the standard elicitation response
// (action + content) into a ConfirmResponse protobuf.
func parseElicitationConfirmResponse(raw json.RawMessage) (*dotfilesdv1.ConfirmResponse, error) {
	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parse MCP response: %w", err))
	}
	if rpcResp.Error != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("MCP elicitation rejected by client: %s", rpcResp.Error.Message))
	}
	if rpcResp.Result == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("MCP response missing result and error"))
	}

	var elicitationResp struct {
		Action  string          `json:"action"`
		Content json.RawMessage `json:"content,omitempty"`
	}
	if err := json.Unmarshal(rpcResp.Result, &elicitationResp); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parse elicitation response: %w", err))
	}

	switch elicitationResp.Action {
	case "accept":
		var content struct {
			Confirmed bool `json:"confirmed"`
		}
		if err := json.Unmarshal(elicitationResp.Content, &content); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parse elicitation content: %w", err))
		}
		return &dotfilesdv1.ConfirmResponse{Confirmed: content.Confirmed}, nil
	case "decline":
		// Explicit "no" — return false without error.
		return &dotfilesdv1.ConfirmResponse{Confirmed: false}, nil
	case "cancel":
		return nil, connect.NewError(connect.CodeCanceled, fmt.Errorf("user cancelled confirm request"))
	default:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("unknown elicitation action: %s", elicitationResp.Action))
	}
}
