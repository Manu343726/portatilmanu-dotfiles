package plugin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"dotfilesd/internal/pkg/logging"
	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
)

// Context provides a plugin tool with controlled access to the daemon's
// capabilities: shell execution (with or without sudo), user input prompts,
// confirmations, and choice selection. It also provides stdout and stderr
// writers that tunnel output back to the caller in real time via RPC
// streaming.
//
// This is the ONLY way a plugin tool should interact with the host system.
// Plugins never call the daemon's core RPCs directly.
//
// Tools write their output to Stdout() and Stderr() writers (for human-
// readable progress and results). The Run() method returns a Go error:
// nil means success, non-nil means the tool failed.
type Context interface {
	// Stdout returns a writer that tunnels stdout output back to the
	// CLI/MCP caller in real time via RPC streaming.
	Stdout() io.Writer

	// Stderr returns a writer that tunnels stderr output back to the
	// CLI/MCP caller in real time via RPC streaming.
	Stderr() io.Writer

	// Log returns a structured logger that sends log entries to the
	// daemon's logging system. The plugin name is automatically attached
	// so entries appear under a "plugins.<name>" hierarchy.
	Log() logging.Logger

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

	// CallPlugin invokes a tool on another loaded plugin. Plugins can use
	// this to delegate work to other plugins without shelling out to
	// dotfilesctl as a subprocess.
	CallPlugin(pluginName, toolName string, args map[string]string) (ExecResult, error)

	// RunScript runs a registered script by name (e.g. "git/commit").
	// The script executes on the daemon host and may include feedback
	// steps (confirm, input, choose).
	RunScript(name string) (ScriptResult, error)
}

// ExecResult contains the result of a shell command execution.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// ScriptResult contains the result of a script execution.
type ScriptResult struct {
	AllSucceeded bool
	Steps        []ScriptStepResult
	Error        string
}

// ScriptStepResult contains the result of a single script step.
type ScriptStepResult struct {
	StepNumber    int
	SourceLine    string
	StepKind      string // "exec", "confirm", "input", "choose"
	ExitCode      int
	Stdout        string
	Stderr        string
	FeedbackValue string
}

// contextClient implements the Context interface by calling the daemon's
// usage-level services over Connect RPC. Each domain has its own service
// client (ExecService, FeedbackService, LogService, PluginService,
// ScriptService). All requests carry the X-Dotfiles-Context-Token header
// for authentication.
type contextClient struct {
	execClient     dotfilesdv1connect.ExecServiceClient
	feedbackClient dotfilesdv1connect.FeedbackServiceClient
	logClient      dotfilesdv1connect.LogServiceClient
	pluginClient   dotfilesdv1connect.PluginServiceClient
	scriptClient   dotfilesdv1connect.ScriptServiceClient

	token      string
	sessionID  string
	pluginName string
	log        logging.Logger
}

// newContextClient creates a new Context client connected to the daemon's
// usage-level services.
func newContextClient(url, token, sessionID, pluginName string) *contextClient {
	httpClient := &http.Client{}
	c := &contextClient{
		execClient:     dotfilesdv1connect.NewExecServiceClient(httpClient, url),
		feedbackClient: dotfilesdv1connect.NewFeedbackServiceClient(httpClient, url),
		logClient:      dotfilesdv1connect.NewLogServiceClient(httpClient, url),
		pluginClient:   dotfilesdv1connect.NewPluginServiceClient(httpClient, url),
		scriptClient:   dotfilesdv1connect.NewScriptServiceClient(httpClient, url),
		token:          token,
		sessionID:      sessionID,
		pluginName:     pluginName,
	}
	c.log = &pluginLogger{client: c, pluginName: pluginName}
	return c
}

// Log returns a structured logger that sends entries to the daemon.
func (c *contextClient) Log() logging.Logger { return c.log }

// buildSession creates a Session message for use in context requests.
func (c *contextClient) buildSession() *dotfilesdv1.Session {
	return &dotfilesdv1.Session{Id: c.sessionID}
}

// setTokenHeader sets the X-Dotfiles-Context-Token header on a request.
func (c *contextClient) setTokenHeader(req connect.AnyRequest) {
	if c.token != "" {
		req.Header().Set("X-Dotfiles-Context-Token", c.token)
	}
}

