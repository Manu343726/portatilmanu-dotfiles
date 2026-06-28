package main

import (
	"fmt"

	"dotfilesd/internal/pkg/cli"

	"github.com/spf13/cobra"
)

// newPluginCmd returns the "plugin" command with subcommands.
// Plugin tools are registered as top-level commands by registerDynamicCommands.
func newPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "List and manage loaded plugins",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List loaded plugins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureClients(); err != nil {
				return err
			}
			return cli.RunListPlugins(clients, sessionID, false)
		},
	}

	loadCmd := &cobra.Command{
		Use:   "load <name>",
		Short: "Load a plugin by name (loads dependencies too)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureClients(); err != nil {
				return err
			}
			return cli.RunLoadPlugin(clients, args[0])
		},
	}

	unloadCmd := &cobra.Command{
		Use:   "unload <name>",
		Short: "Unload a plugin by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureClients(); err != nil {
				return err
			}
			return cli.RunUnloadPlugin(clients, args[0])
		},
	}

	reloadCmd := &cobra.Command{
		Use:   "reload",
		Short: "Rescan plugins directory, load new and unload stale plugins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureClients(); err != nil {
				return err
			}
			return cli.RunReloadPlugins(clients)
		},
	}

	cmd.AddCommand(listCmd, loadCmd, unloadCmd, reloadCmd)
	return cmd
}

// ensureClients checks that the clients are initialized.
func ensureClients() error {
	if clients == nil {
		return fmt.Errorf("clients not initialized")
	}
	return nil
}
