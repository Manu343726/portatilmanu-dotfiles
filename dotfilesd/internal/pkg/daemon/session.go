package daemon

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
)

// daemonPort is the RPC port the daemon listens on, set at startup via SetDaemonPort().
// Used to inject DOTFILESD_PORT into shell sessions so nested dotfilesctl calls
// know where to connect.
var daemonPort string

// SetDaemonPort configures the daemon port used for injecting DOTFILESD_PORT
// into managed shell sessions.
func SetDaemonPort(port string) {
	daemonPort = port
}

type shellSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
	cwd    string
}

// buildShellEnv constructs the environment variable list for a shell session.
// It merges CLI env (from shellInfo) with daemon mandatory vars and session variables.
// Exported for testing.
func buildShellEnv(sessionID string, variables map[string]string, shellInfo *dotfilesdv1.Shell) []string {
	home := os.Getenv("HOME")

	var cmdEnv []string

	if shellInfo != nil && len(shellInfo.Env) > 0 {
		for k, v := range shellInfo.Env {
			cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
		}
	} else {
		// Fallback when no CLI env provided.
		path := os.Getenv("PATH")
		localBin := home + "/.local/bin"
		if !strings.Contains(path, localBin) {
			path = localBin + ":" + path
		}
		cmdEnv = []string{
			"HOME=" + home,
			"PATH=" + path,
		}
	}

	// Ensure PATH includes ~/.local/bin.
	localBin := home + "/.local/bin"
	pathVal := ""
	for _, e := range cmdEnv {
		if strings.HasPrefix(e, "PATH=") {
			pathVal = strings.TrimPrefix(e, "PATH=")
			break
		}
	}
	if !strings.Contains(pathVal, localBin) {
		if pathVal != "" {
			pathVal = localBin + ":" + pathVal
		} else {
			pathVal = localBin
		}
		found := false
		for i, e := range cmdEnv {
			if strings.HasPrefix(e, "PATH=") {
				cmdEnv[i] = "PATH=" + pathVal
				found = true
				break
			}
		}
		if !found {
			cmdEnv = append(cmdEnv, "PATH="+pathVal)
		}
	}

	// Override daemon mandatory vars.
	cmdEnv = append(cmdEnv,
		"DOTFILESD_DAEMON=1",
		"DOTFILESD_PORT="+daemonPort,
		"DOTFILESD_SESSION="+sessionID,
	)
	// Ensure HOME is consistently set.
	homeSet := false
	for _, e := range cmdEnv {
		if strings.HasPrefix(e, "HOME=") {
			homeSet = true
			break
		}
	}
	if !homeSet {
		cmdEnv = append(cmdEnv, "HOME="+home)
	} else {
		for i, e := range cmdEnv {
			if strings.HasPrefix(e, "HOME=") {
				cmdEnv[i] = "HOME=" + home
				break
			}
		}
	}

	// Add session variables on top (skipping internal/private ones prefixed with _).
	for k, v := range variables {
		if strings.HasPrefix(k, "_") {
			continue
		}
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}

	return cmdEnv
}

func newShellSession(sessionID string, variables map[string]string, shellInfo *dotfilesdv1.Shell) (*shellSession, error) {
	// Always use bash as the execution engine — it's predictable and reliable
	// for command capture. CLI env vars and cwd are injected from Shell info.
	cmd := execCommand("bash", "--norc", "--noprofile")

	cmd.Env = buildShellEnv(sessionID, variables, shellInfo)

	// Set working directory from CLI context.
	if shellInfo != nil && shellInfo.Cwd != "" {
		cmd.Dir = shellInfo.Cwd
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start shell: %w", err)
	}

	cwd := ""
	if shellInfo != nil {
		cwd = shellInfo.Cwd
	}
	return &shellSession{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
		cwd:    cwd,
	}, nil
}

func bashQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func (sh *shellSession) Exec(command string, variables map[string]string) (string, string, int) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	delim := fmt.Sprintf("__GS_%x__", rand.Int63())

	// Prepend cd to cwd so commands run in the CLI's working directory.
	var prefix string
	if sh.cwd != "" {
		prefix += fmt.Sprintf("cd %s\n", bashQuote(sh.cwd))
	}

	// Prepend exports for session variables so they are available in the shell.
	for k, v := range variables {
		prefix += fmt.Sprintf("export %s=%s; ", k, bashQuote(v))
	}
	cmdLine := fmt.Sprintf("%s%s 2>&1\necho \"%s=$?\"\n", prefix, command, delim)
	if _, err := io.WriteString(sh.stdin, cmdLine); err != nil {
		slog.Warn("shell write failed", "error", err)
		return "", "", -1
	}

	var output strings.Builder
	for {
		line, err := sh.reader.ReadString('\n')
		if err != nil {
			slog.Warn("shell read failed", "error", err)
			return output.String(), "", -1
		}
		line = strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(line, delim+"=") {
			codeStr := strings.TrimPrefix(line, delim+"=")
			code, err := strconv.Atoi(strings.TrimSpace(codeStr))
			if err != nil {
				return output.String(), "", -1
			}
			return output.String(), "", code
		}
		output.WriteString(line)
		output.WriteByte('\n')
	}
}

// ExecStream is like Exec but streams output line-by-line to a Connect
// server stream. Each line is sent as a stdout_chunk (stderr is merged).
// The final message has done=true with the exit code.
func (sh *shellSession) ExecStream(
	ctx context.Context,
	stream *connect.ServerStream[dotfilesdv1.ExecStreamResponse],
	command string,
	variables map[string]string,
) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	delim := fmt.Sprintf("__GS_%x__", rand.Int63())

	var prefix string
	if sh.cwd != "" {
		prefix += fmt.Sprintf("cd %s\n", bashQuote(sh.cwd))
	}
	for k, v := range variables {
		prefix += fmt.Sprintf("export %s=%s; ", k, bashQuote(v))
	}
	cmdLine := fmt.Sprintf("%s%s 2>&1\necho \"%s=$?\"\n", prefix, command, delim)
	if _, err := io.WriteString(sh.stdin, cmdLine); err != nil {
		return stream.Send(&dotfilesdv1.ExecStreamResponse{
			Done:         true,
			ExitCode:     -1,
			ErrorMessage: fmt.Sprintf("shell write: %v", err),
		})
	}

	for {
		line, err := sh.reader.ReadString('\n')
		if err != nil {
			return stream.Send(&dotfilesdv1.ExecStreamResponse{
				Done:         true,
				ExitCode:     -1,
				ErrorMessage: fmt.Sprintf("shell read: %v", err),
			})
		}
		line = strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(line, delim+"=") {
			codeStr := strings.TrimPrefix(line, delim+"=")
			code, err := strconv.Atoi(strings.TrimSpace(codeStr))
			if err != nil {
				return stream.Send(&dotfilesdv1.ExecStreamResponse{
					Done:         true,
					ExitCode:     -1,
					ErrorMessage: fmt.Sprintf("parse exit code: %v", err),
				})
			}
			return stream.Send(&dotfilesdv1.ExecStreamResponse{
				Done:     true,
				ExitCode: int32(code),
			})
		}
		// Send each line as a chunk (add back the newline).
		if err := stream.Send(&dotfilesdv1.ExecStreamResponse{
			StdoutChunk: []byte(line + "\n"),
		}); err != nil {
			return err
		}
	}
}

func (sh *shellSession) Close() error {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.cmd != nil && sh.cmd.Process != nil {
		return sh.cmd.Process.Kill()
	}
	return nil
}

type Session struct {
	id           string
	createdAt    time.Time
	lastActive   time.Time
	requestCount int
	finalized    bool
	data         map[string]string
	variables    map[string]string // session variables injected into shell env
	shell        *shellSession
	shellInfo    *dotfilesdv1.Shell // CLI shell context (cwd, shell, env)
	callbackURL  string
	mu           sync.RWMutex
}

func newSession(id string) *Session {
	now := time.Now()
	return &Session{
		id:         id,
		createdAt:  now,
		lastActive: now,
		data:       make(map[string]string),
	}
}

func (s *Session) SetVariables(vars map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.variables == nil {
		s.variables = make(map[string]string, len(vars))
	}
	for k, v := range vars {
		s.variables[k] = v
	}
}

func (s *Session) Variables() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string, len(s.variables))
	for k, v := range s.variables {
		result[k] = v
	}
	return result
}

