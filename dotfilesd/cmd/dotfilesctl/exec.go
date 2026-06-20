package main

import (
	"strings"

	"dotfilesd/internal/pkg/cli"
	"github.com/spf13/cobra"
)

func newExecCmd() *cobra.Command {
	var sudo bool

	cmd := &cobra.Command{
		Use:   "exec [--sudo] <command>",
		Short: "run a shell command",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := strings.Join(args, " ")
			if !sudo {
				return cli.RunExec(clients, sessionID, command)
			}
			return cli.RunSudoExec(clients, sessionID, command)
		},
	}

	cmd.Flags().BoolVar(&sudo, "sudo", false, "run with sudo (interactive password prompt in terminal)")
	return cmd
}
