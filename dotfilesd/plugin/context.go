package plugin

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	cryptorand "crypto/rand"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"dotfilesd/internal/pkg/logging"
	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
)

// contextClient implements Context by calling daemon usage services.

// Context is the interface plugins use to interact with the daemon.
// Plugins call each other DIRECTLY via generated Connect clients,
// NOT through this Context.
type Context interface {
	Stdout() io.Writer
	Stderr() io.Writer
	Stdin() io.Reader
	Log() logging.Logger

	// RenderOutput returns true if the caller expects human-readable formatted
	// output written to Stdout(). When false, handlers should return raw data
	// in the RPC response message for programmatic consumption.
	RenderOutput() bool

	// DiagParent returns the diagnostic parent resource ID (set by the daemon
	// executor for CLI-triggered calls). Empty string means no parent override.
	// Plugins can propagate this to downstream calls for full traceability.
	DiagParent() string

	// WithRenderOutput returns a child Context that forwards the render
	// preference to downstream plugin calls.
	WithRenderOutput(bool) Context

	// Colored output helpers for plugin stdout. They honour NO_COLOR and
	// terminal detection automatically.
	ColorStdout() io.Writer                 // stdout with bold, green, red etc.
	Greenf(format string, a ...any) string  // green formatted text
	Redf(format string, a ...any) string    // red formatted text
	Bluef(format string, a ...any) string   // blue formatted text
	Orangef(format string, a ...any) string // orange formatted text
	Yellowf(format string, a ...any) string // yellow formatted text
	Dimf(format string, a ...any) string    // dim/grey formatted text
	Boldf(format string, a ...any) string   // bold formatted text
	Styled(s, style string) string          // wrap text in arbitrary ANSI style
	ColorReset() string                     // ANSI reset sequence

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

	// TtyConn opens a raw bidirectional TTY stream to the CLI's terminal.
	// Use this instead of Stdin()/Stdout() when you need raw byte-level
	// terminal I/O (e.g. for tview/tcell). The stream bypasses the
	// line-buffered log system and delivers every byte immediately.
	// Returns an error if no interactive executor stream is available.
	TtyConn() (TTYConn, error)

	// PtyTtyConn returns a TTYConn backed by a real OS-level PTY pair.
	// Unlike TtyConn() which is a raw byte tunnel, PtyTtyConn gives the
	// caller a real pseudo-terminal. This is useful for tcell/tview
	// because the screen library can query terminfo, handle SIGWINCH,
	// and use proper terminal semantics. The PTY slave is used for I/O
	// with the TUI library; the PTY master is bridged to the CLI
	// terminal through the daemon.
	PtyTtyConn() (TTYConn, error)

	// DaemonClient returns an authenticated HTTP client for calling daemon
	// services. Use this with Connect RPC clients that need the plugin token
	// (e.g. DiagnosticsQueryService).
	DaemonClient() *http.Client
	DaemonURL() string

	// GetSecret retrieves a secret value for this plugin from the daemon's
	// secrets store. The value is encrypted in transit using an ECDH-negotiated
	// AES-256-GCM key and decrypted locally. The caller MUST zero the
	// returned byte slice after use to avoid plaintext lingering in memory.
	// Returns an error if no shared key has been negotiated or the secret
	// does not exist.
	// Example: token, err := ctx.GetSecret("api_token"); defer zeroBytes(token)
	GetSecret(key string) ([]byte, error)
}

