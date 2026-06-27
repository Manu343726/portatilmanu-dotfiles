package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// feedbackServer implements the FeedbackService usage-level RPCs.
// Both CLI tools and plugins use this service to request user feedback
// (input, confirm, choose). The daemon routes the prompt through whatever
// channel is available (MCP elicitation, terminal, graphical dialog).
type feedbackServer struct {
	sessions *SessionStore
}

func newFeedbackServer(sessions *SessionStore) *feedbackServer {
	return &feedbackServer{sessions: sessions}
}

func (s *feedbackServer) RequestInput(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.InputRequest],
) (*connect.Response[dotfilesdv1.InputResponse], error) {
	slog.Log(ctx, levelTrace, "FeedbackService.RequestInput", "prompt", req.Msg.Prompt, "sensitive", req.Msg.Sensitive)

	session := s.sessions.ResolveSession(req.Msg.GetSession())

	value, err := session.RequestInput(ctx, req.Msg.Prompt, req.Msg.Default, req.Msg.Sensitive)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("request input: %w", err))
	}

	return connect.NewResponse(&dotfilesdv1.InputResponse{
		Value: value,
	}), nil
}

func (s *feedbackServer) RequestConfirm(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ConfirmRequest],
) (*connect.Response[dotfilesdv1.ConfirmResponse], error) {
	slog.Log(ctx, levelTrace, "FeedbackService.RequestConfirm", "message", req.Msg.Message)

	session := s.sessions.ResolveSession(req.Msg.GetSession())

	confirmed, err := session.RequestConfirm(ctx, req.Msg.Message, req.Msg.DefaultConfirm)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("request confirm: %w", err))
	}

	return connect.NewResponse(&dotfilesdv1.ConfirmResponse{
		Confirmed: confirmed,
	}), nil
}

func (s *feedbackServer) RequestChoose(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ChooseRequest],
) (*connect.Response[dotfilesdv1.ChooseResponse], error) {
	slog.Log(ctx, levelTrace, "FeedbackService.RequestChoose", "prompt", req.Msg.Prompt)

	session := s.sessions.ResolveSession(req.Msg.GetSession())

	selectedIndex, selectedOption, err := session.RequestChoose(ctx, req.Msg.Prompt, req.Msg.Options, int(req.Msg.DefaultIndex))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("request choose: %w", err))
	}

	return connect.NewResponse(&dotfilesdv1.ChooseResponse{
		SelectedIndex:  int32(selectedIndex),
		SelectedOption: selectedOption,
	}), nil
}
