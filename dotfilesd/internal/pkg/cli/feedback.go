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
	chooseSvc  *chooseHandler
}

func NewFeedbackServer() (*FeedbackServer, error) {
	mux := http.NewServeMux()

	inputSvc := &inputHandler{}
	p, h := dotfilesdv1connect.NewInputServiceHandler(inputSvc)
	mux.Handle(p, h)

	confirmSvc := &confirmHandler{}
	p, h = dotfilesdv1connect.NewConfirmServiceHandler(confirmSvc)
	mux.Handle(p, h)

	chooseSvc := &chooseHandler{}
	p, h = dotfilesdv1connect.NewChooseServiceHandler(chooseSvc)
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
		chooseSvc:  chooseSvc,
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

func (fs *FeedbackServer) SetChooseHandler(fn func(context.Context, *dotfilesdv1.ChooseRequest) (int, string, error)) {
	fs.chooseSvc.handler = fn
}

type inputHandler struct {
	dotfilesdv1connect.UnimplementedInputServiceHandler
	handler func(context.Context, *dotfilesdv1.InputRequest) (string, error)
}

func (h *inputHandler) RequestInput(ctx context.Context, req *connect.Request[dotfilesdv1.InputRequest]) (*connect.Response[dotfilesdv1.InputResponse], error) {
	slog.Debug("input requested", "prompt", req.Msg.Prompt, "default", req.Msg.Default)

	if mcpBridge != nil {
		if !clientCaps.hasElicitation {
			return nil, connect.NewError(connect.CodeUnavailable,
				fmt.Errorf("MCP client does not support elicitation"))
		}

		inputSchema := map[string]any{
			"type":        "string",
			"description": req.Msg.Prompt,
			"default":     req.Msg.Default,
		}
		if req.Msg.Sensitive {
			// Hint to the client to render a masked password field.
			inputSchema["format"] = "password"
			inputSchema["writeOnly"] = true
		}

		raw, err := mcpBridge.SendRequest("elicitation/create", map[string]any{
			"message": req.Msg.Prompt,
			"mode":    "form",
			"requestedSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"input": inputSchema,
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
	if sm := req.Msg.GetSession(); sm != nil {
		resp.Header().Set("Session-Id", sm.GetId())
	}
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
	if sm := req.Msg.GetSession(); sm != nil {
		resp.Header().Set("Session-Id", sm.GetId())
	}
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

type chooseHandler struct {
	dotfilesdv1connect.UnimplementedChooseServiceHandler
	handler func(context.Context, *dotfilesdv1.ChooseRequest) (int, string, error)
}

func (h *chooseHandler) RequestChoose(ctx context.Context, req *connect.Request[dotfilesdv1.ChooseRequest]) (*connect.Response[dotfilesdv1.ChooseResponse], error) {
	slog.Debug("choose requested", "prompt", req.Msg.Prompt, "options", req.Msg.Options, "default", req.Msg.DefaultIndex)

	if mcpBridge != nil {
		if !clientCaps.hasElicitation {
			return nil, connect.NewError(connect.CodeUnavailable,
				fmt.Errorf("MCP client does not support elicitation"))
		}

		// Build enum items with titles for richer display.
		oneOf := make([]map[string]any, len(req.Msg.Options))
		for i, opt := range req.Msg.Options {
			oneOf[i] = map[string]any{
				"const": opt,
				"title": opt,
			}
		}

		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"option": map[string]any{
					"type":        "string",
					"title":       "Select an option",
					"description": req.Msg.Prompt,
					"oneOf":       oneOf,
				},
			},
			"required": []string{"option"},
		}
		// Set default if valid index provided.
		if req.Msg.DefaultIndex >= 0 && int(req.Msg.DefaultIndex) < len(req.Msg.Options) {
			schema["properties"].(map[string]any)["option"].(map[string]any)["default"] = req.Msg.Options[req.Msg.DefaultIndex]
		}

		raw, err := mcpBridge.SendRequest("elicitation/create", map[string]any{
			"message":         req.Msg.Prompt,
			"mode":            "form",
			"requestedSchema": schema,
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("MCP elicitation request: %w", err))
		}

		pbResp, err := parseElicitationChooseResponse(raw, req.Msg.Options)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(pbResp), nil
	}

	if h.handler == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("choose handler not set"))
	}
	idx, option, err := h.handler(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	resp := connect.NewResponse(&dotfilesdv1.ChooseResponse{SelectedIndex: int32(idx), SelectedOption: option})
	if sm := req.Msg.GetSession(); sm != nil {
		resp.Header().Set("Session-Id", sm.GetId())
	}
	return resp, nil
}

// parseElicitationChooseResponse parses the JSON-RPC response from an
// elicitation/create form request and maps the standard elicitation response
// (action + content) into a ChooseResponse protobuf.
func parseElicitationChooseResponse(raw json.RawMessage, options []string) (*dotfilesdv1.ChooseResponse, error) {
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
			Option string `json:"option"`
		}
		if err := json.Unmarshal(elicitationResp.Content, &content); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parse elicitation content: %w", err))
		}
		// Find the index of the selected option.
		idx := -1
		for i, opt := range options {
			if opt == content.Option {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("selected option %q not in options list", content.Option))
		}
		return &dotfilesdv1.ChooseResponse{SelectedIndex: int32(idx), SelectedOption: content.Option}, nil
	case "decline":
		return &dotfilesdv1.ChooseResponse{SelectedIndex: -1, SelectedOption: ""}, nil
	case "cancel":
		return nil, connect.NewError(connect.CodeCanceled, fmt.Errorf("user cancelled choose request"))
	default:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("unknown elicitation action: %s", elicitationResp.Action))
	}
}
