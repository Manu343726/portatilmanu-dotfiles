package main

import (
	"context"
	"os"
	"strings"

	"dotfilesd/internal/pkg/cli"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
)

func newScriptCmd() *cobra.Command {
	var scriptFile string

	cmd := &cobra.Command{
		Use:   "script [flags] [script text]",
		Short: "run a multi-step script with feedback directives",
		Long: `Run a script containing shell commands interleaved with feedback directives.

Directives:
  @confirm "message"                Ask for confirmation
  @input "prompt" [as VARNAME]      Request text input (default var: $_input)
  @choose "prompt" "opt1" "opt2" ... [as VARNAME]  Choose from options (default: $_choose)

You can also run registered scripts (from the daemon's scripts directory) by path.
Use "script list" to see available registered scripts.

Examples:
  dotfilesctl script 'echo "hello"'
  dotfilesctl script --file ~/myscript.dsh
  dotfilesctl script list
  dotfilesctl script run git/commit
  dotfilesctl script run git/status
  dotfilesctl script '
    echo "Starting setup..."
    @confirm "Ready to proceed?"
    @input "Enter project name:" as PROJECT
    echo "Project: $PROJECT"
  '
`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if scriptFile != "" {
				return cli.RunScriptFile(clients, sessionID, scriptFile)
			}
			if len(args) == 0 {
				info, _ := os.Stdin.Stat()
				if info != nil && (info.Mode()&os.ModeNamedPipe) != 0 {
					data, err := os.ReadFile("/dev/stdin")
					if err != nil {
						return cmd.Help()
					}
					return cli.RunScript(clients, sessionID, string(data))
				}
				return cmd.Help()
			}
			script := strings.Join(args, "\n")
			return cli.RunScript(clients, sessionID, script)
		},
	}

	cmd.Flags().StringVarP(&scriptFile, "file", "f", "", "path to script file on daemon host")

	// Built-in subcommands.
	cmd.AddCommand(newScriptListCmd())
	cmd.AddCommand(newScriptRunCmd())

	return cmd
}

// newScriptListCmd returns the "script list" subcommand.
func newScriptListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list registered scripts",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunListScripts(clients, sessionID)
		},
	}
}

// newScriptRunCmd returns the "script run <relPath>" subcommand.
func newScriptRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <relative-path>",
		Short: "run a registered script by relative path",
		Long: `Run a registered script by its relative path in the scripts tree.

Examples:
  dotfilesctl script run git/status
  dotfilesctl script run git/commit
  dotfilesctl script run system/update
`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completeRegisteredScripts(toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunRegisteredScript(clients, sessionID, args[0])
		},
	}
}

// completeRegisteredScripts provides shell completion for registered script paths.
func completeRegisteredScripts(prefix string) ([]string, cobra.ShellCompDirective) {
	if clients == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var sess *dotfilesdv1.Session
	if sessionID != "" {
		sess = &dotfilesdv1.Session{Id: sessionID}
	}
	req := connect.NewRequest(&dotfilesdv1.ListScriptsRequest{Session: sess})
	resp, err := clients.Script.ListScripts(context.Background(), req)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var suggestions []string
	var collectPaths func(entries []*dotfilesdv1.ScriptEntry, parent string)
	collectPaths = func(entries []*dotfilesdv1.ScriptEntry, parent string) {
		for _, e := range entries {
			fullPath := parent + e.Name
			if parent != "" {
				fullPath = parent + "/" + e.Name
			}
			if e.IsDirectory {
				// Add directory path as a completion (without trailing slash)
				if strings.HasPrefix(fullPath, prefix) {
					suggestions = append(suggestions, fullPath+"/")
				}
				collectPaths(e.Children, fullPath)
			} else {
				if strings.HasPrefix(fullPath, prefix) {
					suggestions = append(suggestions, fullPath)
				}
			}
		}
	}
	collectPaths(resp.Msg.Entries, "")
	return suggestions, cobra.ShellCompDirectiveNoFileComp
}
