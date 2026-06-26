package main

import (
	"fmt"

	"dotfilesd/internal/pkg/cli"

	"github.com/spf13/cobra"
)

// newPluginCmd returns the "plugin" list command (core builtin).
// Plugin tools are registered as top-level commands by registerDynamicCommands.
func newPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "List loaded plugins and their tools",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureClients(); err != nil {
				return err
			}
			return cli.RunListPlugins(clients, sessionID, false)
		},
	}

	return cmd
}

// ensureClients checks that the clients are initialized.
func ensureClients() error {
	if clients == nil {
		return fmt.Errorf("clients not initialized")
	}
	return nil
}
