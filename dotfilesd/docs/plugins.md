# Plugin System

dotfilesd supports dynamic extensions called **plugins** — standalone Go programs that
register **tools** (commands) which get automatically exposed as both `dotfilesctl` CLI
subcommands and MCP tools for AI agents.

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                     dotfilesd (daemon)                        │
│  ┌────────────────────────────────────────────────────────┐   │
│  │  Plugin Manager                                        │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐              │   │
│  │  │ Plugin A │  │ Plugin B │  │ Plugin C │  ...         │   │
│  │  │ (subproc)│  │ (subproc)│  │ (subproc)│              │   │
│  │  └────┬─────┘  └────┬─────┘  └────┬─────┘              │   │
│  │       │              │              │                    │   │
│  │  ┌────▼──────────────▼──────────────▼────────────────┐  │   │
│  │  │           Extension API (Connect RPC)              │  │   │
│  │  │   GetDescriptor → discover tools & their schema    │  │   │
│  │  │   CallTool      → invoke a tool on a plugin        │  │   │
│  │  └────────────────────────────────────────────────────┘  │   │
│  │                                                           │   │
│  │  ┌────────────────────────────────────────────────────┐  │   │
│  │  │       Execution Context (Connect RPC)               │  │   │
│  │  │   Exec     → run shell commands                     │  │   │
│  │  │   SudoExec → run commands with sudo                  │  │   │
│  │  │   RequestInput/Confirm/Choose → interact with user   │  │   │
│  │  └────────────────────────────────────────────────────┘  │   │
│  └────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────┘
```

### Key design principles

1. **Non-intrusive** — The daemon core stays simple. Plugins are separate processes that
   communicate via Connect RPC. A plugin crash never takes down the daemon.

2. **Auto-discovery** — Plugins live in `~/.config/dotfilesd/plugins/<name>/`. The daemon
   scans this directory on startup, builds any Go plugin sources, launches them, and
   queries their capabilities via the **Extension API**.

3. **Auto-generated interfaces** — Each plugin declares its tools (name, description,
   input schema, CLI hints). The daemon and CLI use this metadata to automatically
   construct CLI commands (`dotfilesctl plugin call <plugin> <tool>`) and MCP tool
   definitions (qualified as `<plugin>_<tool>`).

4. **Security isolation** — Plugins never access the daemon's core RPCs directly.
   They interact with the host through a restricted **Execution Context** proxy that
   validates each request against a per-session token.

5. **Compiled from source, cached binaries** — The daemon compiles plugin sources on
   first load and caches the binary keyed by the SHA-256 hash of all source files.
   Recompilation only happens when sources change.

## Plugin directory structure

```
~/.config/dotfilesd/plugins/
└── weather/
    ├── main.go          # Plugin entry point (package main)
    ├── go.mod           # Module definition with replace directive
    └── go.sum           # Go module checksums
```

Each plugin is a directory under `~/.config/dotfilesd/plugins/<name>/`. The directory
name becomes the plugin name used in CLI commands and MCP tool names.

### `go.mod` requirements

The plugin's `go.mod` must include a `replace` directive pointing to the dotfilesd
module on your local filesystem:

```go
module plugins/weather

go 1.26.3

require dotfilesd v0.0.0

replace dotfilesd => /home/manu343726/dotfilesd
```

After adding or changing dependencies, run `go mod tidy` in the plugin directory.

## Writing a plugin

### SDK package

The plugin SDK lives in `dotfilesd/plugin/`. Import it as:

```go
import "dotfilesd/plugin"
```

### Minimal plugin

```go
package main

import "dotfilesd/plugin"