func (s *Session) touch() {
	s.mu.Lock()
	s.lastActive = time.Now()
	s.requestCount++
	s.mu.Unlock()
}

func (s *Session) SetShellInfo(si *dotfilesdv1.Shell) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if si != nil {
		// Copy to avoid holding a reference to the request message.
		env := make(map[string]string, len(si.Env))
		for k, v := range si.Env {
			env[k] = v
		}
		s.shellInfo = &dotfilesdv1.Shell{
			CurrentShell: si.CurrentShell,
			Cwd:          si.Cwd,
			Env:          env,
		}
	}
}

func (s *Session) toProto() *dotfilesdv1.Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data := make(map[string]string, len(s.data))
	for k, v := range s.data {
		data[k] = v
	}
	vars := make(map[string]string, len(s.variables))
	for k, v := range s.variables {
		vars[k] = v
	}
	return &dotfilesdv1.Session{
		Id:           s.id,
		CreatedAt:    s.createdAt.Unix(),
		LastActive:   s.lastActive.Unix(),
		RequestCount: int32(s.requestCount),
		Finalized:    s.finalized,
		Data:         data,
		Variables:    vars,
		Shell:        s.shellInfo,
	}
}

func (s *Session) ensureShell() (*shellSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finalized {
		return nil, fmt.Errorf("session is finalized")
	}
	if s.shell == nil {
		sh, err := newShellSession(s.id, s.variables, s.shellInfo)
		if err != nil {
			return nil, err
		}
		s.shell = sh
		slog.Debug("session shell created", "session_id", s.id)
	}
	return s.shell, nil
}

func (s *Session) closeShell() {
	if s.shell != nil {
		if err := s.shell.Close(); err != nil {
			slog.Warn("error closing session shell", "session_id", s.id, "error", err)
		}
		s.shell = nil
		slog.Debug("session shell closed", "session_id", s.id)
	}
}

func (s *Session) SetCallbackURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callbackURL = url
}

func (s *Session) HasCallbackURL() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.callbackURL != ""
}

func (s *Session) RequestInput(ctx context.Context, prompt, defaultValue string, sensitive bool) (string, error) {
	s.mu.RLock()
	url := s.callbackURL
	s.mu.RUnlock()
	if url == "" {
		return "", fmt.Errorf("no callback URL registered for session %s", s.id)
	}

	client := dotfilesdv1connect.NewInputServiceClient(http.DefaultClient, url)
	req := connect.NewRequest(&dotfilesdv1.InputRequest{
		Session:   &dotfilesdv1.Session{Id: s.id},
		Prompt:    prompt,
		Default:   defaultValue,
		Sensitive: sensitive,
	})

	resp, err := client.RequestInput(ctx, req)
	if err != nil {
		return "", fmt.Errorf("request input: %w", err)
	}
	return resp.Msg.Value, nil
}

func (s *Session) RequestConfirm(ctx context.Context, message string, defaultConfirm bool) (bool, error) {
	s.mu.RLock()
	url := s.callbackURL
	s.mu.RUnlock()
	if url == "" {
		return false, fmt.Errorf("no callback URL registered for session %s", s.id)
	}

	client := dotfilesdv1connect.NewConfirmServiceClient(http.DefaultClient, url)
	req := connect.NewRequest(&dotfilesdv1.ConfirmRequest{
		Session:        &dotfilesdv1.Session{Id: s.id},
		Message:        message,
		DefaultConfirm: defaultConfirm,
	})

	resp, err := client.RequestConfirm(ctx, req)
	if err != nil {
		return false, fmt.Errorf("request confirm: %w", err)
	}
	return resp.Msg.Confirmed, nil
}

// RequestChoose asks the user to pick from a list of options via the session's
// callback URL. Returns the selected index (0-based) and option text, or -1/empty
// if the user cancelled.
func (s *Session) RequestChoose(ctx context.Context, prompt string, options []string, defaultIndex int) (int, string, error) {
	s.mu.RLock()
	url := s.callbackURL
	s.mu.RUnlock()
	if url == "" {
		return -1, "", fmt.Errorf("no callback URL registered for session %s", s.id)
	}

	client := dotfilesdv1connect.NewChooseServiceClient(http.DefaultClient, url)
	req := connect.NewRequest(&dotfilesdv1.ChooseRequest{
		Session:      &dotfilesdv1.Session{Id: s.id},
		Prompt:       prompt,
		Options:      options,
		DefaultIndex: int32(defaultIndex),
	})

	resp, err := client.RequestChoose(ctx, req)
	if err != nil {
		return -1, "", fmt.Errorf("request choose: %w", err)
	}
	return int(resp.Msg.SelectedIndex), resp.Msg.SelectedOption, nil
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
	}
}

