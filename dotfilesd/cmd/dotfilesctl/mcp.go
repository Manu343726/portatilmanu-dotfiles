package main

import (
	"dotfilesd/internal/pkg/cli"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "start MCP stdio server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cli.RunMCP(clients)
			return nil
		},
	}
}
