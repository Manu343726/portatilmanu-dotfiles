package main

import (
	"dotfilesd/internal/pkg/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "show version info",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			version.Print("dotfilesctl")
			return nil
		},
	}
}
