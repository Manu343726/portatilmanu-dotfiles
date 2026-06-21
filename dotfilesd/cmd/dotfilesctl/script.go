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
Use "script list" to see available registered scripts. Path components are
space-separated, not slash-separated — use "run git status" not "run git/status".

Examples:
  dotfilesctl script 'echo "hello"'
  dotfilesctl script --file ~/myscript.dsh
  dotfilesctl script list
  dotfilesctl script run git status
  dotfilesctl script run git commit
  dotfilesctl script run system update
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

// newScriptRunCmd returns the "script run <path...>" subcommand.
func newScriptRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <directory>... <script>",
		Short: "run a registered script by path (space-separated)",
		Long: `Run a registered script by its path in the scripts tree.
Path components are space-separated (not slash-separated).

Examples:
  dotfilesctl script run git status
  dotfilesctl script run git commit
  dotfilesctl script run system update
`,
		Args: cobra.MinimumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completeRegisteredScripts(args, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			path := strings.Join(args, "/")
			return cli.RunRegisteredScript(clients, sessionID, path)
		},
	}
}

// completeRegisteredScripts provides shell completion for registered script paths.
// args holds the already-completed positional args (path components typed so far),
// toComplete is the current word being completed.
func completeRegisteredScripts(args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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

	// Build the parent prefix from already-completed args.
	parentPrefix := strings.Join(args, "/")

	// Find the subtree matching parentPrefix, then suggest children filtered by toComplete.
	var suggestions []string
	var findAndSuggest func(entries []*dotfilesdv1.ScriptEntry, prefix string)
	findAndSuggest = func(entries []*dotfilesdv1.ScriptEntry, prefix string) {
		for _, e := range entries {
			childPath := e.Name
			if prefix != "" {
				childPath = prefix + "/" + e.Name
			}

			if childPath == parentPrefix || prefix == parentPrefix {
				// We're at the target parent — suggest its direct children.
				if e.IsDirectory {
					for _, c := range e.Children {
						if strings.HasPrefix(c.Name, toComplete) {
							if c.IsDirectory {
								suggestions = append(suggestions, c.Name+"/")
							} else {
								suggestions = append(suggestions, c.Name)
							}
						}
					}
				}
				return
			}

			if strings.HasPrefix(parentPrefix, childPath+"/") || strings.HasPrefix(parentPrefix, childPath) {
				findAndSuggest(e.Children, childPath)
			}
		}
	}

	// If no args yet, suggest top-level entries.
	if parentPrefix == "" {
		for _, e := range resp.Msg.Entries {
			if strings.HasPrefix(e.Name, toComplete) {
				if e.IsDirectory {
					suggestions = append(suggestions, e.Name+"/")
				} else {
					suggestions = append(suggestions, e.Name)
				}
			}
		}
	} else {
		findAndSuggest(resp.Msg.Entries, "")
	}

	return suggestions, cobra.ShellCompDirectiveNoFileComp
}
