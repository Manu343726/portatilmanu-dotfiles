package main

import (
	"fmt"
	"strings"

	"dotfilesd/internal/pkg/cli"

	"github.com/spf13/cobra"
)

func newPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage and invoke plugin extensions",
		Long: `Manage and invoke plugin extensions.

Plugins are Go programs compiled at daemon startup that extend dotfilesd
with additional tools. Use this command to list loaded plugins and
invoke their tools manually.

See 'dotfilesctl plugin list' for available plugins and tools.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newPluginListCmd())
	cmd.AddCommand(newPluginCallCmd())

	return cmd
}

func newPluginListCmd() *cobra.Command {
	verbose := false
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List loaded plugins and their tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureClients(); err != nil {
				return err
			}
			return cli.RunListPlugins(clients, sessionID, verbose)
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show detailed tool information")
	return cmd
}

func newPluginCallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "call <plugin> <tool> [args...]",
		Short: "Call a tool on a plugin",
		Long: `Call a tool on a plugin with key=value arguments.

Example:
  dotfilesctl plugin call weather forecast location=Madrid
  dotfilesctl plugin call weather forecast location="New York"`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureClients(); err != nil {
				return err
			}
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
		},
	}
	return cmd
}

// ensureClients connects clients if not already connected.
func ensureClients() error {
	if clients == nil {
		return fmt.Errorf("clients not initialized")
	}
	return nil
}
