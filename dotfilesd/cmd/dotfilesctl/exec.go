package main

import (
	"strings"

	"dotfilesd/internal/pkg/cli"
	"github.com/spf13/cobra"
)

func newExecCmd() *cobra.Command {
	var sudo bool
	var noNewline bool
	var sudoTimeoutSec int32

	cmd := &cobra.Command{
		Use:   "exec [flags] <command>",
		Short: "run a shell command",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := strings.Join(args, " ")
			if !sudo {
				return cli.RunExec(clients, sessionID, command, noNewline, sudoTimeoutSec)
			}
			return cli.RunSudoExec(clients, sessionID, command, noNewline, sudoTimeoutSec)
		},
	}

	cmd.Flags().BoolVar(&sudo, "sudo", false, "run with sudo (interactive password prompt in terminal)")
	cmd.Flags().BoolVarP(&noNewline, "no-newline", "n", false, "omit trailing newline from output")
	cmd.Flags().Int32Var(&sudoTimeoutSec, "sudo-timeout", 0, "sudo credential cache TTL in seconds (0 = daemon default)")
	return cmd
}
