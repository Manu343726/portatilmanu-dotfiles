package main

import (
	"context"
	"fmt"
	"os"

	"dotfilesd/internal/pkg/cli"
	"dotfilesd/internal/pkg/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	verbose   bool
	noVerify  bool
	port      string
	sessionID string
	clients   *cli.Clients
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dotfilesctl",
		Short: "dotfiles runtime CLI",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			shared.CheckBuildHash(buildHash, noVerify, "dotfilesctl")

			viper.SetConfigName("config")
			viper.SetConfigType("yaml")
			viper.AddConfigPath("$HOME/.config/dotfilesctl")
			viper.AutomaticEnv()
			viper.SetEnvPrefix("DOTFILESCTL")

			if err := viper.ReadInConfig(); err != nil {
				if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
					fmt.Fprintf(os.Stderr, "config error: %v\n", err)
				}
			}

			if !cmd.Flags().Changed("port") {
				port = viper.GetString("port")
			}
			if !cmd.Flags().Changed("verbose") {
				verbose = viper.GetBool("verbose")
			}

			cli.SetupLogging(verbose)
			if port == "" {
				port = os.Getenv("DOTFILESD_PORT")
				if port == "" {
					port = "9105"
				}
			}
			clients = cli.NewClients(port)
			clients.SessionID = sessionID

			// Skip daemon connect in MCP mode — tools connect lazily on first use.
			if cmd.Name() != "mcp" {
				if err := clients.Connect(context.Background()); err != nil {
					return err
				}
				sessionID = clients.SessionID
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose logging to stderr")
	cmd.PersistentFlags().BoolVar(&noVerify, "no-verify", false, "skip source version check")
	cmd.PersistentFlags().StringVarP(&port, "port", "p", "", "daemon port (default DOTFILESD_PORT env or 9105)")
	cmd.PersistentFlags().StringVar(&sessionID, "session", "", "session ID for grouping requests")

	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newSystemCmd())
	cmd.AddCommand(newDotfilesCmd())
	cmd.AddCommand(newExecCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newMCPCmd())
	cmd.AddCommand(newSessionCmd())

	return cmd
}