// TTYConn is a bidirectional byte stream connected to the CLI's terminal
// through the executor bidi stream. It implements io.ReadWriteCloser and
// also provides Resize to notify the daemon of terminal dimension changes.
//
// Unlike Stdin()/Stdout() which go through the line-buffered Log RPC,
// TTYConn delivers every byte immediately — suitable for full-screen
// terminal libraries like tview/tcell.
type TTYConn interface {
	io.ReadWriteCloser
	Resize(width, height int) error
	Getsize() (width, height int, err error)
}
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
	ioClient       dotfilesdv1connect.IOServiceClient
	scriptClient   dotfilesdv1connect.ScriptServiceClient
	sessionClient  dotfilesdv1connect.SessionServiceClient
	diagPostClient dotfilesdv1connect.DiagnosticsPostServiceClient
	secretsClient  dotfilesdv1connect.SecretsServiceClient
	keyClient      dotfilesdv1connect.KeyServiceClient

	token, sessionID, pluginName, daemonURL string
	renderOutput                 bool
	clientID                     string
	diagParent                   string
	log                          logging.Logger

	daemonHTTPClient *http.Client

	// ECDH key negotiation for secrets.
	secretsKey      []byte // 32-byte shared secret, derived after NegotiateKey("secrets")
	secretsKeyReady bool
	secretsPriv     *ecdh.PrivateKey // ephemeral X25519 private key, kept until secretsKey is derived
}

// authRoundTripper injects X-Dotfiles-Context-Token into every outgoing request.
type authRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (a *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if a.token != "" {
		req.Header.Set("X-Dotfiles-Context-Token", a.token)
	}
	return a.base.RoundTrip(req)
}

func newContextClient(url, token, sessionID, pluginName, clientID string) *contextClient {
	h2cTransport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
	baseTransport := h2cTransport

	// Wrap transport to inject auth token on every request.
	var authTransport http.RoundTripper = baseTransport
	if token != "" {
		authTransport = &authRoundTripper{base: baseTransport, token: token}
	}

	httpClient := &http.Client{Transport: authTransport}
	c := &contextClient{
		execClient:     dotfilesdv1connect.NewExecServiceClient(httpClient, url),
		feedbackClient: dotfilesdv1connect.NewFeedbackServiceClient(httpClient, url),
		ioClient:       dotfilesdv1connect.NewIOServiceClient(httpClient, url),
		scriptClient:   dotfilesdv1connect.NewScriptServiceClient(httpClient, url),
		sessionClient:  dotfilesdv1connect.NewSessionServiceClient(httpClient, url),
		diagPostClient: dotfilesdv1connect.NewDiagnosticsPostServiceClient(httpClient, url),
		secretsClient:  dotfilesdv1connect.NewSecretsServiceClient(httpClient, url),
		keyClient:      dotfilesdv1connect.NewKeyServiceClient(httpClient, url),
		token:          token,
		sessionID:      sessionID,
		pluginName:     pluginName,
		clientID:       clientID,
		daemonURL:      url,
		daemonHTTPClient: httpClient,
	}
	c.log = &pluginLogger{client: c, pluginName: pluginName}

	// Register a real daemon session so that Exec() calls from background
	// tasks have a proper session context (avoids "session not found" warnings).
	// Pass the plugin name as session ID so daemon logs show which plugin
	// is issuing commands.
	ctx := context.Background()
	connectReq := connect.NewRequest(&dotfilesdv1.ConnectRequest{
		CallbackUrl: "",
		Session: &dotfilesdv1.Session{
			Id: c.sessionID,
			Variables: map[string]string{
				"_diag_parent": "plugin:" + pluginName,
			},
		},
	})
	c.setTokenHeader(connectReq)
	connectResp, err := c.sessionClient.Connect(ctx, connectReq)
	if err == nil && connectResp.Msg.Session != nil && connectResp.Msg.Session.Id != "" {
		c.sessionID = connectResp.Msg.Session.Id
	}
	return c
}

func (c *contextClient) RenderOutput() bool { return c.renderOutput }

func (c *contextClient) DiagParent() string { return c.diagParent }

func (c *contextClient) WithRenderOutput(v bool) Context {
	return &contextClient{
		execClient:     c.execClient,
		feedbackClient: c.feedbackClient,
		ioClient:       c.ioClient,
		scriptClient:   c.scriptClient,
		sessionClient:  c.sessionClient,
		diagPostClient: c.diagPostClient,
		token:          c.token,
		sessionID:      c.sessionID,
		pluginName:     c.pluginName,
		clientID:       c.clientID,
		renderOutput:   v,
		diagParent:     c.diagParent,
		log:            c.log,
	}
}

