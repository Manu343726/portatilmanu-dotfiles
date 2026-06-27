package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// RunListPlugins lists all loaded plugins and their services.
func RunListPlugins(clients *Clients, sessionID string, verbose bool) error {
	slog.Debug("list plugins requested", "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.RegistryListPluginsRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Registry.ListPlugins(context.Background(), req)
	if err != nil {
		slog.Error("list plugins failed", "error", err)
		return fmt.Errorf("list plugins failed: %w", err)
	}

	plugins := resp.Msg.Plugins
	if len(plugins) == 0 {
		fmt.Println("No plugins loaded.")
		return nil
	}

	for i, p := range plugins {
		if i > 0 {
			fmt.Println("---")
		}
		fmt.Printf("Name:        %s\n", p.Name)
		fmt.Printf("Display:     %s\n", p.DisplayName)
		fmt.Printf("Version:     %s\n", p.Version)
		fmt.Printf("Description: %s\n", p.Description)
		if verbose {
			if len(p.Services) > 0 {
				fmt.Println("  Services:")
				for _, svc := range p.Services {
					fmt.Printf("    %s\n", svc)
				}
			} else {
				fmt.Println("  No custom services")
			}
		} else {
			if len(p.Services) > 0 {
				fmt.Printf("Services:    %s\n", strings.Join(p.Services, ", "))
			} else {
				fmt.Println("Services:    (none)")
			}
		}
	}
	fmt.Printf("\n%d plugin(s) loaded.\n", len(plugins))
	return nil
}
