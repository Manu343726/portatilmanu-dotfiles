package main

import (
	"dotfilesd/internal/pkg/cli"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp [subcommand]",
		Short: "MCP stdio server and client management",
		RunE: func(cmd *cobra.Command, args []string) error {
			// No subcommand: start MCP server (original behavior).
			cli.RunMCP(clients)
			return nil
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "clients",
		Short: "list connected MCP clients",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunListClients(clients)
		},
	})

	return cmd
}
