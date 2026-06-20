package cli

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

func RunCreateSession(clients *Clients) error {
	resp, err := clients.Session.CreateSession(context.Background(), connect.NewRequest(&dotfilesdv1.CreateSessionRequest{}))
	if err != nil {
		return fmt.Errorf("create session failed: %w", err)
	}
	s := resp.Msg.Session
	fmt.Printf("session: %s\n", s.Id)
	fmt.Printf("created: %d\n", s.CreatedAt)
	return nil
}

func RunFinalizeSession(clients *Clients, sessionID string) error {
	resp, err := clients.Session.FinalizeSession(context.Background(), connect.NewRequest(&dotfilesdv1.FinalizeSessionRequest{
		SessionId: sessionID,
	}))
	if err != nil {
		return fmt.Errorf("finalize session failed: %w", err)
	}
	fmt.Println(resp.Msg.Message)
	if !resp.Msg.Success {
		return fmt.Errorf("session %s not found", sessionID)
	}
	return nil
}

func RunListSessions(clients *Clients) error {
	resp, err := clients.Session.ListSessions(context.Background(), connect.NewRequest(&dotfilesdv1.ListSessionsRequest{}))
	if err != nil {
		return fmt.Errorf("list sessions failed: %w", err)
	}
	if len(resp.Msg.Sessions) == 0 {
		fmt.Println("no active sessions")
		return nil
	}
	for _, s := range resp.Msg.Sessions {
		fmt.Printf("%-24s  created=%d  requests=%d\n", s.Id, s.CreatedAt, s.RequestCount)
	}
	return nil
}
