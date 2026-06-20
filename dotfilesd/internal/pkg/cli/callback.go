package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
)

// defaultFeedbackHandler is the default handler for feedback requests.
// It can be replaced by setting a custom handler via CallbackServer.SetFeedbackHandler.
var defaultFeedbackHandler = func(ctx context.Context, req *dotfilesdv1.FeedbackRequest) (string, error) {
	slog.Warn("feedback requested but no handler set",
		"feedback_id", req.FeedbackId,
		"prompt", req.Prompt,
	)
	return "", fmt.Errorf("feedback not supported")
}

type CallbackServer struct {
	server  *http.Server
	port    int
	handler *callbackHandler
}

type callbackHandler struct {
	dotfilesdv1connect.UnimplementedClientCallbackHandler
	onFeedback func(context.Context, *dotfilesdv1.FeedbackRequest) (string, error)
}

func (h *callbackHandler) Feedback(ctx context.Context, req *connect.Request[dotfilesdv1.FeedbackRequest]) (*connect.Response[dotfilesdv1.FeedbackResponse], error) {
	slog.Debug("callback feedback received", "feedback_id", req.Msg.FeedbackId, "prompt", req.Msg.Prompt)
	data, err := h.onFeedback(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	resp := connect.NewResponse(&dotfilesdv1.FeedbackResponse{
		FeedbackId: req.Msg.FeedbackId,
		Data:       data,
	})
	resp.Header().Set("Session-Id", req.Msg.SessionId)
	return resp, nil
}

func NewCallbackServer() (*CallbackServer, error) {
	handler := &callbackHandler{
		onFeedback: defaultFeedbackHandler,
	}

	path, svcHandler := dotfilesdv1connect.NewClientCallbackHandler(handler)
	mux := http.NewServeMux()
	mux.Handle(path, svcHandler)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("callback listen: %w", err)
	}

	cs := &CallbackServer{
		server:  &http.Server{Handler: mux},
		port:    listener.Addr().(*net.TCPAddr).Port,
		handler: handler,
	}

	go func() {
		slog.Debug("callback server listening", "port", cs.port)
		if err := cs.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("callback server error", "error", err)
		}
	}()

	return cs, nil
}

func (cs *CallbackServer) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", cs.port)
}

func (cs *CallbackServer) Close() error {
	return cs.server.Close()
}

func (cs *CallbackServer) SetFeedbackHandler(fn func(context.Context, *dotfilesdv1.FeedbackRequest) (string, error)) {
	cs.handler.onFeedback = fn
}
