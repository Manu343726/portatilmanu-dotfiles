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

// RunLoadPlugin loads a plugin by name (and its dependencies).
func RunLoadPlugin(clients *Clients, pluginName string) error {
	if pluginName == "" {
		return fmt.Errorf("plugin name is required")
	}
	slog.Debug("load plugin requested", "plugin", pluginName)
	req := connect.NewRequest(&dotfilesdv1.RegistryLoadPluginRequest{PluginName: pluginName})
	resp, err := clients.Registry.LoadPlugin(context.Background(), req)
	if err != nil {
		return fmt.Errorf("load plugin: %w", err)
	}
	if resp.Msg.Error != "" {
		return fmt.Errorf("load plugin: %s", resp.Msg.Error)
	}
	p := resp.Msg.Plugin
	if p != nil {
		fmt.Printf("Loaded %s v%s (%s)\n", p.DisplayName, p.Version, p.Url)
	}
	if len(resp.Msg.LoadedDeps) > 0 {
		fmt.Printf("Dependencies loaded: %s\n", strings.Join(resp.Msg.LoadedDeps, ", "))
	}
	return nil
}

// RunUnloadPlugin unloads a plugin by name.
func RunUnloadPlugin(clients *Clients, pluginName string) error {
	if pluginName == "" {
		return fmt.Errorf("plugin name is required")
	}
	slog.Debug("unload plugin requested", "plugin", pluginName)
	req := connect.NewRequest(&dotfilesdv1.RegistryUnloadPluginRequest{PluginName: pluginName})
	resp, err := clients.Registry.UnloadPlugin(context.Background(), req)
	if err != nil {
		return fmt.Errorf("unload plugin: %w", err)
	}
	if resp.Msg.Error != "" {
		return fmt.Errorf("unload plugin: %s", resp.Msg.Error)
	}
	fmt.Printf("Unloaded %s\n", pluginName)
	return nil
}

// RunReloadPlugins rescans the plugins directory, loading new and unloading stale.
func RunReloadPlugins(clients *Clients) error {
	slog.Debug("reload plugins requested")
	req := connect.NewRequest(&dotfilesdv1.RegistryReloadPluginsRequest{})
	resp, err := clients.Registry.ReloadPlugins(context.Background(), req)
	if err != nil {
		return fmt.Errorf("reload plugins: %w", err)
	}
	if resp.Msg.Error != "" {
		return fmt.Errorf("reload plugins: %s", resp.Msg.Error)
	}
	if len(resp.Msg.Loaded) > 0 {
		fmt.Printf("Loaded: %s\n", strings.Join(resp.Msg.Loaded, ", "))
	}
	if len(resp.Msg.Unloaded) > 0 {
		fmt.Printf("Unloaded: %s\n", strings.Join(resp.Msg.Unloaded, ", "))
	}
	if len(resp.Msg.Loaded) == 0 && len(resp.Msg.Unloaded) == 0 {
		fmt.Println("No changes.")
	}
	return nil
}
