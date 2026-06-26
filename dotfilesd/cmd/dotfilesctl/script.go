package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"dotfilesd/internal/pkg/cli"

	"github.com/spf13/cobra"
)

// newScriptCmd returns the "script" command (core builtin) for running
// one-off scripts, listing registered scripts, or running a registered
// script by path as a fallback when the name conflicts with a builtin.
func newScriptCmd() *cobra.Command {
	var scriptFile string
	var scriptInline string
	var scriptStdin bool

	cmd := &cobra.Command{
		Use:   "script [flags] [<group>/.../]<script>",
		Short: "List registered scripts or run one-off scripts",
		Long: `Run a one-off script (file, inline, stdin), run a registered script by path,
or list all registered scripts.

Registered scripts are also available as top-level commands (e.g.
"dotfilesctl git status"). Use this command with a script path as a fallback
when the script name conflicts with a builtin command.

Flags (mutually exclusive):
  --file FILE    Run a script from FILE on the daemon host
  --inline STR   Run STR as an inline script
  --stdin        Read script text from stdin

Without any flag, positional arguments specify a registered script path
(e.g., "system update"). If no flags and no arguments are given, lists all
registered scripts.

Directives (in inline, file, and stdin scripts):
  @confirm "message"                Ask for confirmation
  @input "prompt" [as VARNAME]      Request text input (default var: $_input)
  @choose "prompt" "opt1" "opt2" ... [as VARNAME]  Choose from options (default: $_choose)

Examples:
  dotfilesctl script                          List registered scripts
  dotfilesctl script system update            Run shadowed script by path
  dotfilesctl script --file ~/script.dsh      Run a script file from the daemon host
  dotfilesctl script --inline 'echo "hello"'  Run an inline script
  echo 'echo "hello"' | dotfilesctl script --stdin
`,
		Args: cobra.ArbitraryArgs,
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
