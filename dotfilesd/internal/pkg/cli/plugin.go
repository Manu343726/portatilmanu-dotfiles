package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// RunListPlugins lists all loaded plugins and their tools.
func RunListPlugins(clients *Clients, sessionID string, verbose bool) error {
	slog.Debug("list plugins requested", "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.ListPluginsRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Sys.ListPlugins(context.Background(), req)
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
			for _, t := range p.Tools {
				fmt.Printf("  Tool: %s - %s\n", t.Name, t.Description)
				if t.Input != nil {
					for name, prop := range t.Input.Properties {
						req := prop.Type
						if containsString(t.Input.Required, name) {
							req += " (required)"
						}
						fmt.Printf("    Arg: %s (%s)\n", name, req)
						if prop.Description != "" {
							fmt.Printf("         %s\n", prop.Description)
						}
					}
				}
			}
		} else {
			names := make([]string, len(p.Tools))
			for j, t := range p.Tools {
				names[j] = t.Name
			}
			if len(names) > 0 {
				fmt.Printf("Tools:       %s\n", strings.Join(names, ", "))
			}
		}
	}
	fmt.Printf("\n%d plugin(s) loaded.\n", len(plugins))
	return nil
}

// RunCallPluginTool invokes a tool on a plugin.
func RunCallPluginTool(clients *Clients, sessionID, pluginName, toolName string, args map[string]string) error {
	slog.Debug("call plugin tool", "plugin", pluginName, "tool", toolName, "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.CallPluginToolRequest{
		Session:    sessionProto(sessionID),
		PluginName: pluginName,
		ToolName:   toolName,
		Arguments:  args,
	})
	resp, err := clients.Sys.CallPluginTool(context.Background(), req)
	if err != nil {
		slog.Error("call plugin tool failed", "error", err)
		fmt.Fprintf(os.Stderr, "error: call plugin tool: %v\n", err)
		return fmt.Errorf("call plugin tool: %w", err)
	}

	if resp.Msg.IsError {
		fmt.Fprintln(os.Stderr, resp.Msg.Text)
		return fmt.Errorf(resp.Msg.Text)
	}
	fmt.Println(resp.Msg.Text)
	if resp.Msg.StructuredData != "" {
		fmt.Println("---")
		fmt.Println(resp.Msg.StructuredData)
	}
	return nil
}

// ListPluginTools returns all plugin tool definitions from the daemon for
// dynamic MCP tool registration.
func ListPluginTools(clients *Clients, sessionID string) ([]toolDef, error) {
	req := connect.NewRequest(&dotfilesdv1.ListPluginsRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Sys.ListPlugins(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("list plugin tools: %w", err)
	}

	var tools []toolDef
	for _, p := range resp.Msg.Plugins {
		for _, t := range p.Tools {
			schema := toolSchema{
				Type:       "object",
				Properties: make(map[string]propSchema),
			}
			if t.Input != nil {
				if t.Input.Type != "" {
					schema.Type = t.Input.Type
				}
				for k, v := range t.Input.Properties {
					schema.Properties[k] = propSchema{
						Type:        v.Type,
						Description: v.Description,
						Enum:        v.Enum,
					}
				}
				schema.Required = t.Input.Required
			}

			// Qualify tool name with plugin prefix for MCP.
			qualifiedName := p.Name + "_" + t.Name

			desc := t.Description
			if desc == "" {
				desc = fmt.Sprintf("Plugin %q tool %q", p.Name, t.Name)
			}

			tools = append(tools, toolDef{
				Name:        qualifiedName,
				Description: desc,
				InputSchema: schema,
			})
		}
	}
	return tools, nil
}

// CallPluginToolViaMCP dispatches an MCP tool call to a plugin tool.
// The tool name is expected to be in the format "<plugin>_<tool>".
// Returns (text, isError, structuredData, error).
func CallPluginToolViaMCP(clients *Clients, sessionID, qualifiedName string, args map[string]string) (string, bool, string, error) {
	// Parse plugin name and tool name from qualified name.
	// Format: "<plugin>_<tool>"
	parts := splitQualifiedName(qualifiedName)
	if len(parts) < 2 {
		return "", false, "", fmt.Errorf("invalid qualified tool name %q (expected <plugin>_<tool>)", qualifiedName)
	}
	pluginName := parts[0]
	toolName := parts[1]

	req := connect.NewRequest(&dotfilesdv1.CallPluginToolRequest{
		Session:    sessionProto(sessionID),
		PluginName: pluginName,
		ToolName:   toolName,
		Arguments:  args,
	})
	resp, err := clients.Sys.CallPluginTool(context.Background(), req)
	if err != nil {
		return "", false, "", err
	}

	return resp.Msg.Text, resp.Msg.IsError, resp.Msg.StructuredData, nil
}

// splitQualifiedName splits "foo_bar_baz" into ["foo", "bar_baz"].
// The plugin name is the first segment; the rest is the tool name.
func splitQualifiedName(name string) []string {
	idx := strings.Index(name, "_")
	if idx < 0 {
		return []string{name}
	}
	return []string{name[:idx], name[idx+1:]}
}

// RunListPluginTools shows tools for a single plugin.
func RunListPluginTools(clients *Clients, sessionID, pluginName string) error {
	slog.Debug("list plugin tools requested", "plugin", pluginName, "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.ListPluginsRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Sys.ListPlugins(context.Background(), req)
	if err != nil {
		return fmt.Errorf("list plugins failed: %w", err)
	}

	for _, p := range resp.Msg.Plugins {
		if p.Name != pluginName {
			continue
		}

		fmt.Printf("Plugin: %s (%s v%s)\n", p.Name, p.DisplayName, p.Version)
		if p.Description != "" {
			fmt.Printf("  %s\n\n", p.Description)
		}
		if len(p.Tools) == 0 {
			fmt.Println("  No tools exposed.")
			return nil
		}
		fmt.Println("Tools:")
		for _, t := range p.Tools {
			fmt.Printf("  %s", t.Name)
			if t.Description != "" {
				fmt.Printf(" - %s", t.Description)
			}
			fmt.Println()
			if t.Input != nil && len(t.Input.Properties) > 0 {
				for name, prop := range t.Input.Properties {
					reqTag := prop.Type
					if containsString(t.Input.Required, name) {
						reqTag += " (required)"
					}
					fmt.Printf("    %s: %s\n", name, reqTag)
					if prop.Description != "" {
						fmt.Printf("      %s\n", prop.Description)
					}
				}
			}
		}
		return nil
	}

	fmt.Fprintf(os.Stderr, "error: plugin %q not found\n", pluginName)
	return fmt.Errorf("plugin %q not found", pluginName)
}

// containsString checks if a string is in a slice.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