func generateSessionID() string {
	return fmt.Sprintf("ses_%x_%x", time.Now().UnixNano(), rand.Int63())
}

func (ss *SessionStore) Create() *Session {
	id := generateSessionID()
	s := newSession(id)
	ss.mu.Lock()
	ss.sessions[id] = s
	ss.mu.Unlock()
	slog.Debug("session created", "session_id", id)
	return s
}

// CreateWithID creates a new session with the given ID. If a session with that
// ID already exists, it is returned instead.
func (ss *SessionStore) CreateWithID(id string) *Session {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if existing, ok := ss.sessions[id]; ok {
		existing.touch()
		return existing
	}
	s := newSession(id)
	ss.sessions[id] = s
	slog.Debug("session created with ID", "session_id", id)
	return s
}

func (ss *SessionStore) CreateEphemeral() *Session {
	s := newSession("")
	return s
}

// CreateNamed creates a session with the given ID. If a session with that
// ID already exists, it is returned as-is. This is used for plugin sessions.
func (ss *SessionStore) CreateNamed(id string) *Session {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if s, ok := ss.sessions[id]; ok {
		slog.Debug("session already exists, reusing", "session_id", id)
		return s
	}
	s := newSession(id)
	ss.sessions[id] = s
	slog.Debug("session created (named)", "session_id", id)
	return s
}

func (ss *SessionStore) Get(id string) *Session {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.sessions[id]
}

func (ss *SessionStore) Finalize(id string) bool {
	ss.mu.Lock()
	s, ok := ss.sessions[id]
	if !ok {
		ss.mu.Unlock()
		return false
	}
	s.mu.Lock()
	s.finalized = true
	s.closeShell()
	s.mu.Unlock()
	ss.mu.Unlock()
	slog.Debug("session finalized", "session_id", id)
	return true
}

func (ss *SessionStore) List() []*Session {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	result := make([]*Session, 0, len(ss.sessions))
	for _, s := range ss.sessions {
		s.mu.RLock()
		if !s.finalized {
			result = append(result, s)
		}
		s.mu.RUnlock()
	}
	return result
}

func (ss *SessionStore) Resolve(id string) *Session {
	if id == "" {
		return ss.CreateEphemeral()
	}
	s := ss.Get(id)
	if s == nil {
		slog.Warn("session not found, creating ephemeral", "session_id", id)
		return ss.CreateEphemeral()
	}
	if s.finalized {
		slog.Warn("session already finalized, creating ephemeral", "session_id", id)
		return ss.CreateEphemeral()
	}
	s.touch()
	return s
}

// ResolveSession resolves or creates a session from a protobuf Session message.
// If the message is nil or has an empty id, an ephemeral session is created.
// Session variables and shell context from the message are applied to the
// resolved session.
func (ss *SessionStore) ResolveSession(sessionMsg *dotfilesdv1.Session) *Session {
	if sessionMsg == nil {
		return ss.CreateEphemeral()
	}
	id := sessionMsg.GetId()
	var s *Session
	if id == "" {
		s = ss.CreateEphemeral()
	} else {
		s = ss.Get(id)
		if s == nil {
			slog.Warn("session not found, creating ephemeral", "session_id", id)
			s = ss.CreateEphemeral()
		} else if s.finalized {
			slog.Warn("session already finalized, creating ephemeral", "session_id", id)
			s = ss.CreateEphemeral()
		} else {
			s.touch()
		}
	}
	if vars := sessionMsg.GetVariables(); len(vars) > 0 {
		s.SetVariables(vars)
	}
	if shellInfo := sessionMsg.GetShell(); shellInfo != nil {
		s.SetShellInfo(shellInfo)
	}
	return s
}

type sessionServer struct {
	store *SessionStore
}

