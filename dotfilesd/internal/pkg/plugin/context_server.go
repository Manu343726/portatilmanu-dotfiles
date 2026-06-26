package plugin

import (
	"context"
	"fmt"
	"net/http"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
)

// ContextBackend is the interface the daemon implements to provide execution
// and feedback services to plugins. This keeps the Execution Context server
// independent of daemon internals.
type ContextBackend interface {
	// Exec runs a shell command. Returns exit code, stdout, stderr.
	Exec(ctx context.Context, sessionID, command string) (exitCode int32, stdout, stderr string, err error)

	// SudoExec runs a shell command with sudo. The daemon handles password
	// elicitation internally.
	SudoExec(ctx context.Context, sessionID, command string) (exitCode int32, stdout, stderr string, err error)

	// RequestInput prompts the user for text input.
	RequestInput(ctx context.Context, sessionID, prompt, defaultVal string, sensitive bool) (value string, err error)

	// RequestConfirm prompts the user for a yes/no confirmation.
	RequestConfirm(ctx context.Context, sessionID, msg string, defaultConfirm bool) (confirmed bool, err error)

	// RequestChoose prompts the user to pick from a list of options.
	RequestChoose(ctx context.Context, sessionID, prompt string, options []string, defaultIndex int) (selectedIndex int32, selectedOption string, err error)
}

// ContextServerOptions configures the Execution Context server.
type ContextServerOptions struct {
	// Backend implements the actual execution and feedback logic.
	Backend ContextBackend

	// Token is the shared secret that plugins must present via the
	// X-Dotfiles-Context-Token header. If empty, token validation is
	// disabled (not recommended).
	Token string
}

// contextServer implements the ExecutionContext Connect service on the
// daemon side. It validates the plugin's auth token and proxies requests
// to the provided ContextBackend.
type contextServer struct {
	opts ContextServerOptions
}

// NewContextServer creates an ExecutionContext handler and returns the
// HTTP handler and path for mounting on the daemon's mux.
func NewContextServer(opts ContextServerOptions) (string, http.Handler) {
	svc := &contextServer{opts: opts}
	path, handler := dotfilesdv1connect.NewExecutionContextHandler(svc)
	return path, handler
}

// serveHTTP is a convenience wrapper: returns a mux-ready handler without
// needing to extract path manually. Mount with:
//
//	mux.Handle(NewContextServer(opts))
func NewContextServerHandler(opts ContextServerOptions) http.Handler {
	_, handler := NewContextServer(opts)
	return handler
}

// ---- auth ----

func (s *contextServer) authenticate(req connect.AnyRequest) error {
	if s.opts.Token == "" {
		return nil // token validation disabled
	}
	token := req.Header().Get("X-Dotfiles-Context-Token")
	if token == "" || token != s.opts.Token {
		return connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("invalid or missing context token"))
	}
	return nil
}

// ---- Exec ----

func (s *contextServer) Exec(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ExecRequest],
) (*connect.Response[dotfilesdv1.ExecResponse], error) {
	if err := s.authenticate(req); err != nil {
		return nil, err
	}

	sessionID := ""
	if req.Msg.Session != nil {
		sessionID = req.Msg.Session.Id
	}

	exitCode, stdout, stderr, err := s.opts.Backend.Exec(ctx, sessionID, req.Msg.Command)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("exec: %w", err))
	}

	return connect.NewResponse(&dotfilesdv1.ExecResponse{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
	}), nil
}

// ---- SudoExec ----

func (s *contextServer) SudoExec(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ContextSudoExecRequest],
) (*connect.Response[dotfilesdv1.ContextSudoExecResponse], error) {
	if err := s.authenticate(req); err != nil {
		return nil, err
	}

	sessionID := ""
	if req.Msg.Session != nil {
		sessionID = req.Msg.Session.Id
	}

	exitCode, stdout, stderr, err := s.opts.Backend.SudoExec(ctx, sessionID, req.Msg.Command)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("sudo exec: %w", err))
	}

	return connect.NewResponse(&dotfilesdv1.ContextSudoExecResponse{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
	}), nil
}

// ---- RequestInput ----

func (s *contextServer) RequestInput(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.InputRequest],
) (*connect.Response[dotfilesdv1.InputResponse], error) {
	if err := s.authenticate(req); err != nil {
		return nil, err
	}

	sessionID := ""
	if req.Msg.Session != nil {
		sessionID = req.Msg.Session.Id
	}

	value, err := s.opts.Backend.RequestInput(ctx, sessionID, req.Msg.Prompt, req.Msg.Default, req.Msg.Sensitive)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("request input: %w", err))
	}

	return connect.NewResponse(&dotfilesdv1.InputResponse{
		Value: value,
	}), nil
}

// ---- RequestConfirm ----

func (s *contextServer) RequestConfirm(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ConfirmRequest],
) (*connect.Response[dotfilesdv1.ConfirmResponse], error) {
	if err := s.authenticate(req); err != nil {
		return nil, err
	}

	sessionID := ""
	if req.Msg.Session != nil {
		sessionID = req.Msg.Session.Id
	}

	confirmed, err := s.opts.Backend.RequestConfirm(ctx, sessionID, req.Msg.Message, req.Msg.DefaultConfirm)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("request confirm: %w", err))
	}

	return connect.NewResponse(&dotfilesdv1.ConfirmResponse{
		Confirmed: confirmed,
	}), nil
}

// ---- RequestChoose ----

func (s *contextServer) RequestChoose(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ChooseRequest],
) (*connect.Response[dotfilesdv1.ChooseResponse], error) {
	if err := s.authenticate(req); err != nil {
		return nil, err
	}

	sessionID := ""
	if req.Msg.Session != nil {
		sessionID = req.Msg.Session.Id
	}

	selectedIndex, selectedOption, err := s.opts.Backend.RequestChoose(ctx, sessionID, req.Msg.Prompt, req.Msg.Options, int(req.Msg.DefaultIndex))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("request choose: %w", err))
	}

	return connect.NewResponse(&dotfilesdv1.ChooseResponse{
		SelectedIndex:  selectedIndex,
		SelectedOption: selectedOption,
	}), nil
}
