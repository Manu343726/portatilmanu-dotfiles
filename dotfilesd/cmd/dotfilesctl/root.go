package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"dotfilesd/internal/pkg/cli"
	"dotfilesd/internal/pkg/shared"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	logLevel  string
	verbose   bool
	noVerify  bool
	port      string
	sessionID string
	clients   *cli.Clients
)

// registerDynamicCommands connects to the daemon and registers plugin services
// and registered scripts as top-level cobra commands. This is best-effort: if
// the daemon is unreachable, only the static core commands are available.
func registerDynamicCommands(root *cobra.Command, daemonPort string) {
	dynClients := cli.NewClients(daemonPort)
	if err := dynClients.Connect(context.Background()); err != nil {
		slog.Debug("daemon not reachable, skipping dynamic command registration", "port", daemonPort)
		return
	}
	defer dynClients.Close()
	slog.Debug("connected to daemon for dynamic command registration")

	// Register plugin list commands (one per plugin) from the registry.
	pluginResp, err := dynClients.Registry.ListPlugins(context.Background(), connect.NewRequest(&dotfilesdv1.RegistryListPluginsRequest{}))
	if err == nil {
		for _, p := range pluginResp.Msg.Plugins {
			info := cli.PluginRegistryInfo{
				Name:        p.Name,
				DisplayName: p.DisplayName,
				Version:     p.Version,
				Description: p.Description,
				URL:         p.Url,
				Services:    p.Services,
				Schemas:     p.Schemas,
				DaemonURL:   fmt.Sprintf("http://127.0.0.1:%s", daemonPort),
			}
			pluginCmd := cli.BuildPluginCommand(info)
			pluginCmd.GroupID = "plugins"
			root.AddCommand(pluginCmd)
		}
	} else {
		slog.Debug("failed to fetch plugins for command registration", "error", err)
	}

	// Fetch script tree.
	scriptResp, err := dynClients.Script.ListScripts(context.Background(), connect.NewRequest(&dotfilesdv1.ListScriptsRequest{}))
	if err == nil {
		for _, entry := range scriptResp.Msg.Entries {
			registerScriptCommand(root, dynClients, entry, true)
		}
	} else {
		slog.Debug("failed to fetch scripts for command registration", "error", err)
	}
}

// registerScriptCommand recursively creates cobra commands from the script
// entry tree. parent is the command to which the entry should be added.
// isTopLevel indicates whether this is a root-level entry (controls GroupID).
func registerScriptCommand(parent *cobra.Command, dynClients *cli.Clients, entry *dotfilesdv1.ScriptEntry, isTopLevel bool) {
	name := entry.Name
	scriptPath := entry.Path

	// Check for name conflicts by iterating Commands() directly.
	if hasCommand(parent, name) {
		slog.Debug("skipping script command, name conflict", "script", name)
		return
	}

	desc := entry.Description
	if desc == "" {
		desc = fmt.Sprintf("Script %q", scriptPath)
	}

	if entry.IsDirectory {
		dirCmd := &cobra.Command{
			Use:   name,
			Short: desc,
			RunE: func(cmd *cobra.Command, args []string) error {
				return cmd.Help()
			},
		}
		if isTopLevel {
			dirCmd.GroupID = "scripts"
		}
		for _, child := range entry.Children {
			registerScriptCommand(dirCmd, dynClients, child, false)
		}
		parent.AddCommand(dirCmd)
	} else {
		leafCmd := &cobra.Command{
			Use:   name,
			Short: desc,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return cli.RunRegisteredScript(clients, sessionID, scriptPath)
			},
		}
		if isTopLevel {
			leafCmd.GroupID = "scripts"
		}
		parent.AddCommand(leafCmd)
	}
}

// hasCommand checks if parent already has a registered subcommand with the
// given name. We iterate Commands() directly instead of using Find() because
// Find() treats commands with Run/RunE as terminal, returning no error even
// when the named subcommand doesn't exist.
func hasCommand(parent *cobra.Command, name string) bool {
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return true
		}
	}
	return false
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dotfilesctl",
		Short:   "dotfiles runtime CLI",
		GroupID: "core",
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
			if !cmd.Flags().Changed("log-level") && !cmd.Flags().Changed("verbose") {
				logLevel = viper.GetString("log-level")
				if logLevel == "" {
					logLevel = os.Getenv("DOTFILESCTL_LOG_LEVEL")
				}
			}
			// --verbose is a shorthand for --log-level debug.
			if verbose && !cmd.Flags().Changed("log-level") {
				logLevel = "debug"
			}
			if logLevel == "" {
				logLevel = "info"
			}

			cli.SetupLogging(logLevel)
			if port == "" {
				port = os.Getenv("DOTFILESD_PORT")
				if port == "" {
					port = "9105"
				}
			}

			// Inherit session from environment when running inside a daemon-managed shell.
			// Skip for 'exec' which needs its own shell to avoid deadlock when nesting.
			if cmd.Name() != "exec" && !cmd.Flags().Changed("session") {
				if envSession := os.Getenv("DOTFILESD_SESSION"); envSession != "" {
					sessionID = envSession
				}
			}

			clients = cli.NewClients(port)
			clients.SessionID = sessionID

			// Capture client context for diagnostics enrichment.
			clients.ClientType = "cli"
			clients.CommandPath = cmd.CommandPath()
			if pwd, err := os.Getwd(); err == nil {
				clients.PWD = pwd
			}

			// Skip daemon connect in MCP mode — tools connect lazily on first use.
			if cmd.Name() != "mcp" {
				if err := clients.Connect(context.Background()); err != nil {
					return err
				}
				sessionID = clients.SessionID
			} else {
				clients.ClientType = "mcp"
			}
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			// Close client connection to flush client_disconnect and session_end
			// diagnostic events so the tree doesn't accumulate stale "active" nodes.
			if clients != nil {
				clients.Close()
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	// Define command groups (order here determines display order).
	cmd.AddGroup(&cobra.Group{ID: "core", Title: "Builtin/Core:"})
	cmd.AddGroup(&cobra.Group{ID: "plugins", Title: "Plugin Tools:"})
	cmd.AddGroup(&cobra.Group{ID: "scripts", Title: "Scripts:"})

	cmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "", "log level: trace|debug|info|warn|error (default info)")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "shorthand for --log-level debug")
	cmd.PersistentFlags().BoolVar(&noVerify, "no-verify", false, "skip source version check")
	cmd.PersistentFlags().StringVarP(&port, "port", "p", "", "daemon port (default DOTFILESD_PORT env or 9105)")
	cmd.PersistentFlags().StringVar(&sessionID, "session", "", "session ID for grouping requests")

	// Core builtin commands.
	coreCmds := []*cobra.Command{
		newVersionCmd(),
		newSystemCmd(),
		newDotfilesCmd(),
		newExecCmd(),
		newConfigCmd(),
		newMCPCmd(),
		newSessionCmd(),
		newScriptCmd(),
		newPluginCmd(),
	}
	for _, c := range coreCmds {
		c.GroupID = "core"
		cmd.AddCommand(c)
	}

	return cmd
}
