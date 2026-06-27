# Plugin System

Plugins are standalone Go programs that register **tools** — commands auto-exposed as `dotfilesctl` CLI subcommands and MCP tools for AI agents.

## Architecture

```
~/.config/dotfilesd/plugins/<name>/   →   compiled →   subprocess   →   Connect RPC
                                      (builder,       (runtime,         (Extension
                                       hash cache)     supervisor)       API)
```

The daemon scans `~/.config/dotfilesd/plugins/`, builds Go sources, launches binaries, and queries their capabilities via the Extension API. Server-type plugins are supervised with automatic restart on crash.

### Plugin types

| Type | Behavior | Use case |
|------|----------|----------|
| `server` (default) | Long-lived, supervised (auto-restart on crash) | Most plugins — weather, resources |
| `command` | Ephemeral — launched per invocation, not supervised | One-shot tasks |

Controlled by the README.md key `type: command` in the plugin directory.

## Directory structure

```
~/.config/dotfilesd/plugins/
└── weather/
    ├── main.go          # Plugin entry point (package main)
    ├── go.mod           # Must have replace directive to dotfilesd
    └── go.sum
```

## `go.mod` setup

```go
module plugins/weather

go 1.26.3

require dotfilesd v0.0.0
replace dotfilesd => /home/manu343726/dotfilesd
```

## Writing a plugin

```go
package main

import (
    "fmt"
    "dotfilesd/plugin"
    dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

func main() {
    plugin.Serve("weather", "Weather Plugin", "1.0.0",
        "Fetches weather data from wttr.in",
        plugin.NewTool("forecast", "Get weather forecast for a location",
            &dotfilesdv1.ToolInputSchema{
                Properties: map[string]*dotfilesdv1.PropertySchema{
                    "location": {
                        Type:        dotfilesdv1.PropertyType_PROPERTY_TYPE_STRING,
                        Description: "City name, ZIP code, or IP address",
                    },
                },
                Required: []string{"location"},
            },
            &dotfilesdv1.CLIHints{
                CommandPath: "weather forecast",
            },
            func(ctx plugin.Context, args map[string]string) error {
                result, err := ctx.Exec("curl -s 'wttr.in/" + args["location"] + "?format=%c+%t'")
                if err != nil {
                    return err
                }
                fmt.Fprintln(ctx.Stdout(), result.Stdout)
                return nil
            },
        ),
    )
}
```

## SDK Reference

### `plugin.Context` — the plugin's interface to the host

The `Context` is the ONLY way a plugin interacts with the system. Plugins never call daemon RPCs directly.

#### Execution

| Method | Description |
|--------|-------------|
| `Exec(cmd string) (ExecResult, error)` | Run a shell command, return buffered result |
| `SudoExec(cmd string) (ExecResult, error)` | Run with sudo (password handled by daemon) |
| `ExecStream(cmd string, sudo bool) (int, error)` | Run and stream output in real time to Stdout()/Stderr() |
| `BackgroundExec(cmd string, sudo bool) (BackgroundTask, error)` | Start a background command with stdin/stdout/cancel/tee |

#### Plugin-to-plugin calls

| Method | Description |
|--------|-------------|
| `CallPlugin(name, tool, args) (ExecResult, error)` | Invoke another plugin's tool, return buffered result |
| `CallPluginStream(name, tool, args) error` | Invoke another plugin's tool, pipe output to Stdout()/Stderr() in real time |

#### Scripts

| Method | Description |
|--------|-------------|
| `RunScript(name string) (ScriptResult, error)` | Run a registered script (e.g. `"git/status"`) |

#### User interaction

| Method | Description |
|--------|-------------|
| `RequestInput(prompt, default, sensitive) (string, error)` | Ask user for text input |
| `RequestConfirm(msg, defaultConfirm) (bool, error)` | Ask user yes/no |
| `RequestChoose(prompt, options, defaultIdx) (int, string, error)` | Ask user to pick from options |

#### Output and logging

| Method | Description |
|--------|-------------|
| `Stdout() io.Writer` | Write to the caller's stdout (tunnels via RPC stream) |
| `Stderr() io.Writer` | Write to the caller's stderr |
| `Log() logging.Logger` | Structured logging routed through the daemon |

### `BackgroundTask` — interactive command control

```go
task, err := ctx.BackgroundExec("python3 -i", false)

// Write to stdin
io.WriteString(task.Stdin(), "print('hello')\n")
task.Stdin().Close()

// Tee: stream to user AND get readers for processing
stdoutR, stderrR := task.Tee()
go processStdout(stdoutR)

// Wait for completion
exitCode, err := task.Wait()

// Or cancel
task.Cancel()
```

### `ServeWithBackground` — persistent background worker

```go
plugin.ServeWithBackground("monitor", "Monitor", "1.0.0",
    "Continuous system monitoring",
    func(ctx plugin.Context, stop <-chan struct{}) {
        for {
            select {
            case <-stop:
                return
            case <-time.After(5 * time.Second):
                ctx.Exec("...")
            }
        }
    },
    plugin.NewTool("status", "Show current status", nil, nil, statusFn),
)
```

## Building plugins

```sh
make plugin-build PLUGIN=weather     # build a specific plugin
make plugin-build-all                # build all plugins
make plugin-clean                    # clear plugin cache
```

Binaries are cached in `~/.cache/dotfilesd/plugins/<name>/` keyed by source hash. Rebuilds only happen when sources change.

## Debugging

```sh
dotfilesctl plugin list          # list all plugins and tools
dotfilesctl plugin list -v       # verbose, with input schemas
dotfilesctl plugin tree          # show directory hierarchy
dotfilesctl plugin list-tools <name>  # show tools for one plugin

# Tail plugin logs
tail -f ~/dotfilesd/logs/dotfilesd.log | grep "plugin."
```
