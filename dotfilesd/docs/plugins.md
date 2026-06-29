# Plugin System

> **Status:** Active — Connect RPC architecture (implemented)
>
> See [`plugin-rpc-architecture.md`](plugin-rpc-architecture.md) for the full
> design specification.

Plugins are standalone Go programs that serve **Connect RPC services** — methods
auto-exposed as `dotfilesctl` CLI subcommands (with typed flags from proto schemas)
and per-method MCP tools for AI agents.

## Architecture

```
~/.config/dotfilesd/plugins/<name>/   →   compiled →   subprocess   →   grpcreflect
                                      (builder,       (runtime,         →
                                       hash cache,     supervisor)       daemon discovers
                                       proto comp)                       ALL services)
```

The daemon scans `~/.config/dotfilesd/plugins/`, compiles proto files if present,
builds Go sources, launches binaries, reads the handshake JSON from stdout, and
discovers all services via `grpcreflect.NewClient().ListServices()`. Server-type
plugins are supervised with automatic restart on crash.

### Plugin types

| Type | Behavior | Use case |
|------|----------|----------|
| `server` (default) | Long-lived, supervised (auto-restart on crash) | Most plugins — weather, resources |
| `command` | Ephemeral — launched per invocation, not supervised | One-shot tasks |

Controlled by the `Type` field in the plugin's `Config` struct.

## Directory structure

```
~/.config/dotfilesd/plugins/
└── weather/
    ├── main.go          # Plugin entry point (package main)
    ├── go.mod           # Must have replace directive to dotfilesd
    ├── go.sum
    └── proto/
        └── weather/     # Optional: proto files for this plugin
            └── weather.proto
```

## `go.mod` setup

```go
module plugins/weather

go 1.26.3

require dotfilesd v0.0.0
replace dotfilesd => /home/manu343726/dotfilesd
```

Optionally, plugins can depend on other plugins via `require` directives in
`go.mod`. The daemon performs dependency-aware topological sort when building.

## Writing a plugin

Plugins serve **Connect RPC services** discovered via gRPC reflection.

```go
package main

import (
    "context"
    "dotfilesd/plugin"
    pb "plugins/weather/proto/weather"
    "plugins/weather/proto/weather/weatherconnect"
    "connectrpc.com/connect"
)

type weatherService struct{}

func (s *weatherService) Forecast(ctx context.Context, req *connect.Request[pb.ForecastRequest]) (*connect.Response[pb.ForecastResponse], error) {
    pc := plugin.ExtractContext(ctx)
    if pc != nil {
        pc.Log().Info("forecasting", "loc", req.Msg.Location)
        result, _ := pc.Exec("curl -s wttr.in/" + req.Msg.Location + "?0")
        return connect.NewResponse(&pb.ForecastResponse{Report: result.Stdout}), nil
    }
    return connect.NewResponse(&pb.ForecastResponse{ErrorMessage: "no context"}), nil
}

func main() {
    svc := &weatherService{}
    path, handler := weatherconnect.NewWeatherServiceHandler(svc)
    plugin.Serve(plugin.Config{
        Name:        "weather",
        DisplayName: "Weather",
        Version:     "1.0.0",
        Description: "Fetches weather data from wttr.in",
        Services: []plugin.Service{
            {Name: "weather.WeatherService", Description: "Weather forecast API",
             Path: path, Handler: handler},
        },
    })
}
```

## SDK Reference

### `plugin.Config` — Plugin server configuration

```go
type Config struct {
    Name, DisplayName, Version, Description, Author string
    Type     string   // "server" or "command"
    Services []Service
    Background func(ctx Context, stop <-chan struct{})
}
```

### `plugin.Serve(cfg Config)` — Entry point

Starts HTTP server on random port. Auto-mounts:
- `grpc.reflection.v1.ServerReflection` / `v1alpha` — daemon discovers all services
- `dotfilesd.v1.DocumentationService` — default implementation (can be overridden)
- All user services from `Config.Services`

Writes handshake JSON to stdout:
```json
{"protocol":"dotfilesd-plugin-v1","url":"http://127.0.0.1:PORT",
 "session_id":"plugin-weather","name":"weather","version":"1.0.0",
 "description":"Weather forecast"}
```

### `plugin.Context` — Interface to the daemon

```go
type Context interface {
    Stdout() io.Writer
    Stderr() io.Writer
    Stdin() io.Reader
    Log() logging.Logger
    RenderOutput() bool
    DiagParent() string
    WithRenderOutput(bool) Context

    // Colored output
    ColorStdout() io.Writer
    Greenf(...) string / Redf(...) / Bluef(...) / etc.

    // Shell execution
    Exec(cmd string) (ExecResult, error)
    SudoExec(cmd string) (ExecResult, error)
    ExecStream(cmd string, sudo bool) (int, error)
    BackgroundExec(cmd string, sudo bool) (BackgroundTask, error)

    // User interaction
    RequestInput(prompt, defaultVal string, sensitive bool) (string, error)
    RequestConfirm(msg string, defaultConfirm bool) (bool, error)
    RequestChoose(prompt string, options []string, defaultIndex int) (int, string, error)

    // Scripts
    RunScript(name string) (ScriptResult, error)
}
```

### `plugin.ExtractContext(ctx) Context`

Extracts the daemon context from a Connect RPC handler's `context.Context`.
Returns nil if not running inside a daemon-managed plugin process.

### `BackgroundTask` — Interactive command control

```go
task, err := ctx.BackgroundExec("python3 -i", false)

// Write to stdin
io.WriteString(task.Stdin(), "print('hello')\n")
task.Stdin().Close()

// Wait for completion
exitCode, err := task.Wait()

// Or cancel
task.Cancel()
```

### Plugin-to-plugin calls

Plugin B discovers plugin A via the daemon's `PluginRegistryService` at runtime,
then calls plugin A using generated Connect clients (full type safety):

```go
regClient := dotfilesdv1connect.NewPluginRegistryServiceClient(http.DefaultClient, daemonURL)
info, _ := regClient.GetPlugin(ctx, &connect.Request{
    Msg: &dotfilesdv1.RegistryGetPluginRequest{PluginName: "weather"},
})
weatherClient := weatherconnect.NewWeatherServiceClient(http.DefaultClient, info.Msg.Url)
forecast, _ := weatherClient.Forecast(ctx, &connect.Request{
    Msg: &pb.ForecastRequest{Location: "Madrid"},
})
```

## Building plugins

```sh
make plugin-build PLUGIN=weather     # build a specific plugin (compiles proto first)
make plugin-build-all                # build all plugins in dependency order
make plugin-clean                    # clear plugin cache
make plugin-proto PLUGIN=weather     # compile proto files for a plugin
```

Binaries are cached in `~/.cache/dotfilesd/plugins/<name>/` keyed by source hash.
Rebuilds only happen when sources change.

## Debugging

```sh
dotfilesctl plugin list              # all plugins and services
dotfilesctl plugin list -v           # verbose, with input schemas
dotfilesctl plugin load <name>       # load a plugin dynamically
dotfilesctl plugin unload <name>     # unload a plugin
dotfilesctl plugin reload            # rescan plugins directory

# Tail plugin logs
tail -f ~/dotfilesd/logs/dotfilesd.log | grep "plugin."
```