func newSessionServer(store *SessionStore) *sessionServer {
	return &sessionServer{store: store}
}

func (s *sessionServer) CreateSession(ctx context.Context, req *connect.Request[dotfilesdv1.CreateSessionRequest]) (*connect.Response[dotfilesdv1.CreateSessionResponse], error) {
	slog.Log(ctx, levelTrace, "Session.CreateSession")

	session := s.store.Create()
	resp := connect.NewResponse(&dotfilesdv1.CreateSessionResponse{
		Session: session.toProto(),
	})

	slog.Log(ctx, levelTrace, "Session.CreateSession done", "session_id", session.id)
	return resp, nil
}

func (s *sessionServer) Connect(ctx context.Context, req *connect.Request[dotfilesdv1.ConnectRequest]) (*connect.Response[dotfilesdv1.ConnectResponse], error) {
	slog.Log(ctx, levelTrace, "Session.Connect", "callback_url", req.Msg.CallbackUrl)

	sessionMsg := req.Msg.GetSession()
	var session *Session
	if sessionMsg == nil || sessionMsg.GetId() == "" {
		session = s.store.Create()
	} else {
		session = s.store.Get(sessionMsg.GetId())
		if session == nil {
			// Session doesn't exist yet — create it with the requested ID.
			session = s.store.CreateWithID(sessionMsg.GetId())
		} else {
			session.touch()
		}
	}

	// Apply session variables and shell context from the Connect request.
	if sessionMsg != nil {
		if len(sessionMsg.GetVariables()) > 0 {
			session.SetVariables(sessionMsg.GetVariables())
		}
		if shellInfo := sessionMsg.GetShell(); shellInfo != nil {
			session.SetShellInfo(shellInfo)
		}
	}

	session.SetCallbackURL(req.Msg.CallbackUrl)

	resp := connect.NewResponse(&dotfilesdv1.ConnectResponse{
		Session: session.toProto(),
	})

	slog.Log(ctx, levelTrace, "Session.Connect done", "session_id", session.id)
	return resp, nil
}

func (s *sessionServer) FinalizeSession(ctx context.Context, req *connect.Request[dotfilesdv1.FinalizeSessionRequest]) (*connect.Response[dotfilesdv1.FinalizeSessionResponse], error) {
	sessionID := ""
	if sm := req.Msg.GetSession(); sm != nil {
		sessionID = sm.GetId()
	}
	slog.Log(ctx, levelTrace, "Session.FinalizeSession", "session_id", sessionID)

	ok := s.store.Finalize(sessionID)
	if !ok {
		return connect.NewResponse(&dotfilesdv1.FinalizeSessionResponse{
			Success: false,
			Message: fmt.Sprintf("session not found: %s", sessionID),
		}), nil
	}

	return connect.NewResponse(&dotfilesdv1.FinalizeSessionResponse{
		Success: true,
		Message: fmt.Sprintf("session %s finalized", sessionID),
	}), nil
}

func (s *sessionServer) GetSession(ctx context.Context, req *connect.Request[dotfilesdv1.GetSessionRequest]) (*connect.Response[dotfilesdv1.GetSessionResponse], error) {
	sessionID := ""
	if sm := req.Msg.GetSession(); sm != nil {
		sessionID = sm.GetId()
	}
	slog.Log(ctx, levelTrace, "Session.GetSession", "session_id", sessionID)

	session := s.store.Get(sessionID)
	if session == nil {
		return connect.NewResponse(&dotfilesdv1.GetSessionResponse{}), nil
	}

	return connect.NewResponse(&dotfilesdv1.GetSessionResponse{
		Session: session.toProto(),
	}), nil
}

func (s *sessionServer) ListSessions(ctx context.Context, req *connect.Request[dotfilesdv1.ListSessionsRequest]) (*connect.Response[dotfilesdv1.ListSessionsResponse], error) {
	slog.Log(ctx, levelTrace, "Session.ListSessions")

	sessions := s.store.List()
	protoSessions := make([]*dotfilesdv1.Session, len(sessions))
	for i, session := range sessions {
		protoSessions[i] = session.toProto()
	}

	return connect.NewResponse(&dotfilesdv1.ListSessionsResponse{
		Sessions: protoSessions,
	}), nil
}
