package daemon

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
)

type shellSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
}

func newShellSession() (*shellSession, error) {
	cmd := exec.Command("bash", "--norc", "--noprofile")
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
	return &shellSession{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
	}, nil
}

func (sh *shellSession) Exec(command string) (string, string, int) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	delim := fmt.Sprintf("__GS_%x__", rand.Int63())
	cmdLine := fmt.Sprintf("%s 2>&1\necho \"%s=$?\"\n", command, delim)
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
	shell        *shellSession
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

func (s *Session) touch() {
	s.mu.Lock()
	s.lastActive = time.Now()
	s.requestCount++
	s.mu.Unlock()
}

func (s *Session) toProto() *dotfilesdv1.Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data := make(map[string]string, len(s.data))
	for k, v := range s.data {
		data[k] = v
	}
	return &dotfilesdv1.Session{
		Id:           s.id,
		CreatedAt:    s.createdAt.Unix(),
		LastActive:   s.lastActive.Unix(),
		RequestCount: int32(s.requestCount),
		Finalized:    s.finalized,
		Data:         data,
	}
}

func (s *Session) ensureShell() (*shellSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finalized {
		return nil, fmt.Errorf("session is finalized")
	}
	if s.shell == nil {
		sh, err := newShellSession()
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
		Prompt:    prompt,
		Default:   defaultValue,
		Sensitive: sensitive,
	})
	req.Header().Set("Session-Id", s.id)

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
		Message:        message,
		DefaultConfirm: defaultConfirm,
	})
	req.Header().Set("Session-Id", s.id)

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
		Prompt:       prompt,
		Options:      options,
		DefaultIndex: int32(defaultIndex),
	})
	req.Header().Set("Session-Id", s.id)

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

func (ss *SessionStore) CreateEphemeral() *Session {
	s := newSession("")
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

func GetSessionID[T any](req *connect.Request[T]) string {
	if id := req.Header().Get("Session-Id"); id != "" {
		return id
	}
	return ""
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
	slog.Log(ctx, levelTrace, "Session.Connect", "session_id", req.Msg.SessionId, "callback_url", req.Msg.CallbackUrl)

	var session *Session
	if req.Msg.SessionId == "" {
		session = s.store.Create()
	} else {
		session = s.store.Get(req.Msg.SessionId)
		if session == nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %s not found", req.Msg.SessionId))
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
	slog.Log(ctx, levelTrace, "Session.FinalizeSession", "session_id", req.Msg.SessionId)

	ok := s.store.Finalize(req.Msg.SessionId)
	if !ok {
		return connect.NewResponse(&dotfilesdv1.FinalizeSessionResponse{
			Success: false,
			Message: fmt.Sprintf("session not found: %s", req.Msg.SessionId),
		}), nil
	}

	return connect.NewResponse(&dotfilesdv1.FinalizeSessionResponse{
		Success: true,
		Message: fmt.Sprintf("session %s finalized", req.Msg.SessionId),
	}), nil
}

func (s *sessionServer) GetSession(ctx context.Context, req *connect.Request[dotfilesdv1.GetSessionRequest]) (*connect.Response[dotfilesdv1.GetSessionResponse], error) {
	slog.Log(ctx, levelTrace, "Session.GetSession", "session_id", req.Msg.SessionId)

	session := s.store.Get(req.Msg.SessionId)
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
