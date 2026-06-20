package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

type Session struct {
	id           string
	createdAt    time.Time
	lastActive   time.Time
	requestCount int
	finalized    bool
	data         map[string]string
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