// pushDiagEvent sends a diagnostic event to the daemon.
func (c *contextClient) pushDiagEvent(eventType, resource, parent, message string, attrs map[string]string) {
	req := connect.NewRequest(&dotfilesdv1.DiagEvent{
		Type:        eventType,
		Resource:    resource,
		Parent:      parent,
		Message:     message,
		TimestampNs: time.Now().UnixNano(),
		Attrs:       attrs,
	})
	c.setTokenHeader(req)
	_, _ = c.diagPostClient.PostEvent(context.Background(), req)
}

// ── Coloured output helpers ──────────────────────────────────────────────

// ANSI escape sequences (standard 16-color codes).
const (
	clrGreen  = "\033[32m"
	clrRed    = "\033[31m"
	clrBlue   = "\033[34m"
	clrOrange = "\033[33m"
	clrYellow = "\033[93m"
	clrDim    = "\033[2m"
	clrBold   = "\033[1m"
	clrReset  = "\033[0m"
)

// noColourCheck is a cached check; re-evaluated on first call.
var noColourCheck = func() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return true
	}
	if fi, _ := os.Stdout.Stat(); fi != nil && (fi.Mode()&os.ModeCharDevice) == 0 {
		return true
	}
	return false
}

func (c *contextClient) isNoColor() bool { return noColourCheck() }

func (c *contextClient) apply(s, style string) string {
	if c.isNoColor() || style == "" {
		return s
	}
	return style + s + clrReset
}

func (c *contextClient) ColorStdout() io.Writer { return c.Stdout() }

func (c *contextClient) ColorReset() string { return c.apply("", "") }

func (c *contextClient) Greenf(format string, a ...any) string {
	return c.apply(fmt.Sprintf(format, a...), clrGreen)
}

func (c *contextClient) Redf(format string, a ...any) string {
	return c.apply(fmt.Sprintf(format, a...), clrRed)
}

func (c *contextClient) Bluef(format string, a ...any) string {
	return c.apply(fmt.Sprintf(format, a...), clrBlue)
}

func (c *contextClient) Orangef(format string, a ...any) string {
	return c.apply(fmt.Sprintf(format, a...), clrOrange)
}

func (c *contextClient) Yellowf(format string, a ...any) string {
	return c.apply(fmt.Sprintf(format, a...), clrYellow)
}

func (c *contextClient) Dimf(format string, a ...any) string {
	return c.apply(fmt.Sprintf(format, a...), clrDim)
}

func (c *contextClient) Boldf(format string, a ...any) string {
	return c.apply(fmt.Sprintf(format, a...), clrBold)
}

func (c *contextClient) Styled(s, style string) string {
	return c.apply(s, style)
}

func (c *contextClient) Log() logging.Logger { return c.log }

func (c *contextClient) buildSession() *dotfilesdv1.Session {
	s := &dotfilesdv1.Session{Id: c.sessionID}
	if c.diagParent != "" {
		s.Variables = map[string]string{"_diag_parent": c.diagParent}
	}
	return s
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
		client:   c,
		level:    dotfilesdv1.LogLevel_LOG_LEVEL_INFO,
		source:   c.pluginName + "/stdout",
		clientID: c.clientID,
	}
}

func (c *contextClient) Stderr() io.Writer {
	return &daemonLogWriter{
		client:   c,
		level:    dotfilesdv1.LogLevel_LOG_LEVEL_WARN,
		source:   c.pluginName + "/stderr",
		clientID: c.clientID,
	}
}

func (c *contextClient) Stdin() io.Reader {
	return &stdinReader{client: c}
}

// stdinReader implements io.Reader by calling the daemon's IOService.ReadStdin
// RPC in a loop. This lets the plugin read stdin forwarded from the CLI
// through the executor bidi stream.
type stdinReader struct {
	client *contextClient
	buf    []byte
}

