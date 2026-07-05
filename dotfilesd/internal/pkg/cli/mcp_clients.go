package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

func RunListClients(clients *Clients) error {
	slog.Debug("listing MCP clients")

	resp, err := clients.Session.ListSessions(context.Background(), connect.NewRequest(&dotfilesdv1.ListSessionsRequest{Session: &dotfilesdv1.Session{}}))
	if err != nil {
		slog.Error("list sessions failed", "error", err)
		return fmt.Errorf("list sessions failed: %w", err)
	}

	// Filter to sessions that have a callback URL (connected clients).
	var connected []*dotfilesdv1.Session
	for _, s := range resp.Msg.Sessions {
		if s.GetData()["_callback_url"] != "" {
			connected = append(connected, s)
		}
	}

	if len(connected) == 0 {
		fmt.Println("no connected clients")
		return nil
	}

	now := time.Now()
	for _, s := range connected {
		host, port := parseCallbackAddr(s.GetData()["_callback_url"])
		clientName := s.GetVariables()["_cap_client_name"]
		if clientName == "" {
			clientName = "-"
		}
		clientVersion := s.GetVariables()["_cap_client_version"]
		if clientVersion == "" {
			clientVersion = "-"
		}
		createdAt := time.Unix(s.GetCreatedAt(), 0)
		duration := now.Sub(createdAt).Round(time.Second)

		fmt.Printf("%-16s  %-5s  %-12s  %-8s  %s  (%s)\n",
			host, port, clientName, clientVersion,
			createdAt.Format("2006-01-02 15:04:05"),
			formatDuration(duration),
		)
	}

	return nil
}

// parseCallbackAddr extracts host and port from a callback URL
// like "http://127.0.0.1:43291".
func parseCallbackAddr(callbackURL string) (host, port string) {
	u, err := url.Parse(callbackURL)
	if err != nil {
		return callbackURL, "-"
	}
	host = u.Hostname()
	port = u.Port()
	if port == "" {
		port = "-"
	}
	return host, port
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}


