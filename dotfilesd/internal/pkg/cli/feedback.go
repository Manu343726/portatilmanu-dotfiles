package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
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
		raw, err := mcpBridge.SendRequest("feedback/input_request", map[string]any{
			"prompt":  req.Msg.Prompt,
			"default": req.Msg.Default,
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("MCP input request: %w", err))
		}
		var rpcResp struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(raw, &rpcResp); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parse MCP response: %w", err))
		}
		var pbResp dotfilesdv1.InputResponse
		if err := json.Unmarshal(rpcResp.Result, &pbResp); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parse InputResponse: %w", err))
		}
		return connect.NewResponse(&pbResp), nil
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

type confirmHandler struct {
	dotfilesdv1connect.UnimplementedConfirmServiceHandler
	handler func(context.Context, *dotfilesdv1.ConfirmRequest) (bool, error)
}

func (h *confirmHandler) RequestConfirm(ctx context.Context, req *connect.Request[dotfilesdv1.ConfirmRequest]) (*connect.Response[dotfilesdv1.ConfirmResponse], error) {
	slog.Debug("confirm requested", "message", req.Msg.Message, "default", req.Msg.DefaultConfirm)

	if mcpBridge != nil {
		raw, err := mcpBridge.SendRequest("feedback/confirm", map[string]any{
			"message":         req.Msg.Message,
			"default_confirm": req.Msg.DefaultConfirm,
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("MCP confirm request: %w", err))
		}
		var rpcResp struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(raw, &rpcResp); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parse MCP response: %w", err))
		}
		var pbResp dotfilesdv1.ConfirmResponse
		if err := json.Unmarshal(rpcResp.Result, &pbResp); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parse ConfirmResponse: %w", err))
		}
		return connect.NewResponse(&pbResp), nil
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
