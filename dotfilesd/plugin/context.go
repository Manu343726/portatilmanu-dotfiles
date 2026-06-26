package plugin

import (
	"context"
	"fmt"
	"net/http"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
)

// Context provides a plugin tool with controlled access to the daemon's
// capabilities: shell execution (with or without sudo), user input prompts,
// confirmations, and choice selection.
//
// This is the ONLY way a plugin tool should interact with the host system.
// Plugins never call the daemon's core RPCs directly.
type Context interface {
	// Exec runs a shell command without privilege escalation.
	Exec(cmd string) (ExecResult, error)

	// SudoExec runs a shell command with sudo. The daemon handles password
	// elicitation internally.
	SudoExec(cmd string) (ExecResult, error)

	// RequestInput prompts the user for arbitrary text input.
	RequestInput(prompt, defaultVal string, sensitive bool) (string, error)

	// RequestConfirm prompts the user for a yes/no confirmation.
	RequestConfirm(msg string, defaultConfirm bool) (bool, error)

	// RequestChoose prompts the user to pick from a list of options.
	// Returns the selected index and option text (index = -1 if cancelled).
	RequestChoose(prompt string, options []string, defaultIndex int) (int, string, error)
}

// ExecResult contains the result of a shell command execution.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// contextClient implements the Context interface by calling the daemon's
// ExecutionContext service over Connect RPC.
type contextClient struct {
	client    dotfilesdv1connect.ExecutionContextClient
	token     string
	sessionID string
}

// newContextClient creates a new Context client connected to the daemon's
// Execution Context service.
func newContextClient(url, token, sessionID string) *contextClient {
	return &contextClient{
		client:    dotfilesdv1connect.NewExecutionContextClient(&http.Client{}, url),
		token:     token,
		sessionID: sessionID,
	}
}

// buildSession creates a Session message for use in context requests.
func (c *contextClient) buildSession() *dotfilesdv1.Session {
	return &dotfilesdv1.Session{Id: c.sessionID}
}

// authHeader returns the auth header value for context requests.
func (c *contextClient) authHeader() string {
	return c.token
}

func (c *contextClient) Exec(cmd string) (ExecResult, error) {
	req := connect.NewRequest(&dotfilesdv1.ExecRequest{
		Session: c.buildSession(),
		Command: cmd,
	})
	req.Header().Set("X-Dotfiles-Context-Token", c.authHeader())

	resp, err := c.client.Exec(context.Background(), req)
	if err != nil {
		return ExecResult{}, fmt.Errorf("context exec: %w", err)
	}

	return ExecResult{
		ExitCode: int(resp.Msg.ExitCode),
		Stdout:   resp.Msg.Stdout,
		Stderr:   resp.Msg.Stderr,
	}, nil
}

func (c *contextClient) SudoExec(cmd string) (ExecResult, error) {
	req := connect.NewRequest(&dotfilesdv1.ContextSudoExecRequest{
		Session: c.buildSession(),
		Command: cmd,
	})
	req.Header().Set("X-Dotfiles-Context-Token", c.authHeader())

	resp, err := c.client.SudoExec(context.Background(), req)
	if err != nil {
		return ExecResult{}, fmt.Errorf("context sudo exec: %w", err)
	}

	return ExecResult{
		ExitCode: int(resp.Msg.ExitCode),
		Stdout:   resp.Msg.Stdout,
		Stderr:   resp.Msg.Stderr,
	}, nil
}

func (c *contextClient) RequestInput(prompt, defaultVal string, sensitive bool) (string, error) {
	req := connect.NewRequest(&dotfilesdv1.InputRequest{
		Session:   c.buildSession(),
		Prompt:    prompt,
		Default:   defaultVal,
		Sensitive: sensitive,
	})
	req.Header().Set("X-Dotfiles-Context-Token", c.authHeader())

	resp, err := c.client.RequestInput(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("context request input: %w", err)
	}

	return resp.Msg.Value, nil
}

func (c *contextClient) RequestConfirm(msg string, defaultConfirm bool) (bool, error) {
	req := connect.NewRequest(&dotfilesdv1.ConfirmRequest{
		Session:        c.buildSession(),
		Message:        msg,
		DefaultConfirm: defaultConfirm,
	})
	req.Header().Set("X-Dotfiles-Context-Token", c.authHeader())

	resp, err := c.client.RequestConfirm(context.Background(), req)
	if err != nil {
		return false, fmt.Errorf("context request confirm: %w", err)
	}

	return resp.Msg.Confirmed, nil
}

func (c *contextClient) RequestChoose(prompt string, options []string, defaultIndex int) (int, string, error) {
	req := connect.NewRequest(&dotfilesdv1.ChooseRequest{
		Session:      c.buildSession(),
		Prompt:       prompt,
		Options:      options,
		DefaultIndex: int32(defaultIndex),
	})
	req.Header().Set("X-Dotfiles-Context-Token", c.authHeader())

	resp, err := c.client.RequestChoose(context.Background(), req)
	if err != nil {
		return 0, "", fmt.Errorf("context request choose: %w", err)
	}

	return int(resp.Msg.SelectedIndex), resp.Msg.SelectedOption, nil
}
