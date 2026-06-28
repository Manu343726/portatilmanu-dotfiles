package plugin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"dotfilesd/internal/pkg/logging"
	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
)

// Context is the interface plugins use to interact with the daemon.
// Plugins call each other DIRECTLY via generated Connect clients,
// NOT through this Context.
type Context interface {
	Stdout() io.Writer
	Stderr() io.Writer
	Log() logging.Logger

	// RenderOutput returns true if the caller expects human-readable formatted
	// output written to Stdout(). When false, handlers should return raw data
	// in the RPC response message for programmatic consumption.
	RenderOutput() bool

	// WithRenderOutput returns a child Context that forwards the render
	// preference to downstream plugin calls.
	WithRenderOutput(bool) Context

	// Shell execution
	Exec(cmd string) (ExecResult, error)
	SudoExec(cmd string) (ExecResult, error)
	ExecStream(cmd string, sudo bool) (int, error)
	BackgroundExec(cmd string, sudo bool) (BackgroundTask, error)

	// User interaction
	RequestInput(prompt, defaultVal string, sensitive bool) (string, error)
	RequestConfirm(msg string, defaultConfirm bool) (bool, error)
	RequestChoose(prompt string, options []string, defaultIndex int) (int, string, error)

	// Scripts
	RunScript(name string) (ScriptResult, error)
}

// ExecResult is the result of a shell command.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// ScriptResult is the result of a script execution.
type ScriptResult struct {
	AllSucceeded bool
	Steps        []ScriptStepResult
	Error        string
}

// ScriptStepResult is the result of a single script step.
type ScriptStepResult struct {
	StepNumber    int
	SourceLine    string
	StepKind      string
	ExitCode      int
	Stdout        string
	Stderr        string
	FeedbackValue string
}

// contextClient implements Context by calling daemon usage services.
type contextClient struct {
	execClient     dotfilesdv1connect.ExecServiceClient
	feedbackClient dotfilesdv1connect.FeedbackServiceClient
	logClient      dotfilesdv1connect.LogServiceClient
	scriptClient   dotfilesdv1connect.ScriptServiceClient
	sessionClient  dotfilesdv1connect.SessionServiceClient

	token, sessionID, pluginName string
	renderOutput                 bool
	log                          logging.Logger
}

func newContextClient(url, token, sessionID, pluginName string) *contextClient {
	httpClient := &http.Client{}
	c := &contextClient{
		execClient:     dotfilesdv1connect.NewExecServiceClient(httpClient, url),
		feedbackClient: dotfilesdv1connect.NewFeedbackServiceClient(httpClient, url),
		logClient:      dotfilesdv1connect.NewLogServiceClient(httpClient, url),
		scriptClient:   dotfilesdv1connect.NewScriptServiceClient(httpClient, url),
		sessionClient:  dotfilesdv1connect.NewSessionServiceClient(httpClient, url),
		token:          token,
		sessionID:      sessionID,
		pluginName:     pluginName,
	}
	c.log = &pluginLogger{client: c, pluginName: pluginName}

	// Register a real daemon session so that Exec() calls from background
	// tasks have a proper session context (avoids "session not found" warnings).
	// The initial sessionID from SESSION_ID env is a sentinel; we replace it
	// with a real daemon-issued session ID.
	ctx := context.Background()
	connectReq := connect.NewRequest(&dotfilesdv1.ConnectRequest{
		CallbackUrl: "",
	})
	c.setTokenHeader(connectReq)
	connectResp, err := c.sessionClient.Connect(ctx, connectReq)
	if err == nil && connectResp.Msg.Session != nil && connectResp.Msg.Session.Id != "" {
		c.sessionID = connectResp.Msg.Session.Id
	}
	return c
}

func (c *contextClient) RenderOutput() bool { return c.renderOutput }

func (c *contextClient) WithRenderOutput(v bool) Context {
	return &contextClient{
		execClient:     c.execClient,
		feedbackClient: c.feedbackClient,
		logClient:      c.logClient,
		scriptClient:   c.scriptClient,
		sessionClient:  c.sessionClient,
		token:          c.token,
		sessionID:      c.sessionID,
		pluginName:     c.pluginName,
		renderOutput:   v,
		log:            c.log,
	}
}

func (c *contextClient) Log() logging.Logger { return c.log }

func (c *contextClient) buildSession() *dotfilesdv1.Session {
	return &dotfilesdv1.Session{Id: c.sessionID}
}

