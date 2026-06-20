package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
)

type Clients struct {
	Sys      dotfilesdv1connect.SystemServiceClient
	Dot      dotfilesdv1connect.DotfilesServiceClient
	Exec     dotfilesdv1connect.ExecServiceClient
	Cfg      dotfilesdv1connect.ConfigServiceClient
	Session  dotfilesdv1connect.SessionServiceClient
	Callback *CallbackServer
	SessionID string
	mu        sync.Mutex
	connected bool
}

func NewClients(port string) *Clients {
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", port)
	return &Clients{
		Sys:     dotfilesdv1connect.NewSystemServiceClient(http.DefaultClient, baseURL),
		Dot:     dotfilesdv1connect.NewDotfilesServiceClient(http.DefaultClient, baseURL),
		Exec:    dotfilesdv1connect.NewExecServiceClient(http.DefaultClient, baseURL),
		Cfg:     dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, baseURL),
		Session: dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, baseURL),
	}
}

func (c *Clients) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.connected {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	slog.Debug("client connecting to daemon")

	cb, err := NewCallbackServer()
	if err != nil {
		return fmt.Errorf("start callback server: %w", err)
	}
	c.Callback = cb

	req := connect.NewRequest(&dotfilesdv1.ConnectRequest{
		CallbackUrl: cb.URL(),
		SessionId:   c.SessionID,
	})
	req.Header().Set("Session-Id", c.SessionID)

	resp, err := c.Session.Connect(ctx, req)
	if err != nil {
		cb.Close()
		return fmt.Errorf("daemon connect: %w", err)
	}

	c.SessionID = resp.Msg.Session.Id
	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()

	slog.Debug("client connected", "session_id", c.SessionID, "callback_url", cb.URL())
	return nil
}

func (c *Clients) Close() {
	if c.Callback != nil {
		c.Callback.Close()
	}
}