func main() {
    plugin.Serve("hello", "Hello World", "1.0.0", "A sample plugin",
        plugin.NewTool("greet", "Greet someone",
            plugin.ToolInputSchema{
                Properties: map[string]plugin.PropertySchema{
                    "name": {Type: "string", Description: "Name to greet"},
                },
                Required: []string{"name"},
            },
            plugin.CLIHints{CommandPath: "greet"},
            func(ctx plugin.Context, args map[string]string) (string, bool, string) {
                return "Hello, " + args["name"] + "!", false, ""
            },
        ),
    )
}
```

### `plugin.Serve()`

This is the main entry point. It:
1. Reads connection info from environment variables (set by the daemon)
2. Starts a Connect RPC server on a random port
3. Writes a JSON handshake to stdout for the daemon
4. Blocks until SIGTERM/SIGINT

```go
plugin.Serve(name, displayName, version, description string, tools ...Tool)
```

### `plugin.NewTool()`

Creates a tool backed by a function:

```go
plugin.NewTool(
    name,        // Tool name (unique within the plugin)
    description, // Human-readable description
    inputSchema, // Input parameter schema (ToolInputSchema)
    cliHints,    // CLI generation hints (CLIHints)
    handlerFn,   // func(ctx Context, args map[string]string) (text string, isError bool, structuredData string)
)
```

### `ToolInputSchema`

Describes the tool's input parameters:

```go
plugin.ToolInputSchema{
    Type: "object",   // optional, defaults to "object"
    Properties: map[string]plugin.PropertySchema{
        "location": {
            Type:        "string",
            Description: "Location to get weather for",
        },
        "format": {
            Type:        "string",
            Description: "Output format",
            Default:     "brief",
        },
    },
    Required: []string{"location"},
}
```

Supported property types: `string`, `number`, `boolean`, `array`, `object`.

### `CLIHints`

Provides hints for auto-generating the CLI command structure:

```go
plugin.CLIHints{
    CommandPath: "weather forecast",  // CLI subcommand path
    Category:    "utilities",         // Grouping category
    FlagMapping: map[string]string{   // Map parameter names to CLI flags
        "location": "location",
    },
}
```

### `Context` interface

Plugin tools interact with the host through a `plugin.Context`:

```go
type Context interface {
    // Run a shell command without privilege escalation.
    Exec(cmd string) (ExecResult, error)

    // Run a shell command with sudo. The daemon handles password elicitation.
    SudoExec(cmd string) (ExecResult, error)

    // Prompt the user for arbitrary text input.
    RequestInput(prompt, defaultVal string, sensitive bool) (string, error)

    // Prompt the user for a yes/no confirmation.
    RequestConfirm(msg string, defaultConfirm bool) (bool, error)

    // Prompt the user to pick from a list of options.
    RequestChoose(prompt string, options []string, defaultIndex int) (int, string, error)
}

type ExecResult struct {
    ExitCode int
    Stdout   string
    Stderr   string
}
```

## Complete example: weather plugin

See `plugins/weather/main.go` for a complete production example that:

- Defines a `forecast` tool with `location` (required) and `format` (optional) parameters
- Uses `ctx.Exec()` to call `curl wttr.in/Location` via the daemon
- Handles errors (curl failure, empty response)
- Returns structured JSON data when `format=json`

```sh
# From the dotfilesd repo root:
ls plugins/weather/
# → main.go  go.mod  go.sum

# The daemon builds and loads it automatically.
# After restarting the daemon:
dotfilesctl plugin list
# → Name:        weather
#    Display:     Weather
#    Version:     1.0.0
#    Description: Weather forecast plugin using wttr.in
#    Tools:       forecast
```

## CLI commands

### `dotfilesctl plugin list [-v]`

Lists all loaded plugins and their tools. The `-v` flag shows detailed input schemas.

```sh
dotfilesctl plugin list
# → Name:        weather
#    Display:     Weather
#    Version:     1.0.0
#    Description: Weather forecast plugin using wttr.in
#    Tools:       forecast
#
#    1 plugin(s) loaded.

dotfilesctl plugin list -v
# → Name:        weather
#    ...
#      Tool: forecast - Get weather forecast for a location
#        Arg: location (string, required)
#        Arg: format (string)
```

### `dotfilesctl plugin call <plugin> <tool> key=value...`

Invokes a tool on a plugin.

```sh
dotfilesctl plugin call weather forecast location=Madrid
# → Weather for Madrid, Spain
#    ⛅  +22°C
#    ...

dotfilesctl plugin call weather forecast location=London format=json
# → {
#      "current_condition": [...]
#    }
```

## MCP tools

When the daemon has plugins loaded, their tools are automatically available
as MCP tools qualified with the plugin name: `<plugin>_<tool>`.

| MCP Tool | Description |
|----------|-------------|
| `weather_forecast` | Get weather forecast for a location |

Example interaction via MCP:

```json
{
  "method": "tools/call",
  "params": {
    "name": "weather_forecast",
    "arguments": {
      "location": "Tokyo",
      "format": "brief"
    }
  }
}
```

## Configuration

Add these to `~/.config/dotfilesd/config.yaml`:

```yaml
plugins_dir: ~/.config/dotfilesd/plugins     # where plugins live
plugin_cache_dir: ~/.cache/dotfilesd/plugins  # where compiled binaries are cached
```

Defaults:
- `plugins_dir`: `~/.config/dotfilesd/plugins`
- `plugin_cache_dir`: `~/.cache/dotfilesd/plugins`

## Building plugins manually

```sh
# Build a specific plugin:
cd ~/.config/dotfilesd/plugins/weather
go build -o ~/.cache/dotfilesd/plugins/weather/plugin .

# Or use the Makefile from the dotfilesd repo:
make plugin-build PLUGIN=weather
```

## Troubleshooting

### Plugin not loading

Check daemon logs:

```sh
journalctl --user -u dotfilesd -f
# Or:
cat ~/dotfilesd/logs/dotfilesd.log | grep plugin
```

Common issues:
- **Missing `replace` directive** in plugin `go.mod`
- **Plugin directory not scanned**: verify `plugins_dir` config and directory layout
- **Build failure**: check that the plugin compiles with `go build .` in its directory
- **Port conflict**: each plugin gets a random port, but ensure no firewall blocks localhost