func (c *contextClient) setTokenHeader(req connect.AnyRequest) {
	if c.token != "" {
		req.Header().Set("X-Dotfiles-Context-Token", c.token)
	}
	if c.renderOutput {
		req.Header().Set("X-Dotfiles-Render-Output", "true")
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

func (c *contextClient) ExecStream(cmd string, sudo bool) (int, error) {
	return execStreamWithWriters(c, c.Stdout(), c.Stderr(), cmd, sudo)
}

func execStreamWithWriters(c *contextClient, stdout, stderr io.Writer, cmd string, sudo bool) (int, error) {
	req := connect.NewRequest(&dotfilesdv1.ExecStreamRequest{
		Session: c.buildSession(),
		Command: cmd,
		Sudo:    sudo,
	})
	c.setTokenHeader(req)

	stream, err := c.execClient.ExecStream(context.Background(), req)
	if err != nil {
		return -1, fmt.Errorf("exec stream: %w", err)
	}

	exitCode := int32(-1)
	for stream.Receive() {
		chunk := stream.Msg()
		if len(chunk.StdoutChunk) > 0 {
			stdout.Write(chunk.StdoutChunk)
		}
		if len(chunk.StderrChunk) > 0 {
			stderr.Write(chunk.StderrChunk)
		}
		if chunk.Done {
			exitCode = chunk.ExitCode
			if chunk.ErrorMessage != "" {
				return int(exitCode), fmt.Errorf("%s", chunk.ErrorMessage)
			}
			break
		}
	}
	if err := stream.Err(); err != nil {
		return int(exitCode), fmt.Errorf("exec stream: %w", err)
	}
	return int(exitCode), nil
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

func (c *contextClient) Stdout() io.Writer {
	return &daemonLogWriter{
		client: c,
		level:  dotfilesdv1.LogLevel_LOG_LEVEL_INFO,
		source: c.pluginName + "/stdout",
	}
}

func (c *contextClient) Stderr() io.Writer {
	return &daemonLogWriter{
		client: c,
		level:  dotfilesdv1.LogLevel_LOG_LEVEL_WARN,
		source: c.pluginName + "/stderr",
	}
}

// daemonLogWriter is an io.Writer that sends data as log entries to the
// daemon's LogService. Writes are buffered and flushed on newline or at
// a max buffer size to avoid excessive RPC calls.
type daemonLogWriter struct {
	client *contextClient
	level  dotfilesdv1.LogLevel
	source string

	mu  sync.Mutex
	buf []byte
}

func (w *daemonLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	total := len(p)
	for len(p) > 0 {
		// Find the next newline or end of data.
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			// No newline — buffer it.
			w.buf = append(w.buf, p...)
			return total, nil
		}

		// Include everything up to and including the newline.
		w.buf = append(w.buf, p[:idx+1]...)
		p = p[idx+1:]

		// Flush the line.
		line := string(w.buf)
		w.buf = w.buf[:0]
		w.flushLine(line)
	}
	return total, nil
}

func (w *daemonLogWriter) flushLine(line string) {
	req := connect.NewRequest(&dotfilesdv1.LogRequest{
		Session: w.client.buildSession(),
		Source:  w.source,
		Entry: &dotfilesdv1.LogEntry{
			Level:      w.level,
			Message:    strings.TrimRight(line, "\r\n"),
			Attributes: nil,
		},
	})
	w.client.setTokenHeader(req)
	// Best-effort; use background context so we don't block on log delivery.
	_, _ = w.client.logClient.Log(context.Background(), req)
}

// pluginLogger implements logging.Logger by calling the daemon's Log RPC.
type pluginLogger struct {
	client     *contextClient
	pluginName string
	fixedAttrs []any
}

func (l *pluginLogger) log(level dotfilesdv1.LogLevel, msg string, attrs ...any) {
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

func (l *pluginLogger) Trace(msg string, attrs ...any) {
	l.log(dotfilesdv1.LogLevel_LOG_LEVEL_TRACE, msg, attrs...)
}
func (l *pluginLogger) Debug(msg string, attrs ...any) {
	l.log(dotfilesdv1.LogLevel_LOG_LEVEL_DEBUG, msg, attrs...)
}
func (l *pluginLogger) Info(msg string, attrs ...any) {
	l.log(dotfilesdv1.LogLevel_LOG_LEVEL_INFO, msg, attrs...)
}
func (l *pluginLogger) Warn(msg string, attrs ...any) {
	l.log(dotfilesdv1.LogLevel_LOG_LEVEL_WARN, msg, attrs...)
}
func (l *pluginLogger) Error(msg string, attrs ...any) {
	l.log(dotfilesdv1.LogLevel_LOG_LEVEL_ERROR, msg, attrs...)
}
func (l *pluginLogger) Fatal(msg string, attrs ...any) {
	l.log(dotfilesdv1.LogLevel_LOG_LEVEL_ERROR, msg, attrs...)
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

func (l *pluginLogger) Enabled(level logging.Level) bool { return true }

func (c *contextClient) BackgroundExec(cmd string, sudo bool) (BackgroundTask, error) {
	return startBackgroundTask(c.execClient, c.token, c.buildSession(), c.Stdout(), c.Stderr(), cmd, sudo)
}