func (r *stdinReader) Read(p []byte) (int, error) {
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}

	req := connect.NewRequest(&dotfilesdv1.StdinRequest{
		ClientId: r.client.clientID,
		MaxBytes: int32(len(p)),
	})
	r.client.setTokenHeader(req)
	resp, err := r.client.ioClient.ReadStdin(context.Background(), req)
	if err != nil {
		return 0, err
	}
	if len(resp.Msg.Data) == 0 && resp.Msg.Eof {
		return 0, io.EOF
	}

	// Convert \r (carriage return, Enter key in raw mode) to \n
	// for line-based plugins (games) that use bufio.ReadString('\n').
	// This conversion is intentionally NOT done for TTYConn — TUI plugins
	// that need raw terminal bytes use the PTY-backed path which bypasses
	// this reader entirely.
	data := bytes.ReplaceAll(resp.Msg.Data, []byte{'\r'}, []byte{'\n'})

	n := copy(p, data)
	if n < len(data) {
		r.buf = make([]byte, len(data)-n)
		copy(r.buf, data[n:])
	}
	return n, nil
}

// daemonLogWriter is an io.Writer that sends data as log entries to the
// daemon's IOService. Writes are buffered and flushed on newline or at
// a max buffer size to avoid excessive RPC calls.
type daemonLogWriter struct {
	client   *contextClient
	level    dotfilesdv1.LogLevel
	source   string
	clientID string

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
	attrs := map[string]string{}
	if w.clientID != "" {
		attrs["client_id"] = w.clientID
	}
	req := connect.NewRequest(&dotfilesdv1.LogRequest{
		Session: w.client.buildSession(),
		Source:  w.source,
		Entry: &dotfilesdv1.LogEntry{
			Level:      w.level,
			Message:    strings.TrimRight(line, "\r\n"),
			Attributes: attrs,
		},
	})
	w.client.setTokenHeader(req)
	// Best-effort; use background context so we don't block on log delivery.
	_, _ = w.client.ioClient.Log(context.Background(), req)
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
	_, _ = l.client.ioClient.Log(context.Background(), req)
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

func (c *contextClient) DaemonClient() *http.Client { return c.daemonHTTPClient }
func (c *contextClient) DaemonURL() string          { return c.daemonURL }

// GetSecret retrieves a secret for this plugin, encrypted with an ECDH-negotiated
// AES-256-GCM key. The caller MUST zero the returned slice after use.
func (c *contextClient) GetSecret(key string) ([]byte, error) {
	// Step 1: Negotiate shared key if not yet done.
	if !c.secretsKeyReady {
		if err := c.negotiateSecretsKey(); err != nil {
			return nil, fmt.Errorf("negotiate secrets key: %w", err)
		}
	}

	// Step 2: Request encrypted secret from daemon.
	req := connect.NewRequest(&dotfilesdv1.GetSecretRequest{
		Session:    c.buildSession(),
		PluginName: c.pluginName,
		Key:        key,
	})
	c.setTokenHeader(req)

	resp, err := c.secretsClient.GetSecret(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("get secret: %w", err)
	}

	// Step 3: Decrypt using shared key.
	keyCopy := make([]byte, 32)
	copy(keyCopy, c.secretsKey)
	defer zeroBytes(keyCopy)

	plaintext, err := decryptSecretsValue(resp.Msg.EncryptedValue, keyCopy)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: %w", err)
	}

	return plaintext, nil
}

// negotiateSecretsKey performs an ephemeral X25519 ECDH exchange with the
// daemon for key_id="secrets", storing the derived shared secret.
func (c *contextClient) negotiateSecretsKey() error {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(cryptorand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	req := connect.NewRequest(&dotfilesdv1.NegotiateKeyRequest{
		Session:         c.buildSession(),
		KeyId:           "secrets",
		ClientPublicKey: priv.PublicKey().Bytes(),
	})
	c.setTokenHeader(req)

	resp, err := c.keyClient.NegotiateKey(context.Background(), req)
	if err != nil {
		return fmt.Errorf("negotiate key: %w", err)
	}

	// Build the peer's public key from the daemon's response.
	peerPub, err := curve.NewPublicKey(resp.Msg.ServerPublicKey)
	if err != nil {
		return fmt.Errorf("invalid server public key: %w", err)
	}

	secret, err := priv.ECDH(peerPub)
	if err != nil {
		return fmt.Errorf("ecdh: %w", err)
	}

	if len(secret) != 32 {
		return fmt.Errorf("unexpected shared key length: %d", len(secret))
	}

	c.secretsKey = secret
	c.secretsKeyReady = true
	c.secretsPriv = priv
	return nil
}

// decryptSecretsValue decrypts an AES-256-GCM encrypted blob (nonce || ciphertext)
// using the given 32-byte key. The caller should zero the key after use.
func decryptSecretsValue(ciphertext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key length: %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aes gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce := ciphertext[:nonceSize]
	enc := ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, enc, nil)
}

