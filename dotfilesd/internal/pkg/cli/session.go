package cli

import (
	"context"
	"fmt"
	"log/slog"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

func RunCreateSession(clients *Clients) error {
	slog.Debug("creating session")
	resp, err := clients.Session.CreateSession(context.Background(), connect.NewRequest(&dotfilesdv1.CreateSessionRequest{}))
	if err != nil {
		slog.Error("create session failed", "error", err)
		return fmt.Errorf("create session failed: %w", err)
	}
	s := resp.Msg.Session
	slog.Info("session created", "session_id", s.Id)
	fmt.Printf("session: %s\n", s.Id)
	fmt.Printf("created: %d\n", s.CreatedAt)
	return nil
}

func RunFinalizeSession(clients *Clients, sessionID string) error {
	slog.Debug("finalizing session", "session_id", sessionID)
	resp, err := clients.Session.FinalizeSession(context.Background(), connect.NewRequest(&dotfilesdv1.FinalizeSessionRequest{
		Session: sessionProto(sessionID),
	}))
	if err != nil {
		slog.Error("finalize session failed", "session_id", sessionID, "error", err)
		return fmt.Errorf("finalize session failed: %w", err)
	}
	slog.Info("session finalized", "session_id", sessionID, "success", resp.Msg.Success)
	fmt.Println(resp.Msg.Message)
	if !resp.Msg.Success {
		return fmt.Errorf("session %s not found", sessionID)
	}
	return nil
}

func RunListSessions(clients *Clients) error {
	slog.Debug("listing sessions")
	resp, err := clients.Session.ListSessions(context.Background(), connect.NewRequest(&dotfilesdv1.ListSessionsRequest{Session: &dotfilesdv1.Session{}}))
	if err != nil {
		slog.Error("list sessions failed", "error", err)
		return fmt.Errorf("list sessions failed: %w", err)
	}
	if len(resp.Msg.Sessions) == 0 {
		slog.Debug("no active sessions")
		fmt.Println("no active sessions")
		return nil
	}
	slog.Debug("active sessions", "count", len(resp.Msg.Sessions))
	for _, s := range resp.Msg.Sessions {
		slog.Debug("session", "id", s.Id, "created", s.CreatedAt, "requests", s.RequestCount)
		fmt.Printf("%-24s  created=%d  requests=%d\n", s.Id, s.CreatedAt, s.RequestCount)
	}
	return nil
}
