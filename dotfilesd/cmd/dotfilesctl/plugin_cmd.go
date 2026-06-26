package main

import (
	"context"
	"fmt"
	"strings"

	"dotfilesd/internal/pkg/cli"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
)

func newPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin [<plugin> [<tool> [<key=value>...]]]",
		Short: "List plugins or invoke plugin tools",
		Long: `List loaded plugins or invoke their tools.

Without arguments, lists all loaded plugins with their tools.
With a plugin name, shows that plugin's tools and invocation help.
With a plugin and tool name, invokes the tool with optional key=value arguments.

Examples:
  dotfilesctl plugin
  dotfilesctl plugin weather
  dotfilesctl plugin weather forecast location=Madrid
  dotfilesctl plugin weather forecast location="New York"
`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: completePluginPath,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureClients(); err != nil {
				return err
			}

			switch len(args) {
			case 0:
				// No args: list all plugins (non-verbose).
				return cli.RunListPlugins(clients, sessionID, false)
			case 1:
				// One arg: show tools for the named plugin.
				return cli.RunListPluginTools(clients, sessionID, args[0])
			default:
				// Two or more args: call a plugin tool.
				pluginName := args[0]
				toolName := args[1]

				// Parse remaining args as key=value pairs.
				flagArgs := make(map[string]string)
				for _, arg := range args[2:] {
					parts := strings.SplitN(arg, "=", 2)
					if len(parts) == 2 {
						flagArgs[parts[0]] = parts[1]
					}
				}

				return cli.RunCallPluginTool(clients, sessionID, pluginName, toolName, flagArgs)
			}
		},
	}

	return cmd
}

// completePluginPath provides shell completion for plugin and tool names.
// With no args, suggests plugin names. With one arg, suggests tool names
// for that plugin. With two or more args, no further completions.
func completePluginPath(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if clients == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var sess *dotfilesdv1.Session
	if sessionID != "" {
		sess = &dotfilesdv1.Session{Id: sessionID}
	}
	req := connect.NewRequest(&dotfilesdv1.ListPluginsRequest{Session: sess})
	resp, err := clients.Sys.ListPlugins(context.Background(), req)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	plugins := resp.Msg.Plugins

	switch len(args) {
	case 0:
		// Suggest plugin names.
		var suggestions []string
		for _, p := range plugins {
			if toComplete != "" && !strings.HasPrefix(p.Name, toComplete) {
				continue
			}
			suggestions = append(suggestions, p.Name)
		}
		return suggestions, cobra.ShellCompDirectiveNoFileComp

	case 1:
		// Suggest tool names for the given plugin.
		pluginName := args[0]
		for _, p := range plugins {
			if p.Name != pluginName {
				continue
			}
			var suggestions []string
			for _, t := range p.Tools {
				if toComplete != "" && !strings.HasPrefix(t.Name, toComplete) {
					continue
				}
				suggestions = append(suggestions, t.Name)
			}
			return suggestions, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp

	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// ensureClients checks that the clients are initialized.
func ensureClients() error {
	if clients == nil {
		return fmt.Errorf("clients not initialized")
	}
	return nil
}