// zeroBytes overwrites the backing array with zeros.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ─── TTY stream ────────────────────────────────────────────────────────────

// ttyConn implements TTYConn by wrapping the daemon's TtySession bidi stream.
type ttyConn struct {
	clientID      string
	client        *contextClient
	stream        *connect.BidiStreamForClient[dotfilesdv1.TtyPacket, dotfilesdv1.TtyPacket]
	readMu        sync.Mutex
	readBuf       []byte
	closed        bool
	resizeHandler func(width, height int)
}

func (c *contextClient) TtyConn() (TTYConn, error) {
	clientID := c.clientID
	if clientID == "" {
		return nil, fmt.Errorf("no client ID available (not in an interactive method call)")
	}

	stream := c.ioClient.TtySession(context.Background())
	if err := stream.Send(&dotfilesdv1.TtyPacket{ClientId: clientID}); err != nil {
		return nil, fmt.Errorf("send initial tty packet: %w", err)
	}

	return &ttyConn{
		clientID: clientID,
		client:   c,
		stream:   stream,
	}, nil
}

// OnResize registers a callback that fires when a TtyPacket with
// WindowWidth/WindowHeight arrives from the daemon (triggered by
// SIGWINCH on the CLI side).
func (t *ttyConn) OnResize(fn func(width, height int)) {
	t.readMu.Lock()
	defer t.readMu.Unlock()
	t.resizeHandler = fn
}

func (t *ttyConn) Read(p []byte) (int, error) {
	t.readMu.Lock()
	defer t.readMu.Unlock()

	if t.closed {
		return 0, fmt.Errorf("tty connection closed")
	}

	if len(t.readBuf) > 0 {
		n := copy(p, t.readBuf)
		t.readBuf = t.readBuf[n:]
		return n, nil
	}

	for {
		pkt, err := t.stream.Receive()
		if err != nil {
			return 0, err
		}
		if pkt.Eof {
			return 0, io.EOF
		}
		// Handle resize notification (WindowWidth/WindowHeight without Data).
		if pkt.WindowWidth > 0 && pkt.WindowHeight > 0 && len(pkt.Data) == 0 {
			if t.resizeHandler != nil {
				t.resizeHandler(int(pkt.WindowWidth), int(pkt.WindowHeight))
			}
			continue
		}
		if len(pkt.Data) == 0 {
			continue
		}
		n := copy(p, pkt.Data)
		if n < len(pkt.Data) {
			t.readBuf = make([]byte, len(pkt.Data)-n)
			copy(t.readBuf, pkt.Data[n:])
		}
		return n, nil
	}
}

func (t *ttyConn) Write(p []byte) (int, error) {
	if t.closed {
		return 0, fmt.Errorf("tty connection closed")
	}
	if err := t.stream.Send(&dotfilesdv1.TtyPacket{Data: p}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *ttyConn) Close() error {
	t.readMu.Lock()
	defer t.readMu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	t.stream.Send(&dotfilesdv1.TtyPacket{Eof: true})
	t.stream.CloseRequest()
	t.stream.CloseResponse()
	return nil
}

// Getsize returns 0, 0 for the base TTYConn — only the PTY-backed
// variant (ptyTtyConn) knows its actual terminal dimensions.
func (t *ttyConn) Getsize() (int, int, error) { return 0, 0, nil }

func (t *ttyConn) Resize(width, height int) error {
	if t.closed {
		return fmt.Errorf("tty connection closed")
	}
	return t.stream.Send(&dotfilesdv1.TtyPacket{
		WindowWidth:  int32(width),
		WindowHeight: int32(height),
	})
}
