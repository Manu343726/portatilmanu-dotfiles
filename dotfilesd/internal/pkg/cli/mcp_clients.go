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

	// Filter to MCP client sessions (have _display_name set by the daemon)
	// and exclude our own CLI session.
	mySessionID := clients.SessionID
	var connected []*dotfilesdv1.Session
	for _, s := range resp.Msg.Sessions {
		if s.GetId() == mySessionID {
			continue
		}
		if s.GetData()["_display_name"] != "" || s.GetData()["_callback_url"] != "" {
			connected = append(connected, s)
		}
	}

	if len(connected) == 0 {
		fmt.Println("no connected clients")
		return nil
	}

	now := time.Now()
	fmt.Printf("%-48s  %s  %s\n", "CLIENT", "CONNECTED", "DURATION")
	for _, s := range connected {
		displayName := s.GetData()["_display_name"]
		if displayName == "" {
			// Fallback: construct from available fields.
			host, port := parseCallbackAddr(s.GetData()["_callback_url"])
			name := s.GetVariables()["_cap_client_name"]
			if name == "" {
				name = "-"
			}
			version := s.GetVariables()["_cap_client_version"]
			if version == "" {
				version = "-"
			}
			displayName = name + "-" + version + "-" + host + ":" + port
		}
		createdAt := time.Unix(s.GetCreatedAt(), 0)
		duration := now.Sub(createdAt).Round(time.Second)

		fmt.Printf("%-48s  %s  (%s)\n",
			displayName,
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