func (c *contextClient) Exec(cmd string) (ExecResult, error) {
	req := connect.NewRequest(&dotfilesdv1.ExecRequest{
		Session: c.buildSession(),
		Command: cmd,
	})
	c.setTokenHeader(req)

	resp, err := c.execClient.Exec(context.Background(), req)
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec: %w", err)
	}

	return ExecResult{
		ExitCode: int(resp.Msg.ExitCode),
		Stdout:   resp.Msg.Stdout,
		Stderr:   resp.Msg.Stderr,
	}, nil
}

func (c *contextClient) SudoExec(cmd string) (ExecResult, error) {
	req := connect.NewRequest(&dotfilesdv1.ExecRequest{
		Session: c.buildSession(),
		Command: cmd,
		Sudo:    true,
	})
	c.setTokenHeader(req)

	resp, err := c.execClient.Exec(context.Background(), req)
	if err != nil {
		return ExecResult{}, fmt.Errorf("sudo exec: %w", err)
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
	c.setTokenHeader(req)

	resp, err := c.feedbackClient.RequestInput(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("request input: %w", err)
	}

	return resp.Msg.Value, nil
}

func (c *contextClient) RequestConfirm(msg string, defaultConfirm bool) (bool, error) {
	req := connect.NewRequest(&dotfilesdv1.ConfirmRequest{
		Session:        c.buildSession(),
		Message:        msg,
		DefaultConfirm: defaultConfirm,
	})
	c.setTokenHeader(req)

	resp, err := c.feedbackClient.RequestConfirm(context.Background(), req)
	if err != nil {
		return false, fmt.Errorf("request confirm: %w", err)
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
	c.setTokenHeader(req)

	resp, err := c.feedbackClient.RequestChoose(context.Background(), req)
	if err != nil {
		return 0, "", fmt.Errorf("request choose: %w", err)
	}

	return int(resp.Msg.SelectedIndex), resp.Msg.SelectedOption, nil
}

func (c *contextClient) CallPlugin(pluginName, toolName string, args map[string]string) (ExecResult, error) {
	req := connect.NewRequest(&dotfilesdv1.CallPluginToolRequest{
		Session:    c.buildSession(),
		PluginName: pluginName,
		ToolName:   toolName,
		Arguments:  args,
	})
	c.setTokenHeader(req)

	stream, err := c.pluginClient.CallPluginTool(context.Background(), req)
	if err != nil {
		return ExecResult{}, fmt.Errorf("call plugin: %w", err)
	}

	var stdoutBuf, stderrBuf string
	var errMsg string
	for stream.Receive() {
		chunk := stream.Msg()
		if len(chunk.StdoutChunk) > 0 {
			stdoutBuf += string(chunk.StdoutChunk)
		}
		if len(chunk.StderrChunk) > 0 {
			stderrBuf += string(chunk.StderrChunk)
		}
		if chunk.Done {
			errMsg = chunk.ErrorMessage
			break
		}
	}
	if err := stream.Err(); err != nil {
		return ExecResult{}, fmt.Errorf("call plugin stream: %w", err)
	}

	exitCode := 0
	if errMsg != "" {
		exitCode = 1
	}

	return ExecResult{
		ExitCode: exitCode,
		Stdout:   stdoutBuf,
		Stderr:   stderrBuf,
	}, nil
}

func (c *contextClient) RunScript(name string) (ScriptResult, error) {
	req := connect.NewRequest(&dotfilesdv1.RunScriptRequest{
		Session: c.buildSession(),
		Source: &dotfilesdv1.RunScriptRequest_RegisteredScript{
			RegisteredScript: name,
		},
	})
	c.setTokenHeader(req)

	resp, err := c.scriptClient.RunScript(context.Background(), req)
	if err != nil {
		return ScriptResult{}, fmt.Errorf("run script: %w", err)
	}

	steps := make([]ScriptStepResult, len(resp.Msg.Steps))
	for i, s := range resp.Msg.Steps {
		steps[i] = ScriptStepResult{
			StepNumber:    int(s.StepNumber),
			SourceLine:    s.SourceLine,
			StepKind:      s.StepKind,
			ExitCode:      int(s.ExitCode),
			Stdout:        s.Stdout,
			Stderr:        s.Stderr,
			FeedbackValue: s.FeedbackValue,
		}
	}

	return ScriptResult{
		AllSucceeded: resp.Msg.AllSucceeded,
		Steps:        steps,
		Error:        resp.Msg.Error,
	}, nil
}

// Stdout returns a no-op writer (no streaming outside a tool call context).
// During tool execution, the plugin server wraps context with a
// streamingContext that provides real stdout/stderr writers.
func (c *contextClient) Stdout() io.Writer { return &nopWriter{} }

// Stderr returns a no-op writer (no streaming outside a tool call context).
func (c *contextClient) Stderr() io.Writer { return &nopWriter{} }

// nopWriter is an io.Writer that discards all data.
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// streamingContext wraps a Context with real stdout/stderr writers that
// tunnel output via the RPC stream. Used by the plugin server during tool
// execution.
type streamingContext struct {
	Context
	stdout io.Writer
	stderr io.Writer
}

func (c *streamingContext) Stdout() io.Writer   { return c.stdout }
func (c *streamingContext) Stderr() io.Writer   { return c.stderr }
func (c *streamingContext) Log() logging.Logger { return c.Context.Log() }

// ---------------------------------------------------------------------------
// pluginLogger — sends log entries to the daemon via the Log RPC
// ---------------------------------------------------------------------------

// pluginLogger implements logging.Logger by calling the daemon's Log RPC.
// Each log call sends a separate RPC. The daemon routes entries through
// its logging system with the plugin name as the logger module.
type pluginLogger struct {
	client     *contextClient
	pluginName string
	fixedAttrs []any
}

func (l *pluginLogger) log(level, msg string, attrs ...any) {
	// Merge fixed attrs with call attrs (call attrs take precedence).
	merged := make(map[string]string)
	for i := 0; i < len(l.fixedAttrs)-1; i += 2 {
		k := fmt.Sprintf("%v", l.fixedAttrs[i])
		v := fmt.Sprintf("%v", l.fixedAttrs[i+1])
		merged[k] = v
	}
	for i := 0; i < len(attrs)-1; i += 2 {
		k := fmt.Sprintf("%v", attrs[i])
		v := fmt.Sprintf("%v", attrs[i+1])
		merged[k] = v
	}

	req := connect.NewRequest(&dotfilesdv1.LogRequest{
		Session: l.client.buildSession(),
		Source:  "plugin." + l.pluginName,
		Entry: &dotfilesdv1.LogEntry{
			Level:      level,
			Message:    msg,
			Attributes: merged,
		},
	})
	l.client.setTokenHeader(req)

	_, _ = l.client.logClient.Log(context.Background(), req)
}

func (l *pluginLogger) Trace(msg string, attrs ...any) { l.log("trace", msg, attrs...) }
func (l *pluginLogger) Debug(msg string, attrs ...any) { l.log("debug", msg, attrs...) }
func (l *pluginLogger) Info(msg string, attrs ...any)  { l.log("info", msg, attrs...) }
func (l *pluginLogger) Warn(msg string, attrs ...any)  { l.log("warn", msg, attrs...) }
func (l *pluginLogger) Error(msg string, attrs ...any) { l.log("error", msg, attrs...) }
func (l *pluginLogger) Fatal(msg string, attrs ...any) {
	l.log("fatal", msg, attrs...)
	os.Exit(1)
}

func (l *pluginLogger) Child(name string) logging.Logger {
	return &pluginLogger{
		client:     l.client,
		pluginName: l.pluginName + "." + name,
		fixedAttrs: l.fixedAttrs,
	}
}

func (l *pluginLogger) WithAttrs(attrs ...any) logging.Logger {
	newAttrs := make([]any, len(l.fixedAttrs)+len(attrs))
	copy(newAttrs, l.fixedAttrs)
	copy(newAttrs[len(l.fixedAttrs):], attrs)
	return &pluginLogger{
		client:     l.client,
		pluginName: l.pluginName,
		fixedAttrs: newAttrs,
	}
}

func (l *pluginLogger) Enabled(level logging.Level) bool {
	// Plugin logger always reports as enabled at every level. The daemon
	// decides whether to actually record the entry based on its own config.
	return true
}
