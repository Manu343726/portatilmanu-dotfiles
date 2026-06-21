package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"dotfilesd/internal/pkg/cli"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
)

func newScriptCmd() *cobra.Command {
	var scriptFile string
	var scriptInline string
	var scriptStdin bool

	cmd := &cobra.Command{
		Use:   "script [flags] [<group>/.../]<script>",
		Short: "Run or list scripts",
		Long: `Run a builtin registered script, a script file, or an inline script.

Flags (mutually exclusive):
  --file FILE    Run a script from FILE on the daemon host
  --inline STR   Run STR as an inline script
  --stdin        Read script text from stdin

Without any flag, positional arguments denote a registered script path
(e.g., "git status" runs the git/status registered script).
If no flags and no arguments are given, lists all registered scripts.

Directives (in inline, file, and stdin scripts):
  @confirm "message"                Ask for confirmation
  @input "prompt" [as VARNAME]      Request text input (default var: $_input)
  @choose "prompt" "opt1" "opt2" ... [as VARNAME]  Choose from options (default: $_choose)

Examples:
  dotfilesctl script                          List registered scripts
  dotfilesctl script git status               Run the git/status registered script
  dotfilesctl script git commit               Run the git/commit registered script
  dotfilesctl script system update            Run the system/update registered script
  dotfilesctl script --file ~/script.dsh      Run a script file from the daemon host
  dotfilesctl script --inline 'echo "hello"'  Run an inline script
  echo 'echo "hello"' | dotfilesctl script --stdin
`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: completeScriptPath,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate mutually exclusive flags.
			modeCount := 0
			if scriptFile != "" {
				modeCount++
			}
			if scriptInline != "" {
				modeCount++
			}
			if scriptStdin {
				modeCount++
			}
			if modeCount > 1 {
				return fmt.Errorf("--file, --inline, and --stdin are mutually exclusive")
			}

			switch {
			case scriptFile != "":
				return cli.RunScriptFile(clients, sessionID, scriptFile)

			case scriptInline != "":
				return cli.RunScript(clients, sessionID, scriptInline)

			case scriptStdin:
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				return cli.RunScript(clients, sessionID, string(data))

			case len(args) > 0:
				scriptPath := strings.Join(args, "/")
				return cli.RunRegisteredScript(clients, sessionID, scriptPath)

			default:
				return cli.RunListScripts(clients, sessionID)
			}
		},
	}

	cmd.Flags().StringVarP(&scriptFile, "file", "f", "", "path to a script file on the daemon host")
	cmd.Flags().StringVar(&scriptInline, "inline", "", "inline script text to run")
	cmd.Flags().BoolVar(&scriptStdin, "stdin", false, "read script from stdin")

	return cmd
}

// completeScriptPath provides shell completion for registered script paths.
// Args are treated as path components separated by "/", so "script git <TAB>"
// shows the scripts inside the git/ directory.
func completeScriptPath(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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

	// Navigate to the node at the path given by already-completed args.
	var children []*dotfilesdv1.ScriptEntry
	if len(args) == 0 {
		children = resp.Msg.Entries
	} else {
		node := findScriptNode(resp.Msg.Entries, args)
		if node == nil || !node.IsDirectory {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		children = node.Children
	}

	var suggestions []string
	for _, child := range children {
		if toComplete != "" && !strings.HasPrefix(child.Name, toComplete) {
			continue
		}
		if child.IsDirectory {
			suggestions = append(suggestions, child.Name+"/")
		} else {
			suggestions = append(suggestions, child.Name)
		}
	}
	return suggestions, cobra.ShellCompDirectiveNoFileComp
}

// findScriptNode navigates the script entry tree following the path parts.
func findScriptNode(entries []*dotfilesdv1.ScriptEntry, parts []string) *dotfilesdv1.ScriptEntry {
	if len(parts) == 0 {
		return nil
	}
	for _, e := range entries {
		if e.Name == parts[0] {
			if len(parts) == 1 {
				return e
			}
			if e.IsDirectory {
				return findScriptNode(e.Children, parts[1:])
			}
			return nil
		}
	}
	return nil
}
