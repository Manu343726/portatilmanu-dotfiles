# Plugin RPC Architecture — Full Specification

> **Status:** Design Document
> **Date:** 2026-06-27
> **Purpose:** Reference for implementing type-safe Connect RPC for plugins

> **⚠️ COMPLETE REWRITE — ZERO BACKWARDS COMPATIBILITY**
>
> This document describes a **from-scratch rewrite** of the entire plugin
> architecture. The old `Tool`-based system, `plugin.proto`, `extension.proto`,
> `plugin_base.proto`, stringly-typed dispatch, and all existing plugin code
> are **deleted and replaced**. Nothing from the v1 plugin system survives.
>
> - Old plugins **will not work** — every existing plugin (weather, resources)
>   must be rewritten from scratch as a Connect RPC service.
> - The old SDK (`plugin.NewTool`, `plugin.RegisterTool`, etc.) is **gone** —
>   replaced by `plugin.Serve()` with Connect RPC handlers and protobuf.
> - There is **no migration path, no compatibility shim, no adapter layer.**
> - This is a clean break: the new system is designed as if the old one never
>   existed.
>
> All existing plugin code in `~/.config/dotfilesd/plugins/` will be replaced
> in a single coordinated commit that also updates the daemon, SDK, CLI, and
> build system.

## Table of Contents

1. [Overview](#1-overview)
2. [Two Libraries: grpcreflect vs grpcurl](#2-two-libraries-grpcreflect-vs-grpcurl)
3. [Plugin SDK Public API](#3-plugin-sdk-public-api)
4. [Plugin Identity (Handshake)](#4-plugin-identity-handshake)
5. [Service Discovery (grpcreflect)](#5-service-discovery-grpcreflect)
   - [CLI Command & MCP Tool Mapping](#5a-cli-command--mcp-tool-mapping)
6. [DocumentationService Plugin Protocol](#6-documentationservice-plugin-protocol)
7. [Daemon-Side Plugin Registry](#7-daemon-side-plugin-registry)
8. [Daemon Plugin Manager](#8-daemon-plugin-manager)
9. [Plugin Discovery & Registration Flow](#9-plugin-discovery--registration-flow)
10. [Plugin-to-Plugin Type-Safe Calls](#10-plugin-to-plugin-type-safe-calls)
11. [Plugin Definition (Proto + Code)](#11-plugin-definition-proto--code)
12. [Build Dependency Graph](#12-build-dependency-graph)
13. [Complete Proto Files](#13-complete-proto-files)
14. [SDK Implementation Details](#14-sdk-implementation-details)
15. [Files to Create/Modify](#15-files-to-createmodify)
16. [Implementation Order](#16-implementation-order)

---

## 1. Overview

> This is a **clean-sheet redesign**. The old plugin system (Tool-based, stringly-typed
> dispatch, `extension.proto`, `plugin.proto`, `plugin_base.proto`) is entirely deleted.
> Nothing carries over — not a single line of plugin code, not a single proto message,
> not a single SDK function. The system below is designed as if from scratch.

```
┌──────────────────────────────────────────────────────────────────────┐
│                    AI Agent / dotfilesctl CLI                      │
│  Discovers plugins and their services via daemon's                  │
│  PluginRegistryService (daemon-known proto).                        │
└───────────────────────┬──────────────────────────────────────────────┘
                        │ Connect RPC over HTTP (port 9105)
                        ▼
┌──────────────────────────────────────────────────────────────────────┐
│                    dotfilesd (daemon)                                 │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  Plugin Manager                                               │   │
│  │  1. Scan plugins/ dir, parse go.mod deps                     │   │
│  │  2. Topological sort → build order                            │   │
│  │  3. For each: build → launch → read handshake →               │   │
│  │     grpcreflect client .ListServices() → ALL service names    │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  PluginRegistryService (daemon-known proto)                    │   │
│  │  GetPlugin(name) → URL + services                             │   │
│  │  ListPlugins() → all plugins                                   │   │
│  └──────────────────────────────────────────────────────────────┘   │
└──────────────────────────┬───────────────────────────────────────────┘
                           │ launches subprocesses
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  Plugin Processes (standalone Go binaries, localhost HTTP)          │
│                                                                     │
│  Serves on random port via plugin.Serve():                          │
│    • grpcreflect.NewHandlerV1 + V1Alpha  (auto-mounted by SDK)     │
│    • DocumentationService handler          (auto-mounted by SDK)    │
│    • All user-defined services             (from Config.Services)   │
│                                                                     │
│  The daemon discovers EVERYTHING via grpcreflect client.            │
└──────────────────────────────────────────────────────────────────────┘
```

### Key Principles

1. **Plugins serve Connect RPC services.** The old `Tool` abstraction is gone.
   Plugins are just Connect RPC servers with protobuf schemas.
2. **grpcreflect** is used BOTH server-side (plugin mounts reflection handlers)
   and client-side (daemon calls `ListServices()` to discover everything).
3. **Plugin identity** comes from the handshake JSON on stdout (name, version, etc.)
4. **Daemon has NO compile-time knowledge of plugin service schemas.** It discovers
   everything at runtime via grpcreflect.
5. **DocumentationService** is a standard proto compiled into both daemon and SDK.
   Plugins get a default implementation; they can override for richer docs.
6. **No backwards compatibility.** The old plugin system is deleted in its entirety.
   No migration path, no adapter layer, no compatibility shim.

---

## 2. Two Libraries: grpcreflect vs grpcurl

### `connectrpc.com/grpcreflect` (used in this architecture)

**Purpose:** Server reflection for Connect servers — implements the [gRPC Server Reflection Protocol](https://github.com/grpc/grpc/blob/master/src/proto/grpc/reflection/v1/reflection.proto).

**Provides:**
- **Server handlers** (`NewHandlerV1`, `NewHandlerV1Alpha`): Mount on your HTTP mux so tools can discover services without the schema.
- **Client** (`grpcreflect.NewClient`): Call `ListServices()` to discover all service names on a remote server.

The SDK mounts the handlers automatically. The daemon uses the client to discover plugin services.

```go
// Server-side (plugin SDK auto-mounts this):
import "connectrpc.com/grpcreflect"

mux.Handle(grpcreflect.NewHandlerV1(reflector))
mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))

// Client-side (daemon plugin manager):
httpClient := &http.Client{}; client := grpcreflect.NewClient(httpClient, pluginURL)
stream := client.NewStream(ctx)
defer stream.Close()
services, err := stream.ListServices()
// Result: ["dotfilesd.v1.DocumentationService", "weather.WeatherService", ...]
```

### `github.com/fullstorydev/grpcurl` (NOT used in this architecture)

**Purpose:** CLI tool (like cURL for gRPC) with a library for dynamic RPC invocation.

**Provides:**
- CLI: `grpcurl -plaintext localhost:PORT list`
- Library: Functions for dynamically invoking RPCs with JSON-encoded messages

Not needed by the daemon or SDK. Useful for debugging plugins manually:
```sh
grpcurl -plaintext 127.0.0.1:12345 list
# → dotfilesd.v1.DocumentationService
# → weather.WeatherService

grpcurl -plaintext 127.0.0.1:12345 describe weather.WeatherService
# Detailed proto description
```

---

## 3. Plugin SDK Public API

### `plugin.Service` — Register a custom RPC service

```go
type Service struct {
    Name        string      // e.g. "weather.WeatherService"
    Description string      // Human-readable, used in docs
    Path        string      // HTTP path from generated handler
    Handler     http.Handler
}
```

### `plugin.Config` — Plugin server configuration

```go
type Config struct {
    Name, DisplayName, Version, Description string
    Type     string   // "server" or "command"
    Services []Service

    // Optional background worker. Runs after HTTP server starts.
    Background func(ctx Context, stop <-chan struct{})
}
```

### `plugin.Serve(cfg Config)` — Entry point

Starts the HTTP server on a random port. **Automatically mounts:**

| Service | Source | Purpose |
|---------|--------|---------|
| `grpc.reflection.v1.ServerReflection` | grpcreflect | Daemon discovers ALL services |
| `grpc.reflection.v1alpha.ServerReflection` | grpcreflect | Older tools compatibility |
| `dotfilesd.v1.DocumentationService` | SDK | CLI help, MCP descriptions |
| User services from `cfg.Services` | plugin | Type-safe RPCs |

Handshake written to stdout:
```json
{"protocol":"dotfilesd-plugin-v1","url":"http://127.0.0.1:PORT",
 "session_id":"plugin-weather","name":"weather","version":"1.0.0",
 "description":"Weather forecast"}
```

### `plugin.Context` — Daemon-facing API

```go
type Context interface {
    Stdout() io.Writer
    Stderr() io.Writer
    Log() logging.Logger
    Exec(cmd string) (ExecResult, error)
    SudoExec(cmd string) (ExecResult, error)
    ExecStream(cmd string, sudo bool) (int, error)
    BackgroundExec(cmd string, sudo bool) (BackgroundTask, error)
    RequestInput(prompt, default string, sensitive bool) (string, error)
    RequestConfirm(msg string, defaultConfirm bool) (bool, error)
    RequestChoose(prompt, options []string, defaultIndex int) (int, string, error)
    RunScript(name string) (ScriptResult, error)

    // RenderOutput returns true if the caller expects human-readable
    // formatted output written to Stdout().
    RenderOutput() bool

    // WithRenderOutput returns a child Context that forwards the render
    // preference to downstream calls (e.g. plugin-to-plugin).
    WithRenderOutput(bool) Context
}
```

### `plugin.ExtractContext(ctx) Context`

For custom RPC handlers that receive a plain `context.Context`:

```go
func (s *weatherServer) Forecast(ctx context.Context, req *connect.Request[pb.ForecastRequest]) (*connect.Response[pb.ForecastResponse], error) {
    pc := plugin.ExtractContext(ctx)
    if pc != nil {
        pc.Log().Info("forecasting", "loc", req.Msg.Location)
        result, _ := pc.Exec("curl wttr.in/" + req.Msg.Location)
    }
    return connect.NewResponse(&pb.ForecastResponse{Report: result.Stdout}), nil
}
```

### `plugin.RenderOutput` — Output formatting flag

The `Context` has a `RenderOutput` method that controls whether a plugin RPC
handler should produce human-readable formatted output (for CLI users) or return
raw data (for programmatic plugin-to-plugin calls).

```go
type Context interface {
    // ... (same as before) ...

    // RenderOutput returns true if the caller expects human-readable formatted
    // output written to Stdout(). When false, handlers should return raw data
    // in the RPC response message for programmatic consumption.
    RenderOutput() bool
}
```

**Default behavior:**
| Caller | `RenderOutput()` | Why |
|--------|-----------------|------|
| **CLI** (`dotfilesctl`) | `true` | User expects human-readable formatted output |
| **MCP bridge** (`dotfilesctl mcp`) | `false` | MCP protocol is JSON-based; the protobuf response message is serialized to JSON automatically. The agent can read the structured result directly from the tool response |
| Plugin A → Plugin B via generated client | `false` | Plugin A processes the response programmatically (e.g. extracts fields, filters) |
| Plugin A → Plugin B with explicit override | as requested | `ctx.WithRenderOutput(true)` delegates formatting; `ctx.WithRenderOutput(false)` forces raw data |

**Both CLI and MCP can override the default:**
- CLI: `dotfilesctl weather forecast --location=Madrid --format=json` — if the CLI detects a `--format=json` or `--json` flag on the command, it sets `RenderOutput=false` so the plugin returns JSON data and the CLI serializes it directly
- MCP: The tool call parameters can include an optional `_render_output` field. When set to `true`, the MCP bridge forwards `X-Dotfiles-Render-Output: true`, and the plugin's pretty-printed output is included in the tool result alongside the JSON payload

**Usage in a plugin handler:**

```go
func (s *weatherServer) Forecast(ctx context.Context, req *connect.Request[pb.ForecastRequest]) (*connect.Response[pb.ForecastResponse], error) {
    pc := plugin.ExtractContext(ctx)
    data, _ := fetchWeather(req.Msg.Location, req.Msg.Format)

    if pc != nil && pc.RenderOutput() {
        // CLI call: format and write to stdout.
        fmt.Fprintf(pc.Stdout(), "Weather report: %s\n", data)
        fmt.Fprintf(pc.Stdout(), "Temperature: %s\n", extractTemp(data))
        return connect.NewResponse(&pb.ForecastResponse{Report: data}), nil
    }

    // Plugin-to-plugin call: return raw data only.
    return connect.NewResponse(&pb.ForecastResponse{Report: data}), nil
}
```

**Plugin A enabling output when calling Plugin B:**

```go
// In plugin A's handler:
func handlerA(ctx plugin.Context, args map[string]string) error {
    // plugin A calls plugin B, wants B's formatted output.
    bClient := weatherconnect.NewWeatherServiceClient(httpClient, bURL)

    // Create a child context with RenderOutput enabled.
    callCtx := ctx.WithRenderOutput(true)
    // This forwards the rendering request through the bidi stream headers.

    forecast, _ := bClient.Forecast(callCtx, &connect.Request{
        Msg: &pb.ForecastRequest{Location: "Madrid"},
    })
    // Plugin B's handler sees RenderOutput()=true and writes formatted output
    // to its Stdout() — which tunnels back through the Connect stream to A,
    // which tunnels it back to the original caller.
    return nil
}
```

**Wire protocol:**

The `RenderOutput` flag is transmitted via a Connect request header:
```
X-Dotfiles-Render-Output: true
```

The SDK's context middleware reads this header and injects the value into the
Go context, where `ExtractContext` picks it up. `WithRenderOutput(true)` sets
the header on outgoing Connect requests.

---

## 4. Plugin Identity (Handshake)

No `GetInfo` RPC needed. The handshake JSON carries all identity info:

```json
{
    "protocol":    "dotfilesd-plugin-v1",
    "url":         "http://127.0.0.1:42859",
    "session_id":  "plugin-weather",
    "name":        "weather",
    "version":     "1.0.0",
    "description": "Fetches weather data from wttr.in"
}
```

---

## 5. Service Discovery (grpcreflect)

### How It Works

grpcreflect implements the standard [gRPC Server Reflection Protocol](https://github.com/grpc/grpc/blob/master/src/proto/grpc/reflection/v1/reflection.proto). This is the SAME protocol that `grpcurl` uses to list services.

**Plugin side** (SDK auto-mounts):
```go
import "connectrpc.com/grpcreflect"

func Serve(cfg Config) {
    mux := http.NewServeMux()
    // ... mount services ...

    // Auto-mount grpcreflect handlers so the daemon can discover everything.
    reflector := grpcreflect.NewStaticReflector(serviceNames...)
    mux.Handle(grpcreflect.NewHandlerV1(reflector))
    mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))
}
```

**Daemon side** (Manager):
```go
import (
    "connectrpc.com/grpcreflect"
    "github.com/fullstorydev/grpcurl"
)

func (m *Manager) discoverServices(ctx context.Context, pluginURL string) (grpcurl.DescriptorSource, error) {
    httpClient := &http.Client{}
    refClient := grpcreflect.NewClient(httpClient, pluginURL)
    source := grpcurl.DescriptorSourceFromServer(ctx, refClient)
    services, err := source.ListServices()
    if err != nil {
        return nil, err
    }
    // Returns: ["dotfilesd.v1.DocumentationService", "weather.WeatherService"]
    return source, nil
}
```

### What the Daemon Does with Services

1. `source.ListServices()` → get all fully-qualified service names
2. `grpcurl.ListMethods(source, name)` → get RPC method names per service
3. `source.FindSymbol(name)` → get typed proto descriptors (`*desc.ServiceDescriptor`, `*desc.MethodDescriptor`, `*desc.MessageDescriptor`) for CLI arg generation
4. `grpcurl.GetAllFiles(source)` → get complete protobuf file set for full schema info
5. Store all discovered info in `PluginInfo`
6. Serve via `PluginRegistryService`

## 5a. CLI Command & MCP Tool Mapping

Services and methods discovered via grpcreflect are automatically mapped to CLI commands and MCP tools.

### CLI Command Structure

```
dotfilesctl <plugin> [service] <rpc> --<arg>=<value> [--<arg>=<value> ...]
```

ALL arguments are proper cobra flags (`--key=value`). The daemon uses the protobuf type information from `source.FindSymbol()` to generate correctly-typed cobra flags with built-in validation.

**If the plugin exposes exactly one service** (other than DocumentationService), the service is elided:
```
dotfilesctl weather forecast --location=Madrid --format=brief
#      ^------ ^------- ^----------------------------------^
#      plugin  rpc      cobra flags typed from .proto
```

**If the plugin exposes multiple services:**
```
dotfilesctl weather alerts list              # no-arg subcommand
dotfilesctl weather alerts subscribe --region=Madrid --max-priority=3
#      ^------ ^----- ^-------- ^---------------------------------^
#      plugin  svc    rpc       cobra flags
```

**If a service has a single RPC method**, it's promoted up one level:
```
dotfilesctl alerts --region=Madrid
#      ^----- ^------------------^
#      plugin + rpc (merged)     cobra flags
```

### CLI Flag Type Mapping from Protobuf

The daemon inspects the `MethodDescriptor.InputType()` message fields and generates cobra flags with these rules:

| Protobuf Field Type | Cobra Flag Type | Validation |
|---|---|---|
| `string` | `string` | Accepts anything |
| `int32`, `int64`, `uint32`, `uint64`, `sint32`, `sint64`, `fixed32`, `fixed64`, `sfixed32`, `sfixed64` | `int` / `int64` | Rejects non-numeric values, range-checked |
| `float`, `double` | `float64` | Rejects non-numeric values |
| `bool` | `bool` | Accepts `true`/`false`/`1`/`0` |
| `enum` | `string` with `AllowedValues` | Cobra's `ValidArgs` restricts to enum choice strings |
| `repeated T` | Multiple `--flag` occurrences | Each occurrence validated per type T, appended to array |
| `message` (object) | **Nested flags** — exploded recursively using dot notation | Each nested field becomes `--parent.field` |
| `map<K, V>` | `--map.key=value` syntax | Key is always a string (proto requirement). Value is parsed and validated according to type V. Each occurrence sets one entry. |

### Cobra Flag Generation Rules

### Cobra Flag Generation Rules

Given a protobuf request message:

```protobuf
message ForecastRequest {
  string location = 1;
  int32 days = 2;
  bool detailed = 3;
  enum Unit { CELSIUS = 0; FAHRENHEIT = 1; }
  Unit unit = 4;
  repeated string tags = 5;
}
```

The daemon generates cobra flags:
```go
// Pseudocode — what the CLI builder generates:
cmd.Flags().String("location", "", "City name (required)")
cmd.Flags().Int("days", 7, "Number of days")
cmd.Flags().Bool("detailed", false, "Show detailed forecast")
cmd.Flags().String("unit", "CELSIUS", "Temperature unit").ValidArgs = []string{"CELSIUS", "FAHRENHEIT"}
cmd.Flags().StringSlice("tags", nil, "Tags to filter")
```

Usage:
```sh
# Basic typed flags:
dotfilesctl weather forecast --location=Madrid --days=5 --detailed=true

# Enum validation — wrong value rejected:
dotfilesctl weather forecast --location=Madrid --unit=KELVIN
# → Error: invalid argument "KELVIN" for "--unit": valid values are CELSIUS, FAHRENHEIT

# Repeated fields:
dotfilesctl weather forecast --location=Madrid --tags=sunny --tags=warm

# Nested object fields:
# message SearchRequest { message Filters { string region = 1; int32 min_temp = 2; } Filters filters = 1; }
dotfilesctl weather search --filters.region=Europe --filters.min-temp=15
```

### Nested Object Explosion Rule

For message fields (object types), flags are generated recursively with dot notation:

```protobuf
message ForecastRequest {
  message Options {
    message Range {
      int32 min = 1;
      int32 max = 2;
    }
    Range temp_range = 1;
    bool include_humidity = 2;
  }
  Options opts = 1;
  string location = 2;
}
```

Generated flags:
```
--opts.temp-range.min=<int>       # dot-path to nested field
--opts.temp-range.max=<int>
--opts.include-humidity=<bool>
--location=<string>
```

Cobra command usage:
```sh
dotfilesctl weather forecast \
  --location=Madrid \
  --opts.temp-range.min=10 \
  --opts.temp-range.max=35 \
  --opts.include-humidity=true
```

### Map Type Mapping

Protobuf map fields are exposed as `--map.key=value` flags. The key is always a
string (protobuf requirement). The value is parsed and validated according to the
map's value type V, following the same rules as other typed flags.

**Protobuf:**
```protobuf
message ConfigureRequest {
  map<string, string> labels = 1;         // value is string — no extra validation
  map<string, int32> limits = 2;          // value is int32 — rejects non-numeric
  map<string, double> thresholds = 3;     // value is double — rejects non-numeric
  map<string, bool> flags = 4;            // value is bool — accepts true/false/1/0
}
```

**Generated cobra flags (one per map field):**
```go
// Each map field becomes a single StringSlice flag.
// Each occurrence sets one key=value entry in the map.
// The value portion is type-checked at parse time.
cmd.Flags().StringSlice("labels", nil, "Key=value pairs (string → string)")
cmd.Flags().StringSlice("limits", nil, "Key=value pairs (string → int32, value must be numeric)")
cmd.Flags().StringSlice("thresholds", nil, "Key=value pairs (string → float64)")
cmd.Flags().StringSlice("flags", nil, "Key=value pairs (string → bool, accepts true/false/1/0)")
```

**Usage:**
```sh
# string → string — any value accepted:
dotfilesctl weather configure --labels.env=prod --labels.team=platform

# string → int32 — non-numeric value rejected:
dotfilesctl weather configure --limits.cpu=4 --limits.mem=8192
dotfilesctl weather configure --limits.cpu=four
# → Error: invalid argument "four" for "--limits.cpu": value must be a valid integer

# string → float64:
dotfilesctl weather configure --thresholds.cpu-warn=0.8 --thresholds.mem-warn=0.9

# string → bool:
dotfilesctl weather configure --flags.verbose=true --flags.dry-run=1

# Multiple entries populate the map:
# Result: labels = {"env": "prod", "team": "platform"}
#         limits = {"cpu": 4, "mem": 8192}
```

**How it works internally:**

Each map field produces a single `StringSlice` flag. The flag name matches the
map field name (e.g. `--limits`). Each occurrence of the flag is validated as
`--<map-field-name>.<key>=<value>`:

1. Split the flag on the first `=` to extract `<key>=<value>`
2. Validate `<value>` according to the map's value type V:
   - `string` → accepted as-is
   - numeric → parse with `strconv.Atoi` / `strconv.ParseFloat`, error if fails
   - `bool` → parse with `strconv.ParseBool`
   - `enum` → validate against allowed enum choice names
3. Store in the resulting Go `map[string]V`

For nested objects inside a map value, use the dot-path prefix as the key:
```protobuf
message ConfigRequest {
  map<string, Options> per_environment = 1;  // map<string, message>
}
```
```sh
# The key is the environment name; the value is the Options message exploded
# via dot notation after the key:
dotfilesctl weather configure \
  --per_environment.prod.temp-range.min=10 \
  --per_environment.prod.temp-range.max=35 \
  --per_environment.staging.temp-range.min=5 \
  --per_environment.staging.temp-range.max=30
# Result: per_environment = {
#   "prod":    { temp_range: { min: 10, max: 35 } },
#   "staging": { temp_range: { min: 5,  max: 30 } },
# }
```

### Cobra Command Tree Construction

```
dotfilesctl
  └── <plugin>          # cobra command, description from proto
      └── [service]     # cobra command, elided if only one service
          └── <rpc>     # cobra command
              └── --flag1  # typed from proto message fields
              └── --flag2  # (exploded recursively for nested objects)
```

Each leaf command (`<rpc>`) executes by:
1. Collecting all flag values
2. Building the protobuf request message programmatically
3. Calling `grpcurl.InvokeRPC()` or a dynamic Connect client invoker
4. Printing the JSON response

### Mapping Algorithm (Pseudocode)

```go
func BuildCLI(source grpcurl.DescriptorSource, pluginName string) *cobra.Command {
    services, _ := source.ListServices()
    pluginCmd := &cobra.Command{Use: pluginName}

    singleSvc := len(services) == 1  // elide service level
    for _, svcName := range services {
        if isDocumentationService(svcName) { continue }
        svcDesc, _ := source.FindSymbol(svcName)
        srvCmd := pluginCmd
        if !singleSvc {
            srvCmd = &cobra.Command{Use: shortName(svcName)}
            pluginCmd.AddCommand(srvCmd)
        }
        methods, _ := grpcurl.ListMethods(source, svcName)
        for _, method := range methods {
            mdDesc := svcDesc.FindMethodByName(method)
            msgDesc := mdDesc.GetInputType()           // request message
            rpcCmd := &cobra.Command{Use: methodName(method), RunE: func(...){
                // 1. Collect flag values into proto message
                msg := dynamicpb.NewMessage(msgDesc)
                // 2. Fill from flags (recursive for nested objects)
                fillMessageFromFlags(msg, msgDesc, flags)
                // 3. Build Connect client dynamically or use grpcurl
                // 4. Invoke RPC, print result
            }}
            addFlagsFromMessage(rpcCmd, msgDesc, "")   // "" = root prefix
            srvCmd.AddCommand(rpcCmd)
        }
    }
    return pluginCmd
}

func addFlagsFromMessage(cmd *cobra.Command, msgDesc protoreflect.MessageDescriptor, prefix string) {
    fields := msgDesc.Fields()
    for i := 0; i < fields.Len(); i++ {
        fd := fields.Get(i)
        fullName := prefix + fd.Name()               // e.g. "opts.temp-range.min"
        switch fd.Kind() {
        case protoreflect.StringKind:
            cmd.Flags().String(fullName, "", fd.Description())
        case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Sint32Kind, ...:
            cmd.Flags().Int(fullName, 0, fd.Description())
        case protoreflect.BoolKind:
            cmd.Flags().Bool(fullName, false, fd.Description())
        case protoreflect.EnumKind:
            vals := fd.Enum().Values()
            choices := make([]string, vals.Len())
            for j := 0; j < vals.Len(); j++ {
                choices[j] = string(vals.Get(j).Name())
            }
            cmd.Flags().String(fullName, choices[0], fd.Description())
            cmd.RegisterFlagCompletionFunc(fullName, func(...) []string{ return choices })
        case protoreflect.MessageKind:
            // Recurse into nested message
            addFlagsFromMessage(cmd, fd.Message(), fullName + ".")
        case protoreflect.MapKind:
            // Map fields: --<map-field>.<key>=<value>
            // Key is always string (proto constraint).
            // Value validation depends on map value kind.
            mapName := fullName
            valKind := fd.MapValue().Kind()
            if valKind == protoreflect.MessageKind {
                // map<string, message> — the key prefixes the exploded sub-fields:
                // --<map-field>.<key>.<subfield1>=<val1> --<map-field>.<key>.<subfield2>=<val2>
                // We add a StringSlice flag for the top-level map and document
                // that entries are built by scanning flags with the "key." prefix.
                // Nested messages inside the value are not supported in this simplified model.
                cmd.Flags().StringSlice(mapName, nil,
                    "Map entries. Use --"+mapName+".<key>=<value> syntax. "+
                    "For message values, explode sub-fields after the key: "+
                    "e.g. --"+mapName+".mykey.subfield1=val1 --"+mapName+".mykey.subfield2=val2")
            } else {
                // map<string, scalar> — single-value per key, type-checked:
                // --<map-field>.<key>=<value>
                desc := "Map entries (string → " + valKind.String() +
                    "). Use --" + mapName + ".<key>=<value> syntax. " +
                    "Value is type-checked."
                cmd.Flags().StringSlice(mapName, nil, desc)
            }
        }
    }
}
```

### MCP Tool Generation Example

```jsonc
// Plugin "weather" with single service (method "Forecast")
{
  "name": "weather_forecast",                 // <plugin>_<method>
  "description": "...",
  "inputSchema": {
    "type": "object",
    "properties": {
      "location": { "type": "string", "description": "City name" },
      "days":     { "type": "integer", "description": "Number of days" },
      "detailed": { "type": "boolean", "description": "Show details" },
      "unit":     { "type": "string", "enum": ["CELSIUS", "FAHRENHEIT"] },
      "tags":     { "type": "array", "items": { "type": "string" } },
      "opts.temp-range.min": { "type": "integer" },
      "opts.temp-range.max": { "type": "integer" }
    },
    "required": ["location"]
  }
}

// Plugin "dashboard" with multiple services
{
  "name": "stats_current",                   // <plugin>_<svc>_<method>
  "inputSchema": { "type": "object", "properties": { "pid": { "type": "string" } } }
},
{
  "name": "stats_history",
  "inputSchema": { "type": "object", "properties": { "count": { "type": "integer" } } }
},
{
  "name": "health_check",
  "inputSchema": { "type": "object", "properties": {} }
}
  "inputSchema": { ... }
},
{
  "name": "stats_history",                   // same service, different method
  "inputSchema": { ... }
},
{
  "name": "health_check",                    // different service
  "inputSchema": { ... }
}
```

---

## 6. DocumentationService Plugin Protocol

Both the daemon and the SDK have this proto at compile time.

### Proto

```protobuf
syntax = "proto3";
package dotfilesd.v1;
option go_package = "dotfilesd/proto/dotfilesd/v1/dotfilesdv1";

service DocumentationService {
  rpc GetDocumentation(DocumentationRequest) returns (DocumentationResponse);
}

message DocumentationRequest {
  string service_name = 1;  // empty = plugin-level docs
}

message DocumentationResponse {
  string format = 1;   // "markdown", "json-schema", "openapi-3.0"
  string content = 2;
}
```

### SDK Default Implementation

The SDK auto-implements this using `Config` fields:
- **Plugin docs** (no service_name): name, display name, version, description, list of all services
- **Per-service docs** (specific service name): description, method names

Plugins can override by providing their own `DocumentationService` in `Config.Services`.

---

## 7. Daemon-Side Plugin Registry

The daemon compiles this proto. Plugins do NOT need it — they discover each other at runtime.

```protobuf
syntax = "proto3";
package dotfilesd.v1;
option go_package = "dotfilesd/proto/dotfilesd/v1/dotfilesdv1";
import "proto/dotfilesd/v1/dotfilesdv1/session.proto";

service PluginRegistryService {
  rpc GetPlugin(RegistryGetPluginRequest) returns (RegistryGetPluginResponse);
  rpc ListPlugins(RegistryListPluginsRequest) returns (RegistryListPluginsResponse);
}

message RegistryGetPluginRequest { Session session = 100; string plugin_name = 1; }
message RegistryGetPluginResponse {
  string name = 1;
  string display_name = 2;
  string version = 3;
  string description = 4;
  string url = 5;
  repeated string services = 6;  // from grpcreflect discovery
}

message RegistryListPluginsRequest { Session session = 100; }
message RegistryListPluginsResponse { repeated RegistryGetPluginResponse plugins = 1; }
```

---

## 8. Daemon Plugin Manager

### PluginInfo

```go
type PluginInfo struct {
    Name, DisplayName, Version, Description string
    URL       string
    Services  []string   // service names from grpcreflect discovery
    Process   *os.Process
    SourceDir string
    CacheDir  string
}
```

### LoadPlugins() Algorithm

```
1. Scan ~/.config/dotfilesd/plugins/ for Go plugin dirs (go.mod or main.go)
2. Parse each plugin's go.mod for `require` directives referencing other plugins
   → Build dependency graph: map[pluginName][]depNames
3. Topological sort (dependencies first)
4. For each plugin in build order:
   a. GENERATE PROTO: if the plugin directory contains a `proto/<name>/` subdirectory
      with `.proto` files, compile them:
      ```sh
      protoc --proto_path=<plugin_dir> --go_out=<plugin_dir> \
        --connect-go_out=<plugin_dir> \
        <plugin_dir>/proto/<name>/*.proto
      ```
      This generates `<name>.pb.go` and `weatherconnect/<name>.connect.go` inside
      the plugin's `proto/<name>/` directory. These files are consumed by `go build`.
   b. `go build -o <cache>/<name>/<name> .`
   c. Launch subprocess with:
      EXECUTION_CONTEXT_URL=http://127.0.0.1:9105
      EXECUTION_CONTEXT_TOKEN=<token>
      SESSION_ID=plugin-<name>
   d. Read handshake JSON from stdout (name, version, description, URL)
   e. grpcreflect.NewClient(httpClient, URL).NewStream(ctx).ListServices() → ALL service names
   f. If DocumentationService is among services, call GetDocumentation() → cache docs
   g. Store PluginInfo in plugins map
5. Done
```

---

## 9. Plugin Discovery & Registration Flow

```
Daemon              PluginManager                 Plugin Process
  │                     │                              │
  │ LoadPlugins()       │                              │
  │────────────────────►│                              │
  │                     │ Scan plugins/                 │
  │                     │ Parse go.mod deps             │
  │                     │ Topological sort              │
  │                     │                              │
  │                     │ For each plugin:              │
  │                     │ go build .                    │
  │                     │ spawn subprocess ───────────►│
  │                     │                              │
  │                     │       handshake (stdout)      │
  │                     │◄─────────────────────────────│
  │                     │  {name,version,url,...}       │
  │                     │                              │
  │                     │ grpcreflect.NewClient(url)   │
  │                     │ .ListServices() ────────────►│
  │                     │◄─────────────────────────────│
  │                     │  ["DocService",              │
  │                     │   "WeatherService", ...]      │
  │                     │                              │
  │                     │ DocumentationService          │
  │                     │ .GetDocumentation() ────────►│
  │                     │◄─────────────────────────────│
  │                     │  docs cached                  │
  │                     │                              │
  │                     │ Store PluginInfo              │
  │◄────────────────────│                              │
```

---

## 10. Plugin-to-Plugin Type-Safe Calls

### Runtime (not compile-time) Discovery

Plugin B (dashboard) wants to call Plugin A (weather). **Plugin B does NOT need the daemon's PluginRegistryService at compile time.** It discovers at runtime.

```go
import (
    "connectrpc.com/grpcreflect"
    "connectrpc.com/connect"
)

func dashboardHandler(ctx plugin.Context, args map[string]string) error {
    daemonURL := os.Getenv("EXECUTION_CONTEXT_URL")

    // 1. Discover weather plugin via daemon's registry.
    regClient := dotfilesdv1connect.NewPluginRegistryServiceClient(
        &http.Client{}, daemonURL)
    info, _ := regClient.GetPlugin(context.Background(),
        &connect.Request{
            Msg: &dotfilesdv1.RegistryGetPluginRequest{PluginName: "weather"},
        })
    weatherURL := info.Msg.Url   // → "http://127.0.0.1:12345"

    // 2. Build type-safe client using generated code from weather plugin.
    weatherClient := weatherconnect.NewWeatherServiceClient(
        &http.Client{}, weatherURL)

    // 3. Call with full type safety.
    forecast, _ := weatherClient.Forecast(context.Background(),
        &connect.Request{
            Msg: &pb.ForecastRequest{Location: "Madrid"},
        })

    fmt.Fprintln(ctx.Stdout(), forecast.Msg.Report)
    return nil
}
```

---

## 11. Plugin Definition (Proto + Code)

### Full Example: Weather Plugin

**`plugins/weather/proto/weather/weather.proto`**
```protobuf
syntax = "proto3";
package weather;
option go_package = "plugins/weather/proto/weather";

service WeatherService {
  rpc Forecast(ForecastRequest) returns (ForecastResponse);
}
message ForecastRequest { string location = 1; string format = 2; }
message ForecastResponse { string report = 1; int32 exit_code = 2; string error_message = 3; }
```

**`plugins/weather/main.go`**
```go
package main

import (
    "context"
    "dotfilesd/plugin"
    pb "plugins/weather/proto/weather"
    "plugins/weather/proto/weather/weatherconnect"
    "connectrpc.com/connect"
)

type weatherServer struct{}

func (s *weatherServer) Forecast(ctx context.Context, req *connect.Request[pb.ForecastRequest]) (*connect.Response[pb.ForecastResponse], error) {
    pc := plugin.ExtractContext(ctx)
    if pc != nil {
        pc.Log().Info("forecasting", "loc", req.Msg.Location)
        result, _ := pc.Exec("curl -s wttr.in/" + req.Msg.Location + "?0")
        return connect.NewResponse(&pb.ForecastResponse{Report: result.Stdout}), nil
    }
    return connect.NewResponse(&pb.ForecastResponse{ErrorMessage: "no context"}), nil
}

func main() {
    svc := &weatherServer{}
    path, handler := weatherconnect.NewWeatherServiceHandler(svc)
    plugin.Serve(plugin.Config{
        Name: "weather", DisplayName: "Weather", Version: "1.0.0",
        Description: "Fetches weather data from wttr.in",
        Services: []plugin.Service{
            {Name: "weather.WeatherService", Description: "Weather forecast API",
             Path: path, Handler: handler},
        },
    })
}
```

**`plugins/weather/go.mod`**
```
module plugins/weather
go 1.26.3
replace dotfilesd => /home/manu343726/dotfilesd
require dotfilesd v0.0.0-00010101000000-000000000000
```

### Dependencies Between Plugins

**`plugins/dashboard/go.mod`**
```
module plugins/dashboard
go 1.26.3
replace (
    dotfilesd => /home/manu343726/dotfilesd
    plugins/weather => ../weather
)
require (
    dotfilesd v0.0.0
    plugins/weather v0.0.0
)
```

Topological sort ensures `weather` is built before `dashboard`.

---

## 12. Build Dependency Graph

The plugin build system handles ALL compilation steps in-order: proto generation first, then Go binary compilation. Plugins that depend on other plugins will have those plugins' protos compiled and binaries built before them.

### Build Algorithm

For each plugin in topological order:
1. **Proto compilation**: If the plugin has custom proto files (e.g. `proto/weather/weather.proto`), compile them using `protoc`:
   ```sh
   protoc --proto_path=<plugin_dir> --go_out=<plugin_dir> \
     --connect-go_out=<plugin_dir> \
     <plugin_dir>/proto/<name>/*.proto
   ```
   This produces the `.pb.go` and `.connect.go` files in the plugin's `proto/` tree that `go build` will consume.
2. **Go compilation**: Run `go build -o <cache>/<name>/<name> .` in the plugin directory.

### Dependency Graph Extraction

```go
func parsePluginDeps(sourceDir, pluginsDir string) ([]string, error) {
    data, _ := os.ReadFile(filepath.Join(sourceDir, "go.mod"))
    var deps []string
    for _, line := range strings.Split(string(data), "\n") {
        line = strings.TrimSpace(line)
        for _, part := range strings.Fields(line) {
            pluginDir := filepath.Join(pluginsDir, filepath.Base(part))
            if _, err := os.Stat(pluginDir); err == nil {
                deps = append(deps, filepath.Base(part))
            }
        }
    }
    return deps, nil
}
```

Since dependencies are in topological order, when plugin B depends on plugin A,
plugin A's proto files are compiled and its binary is built BEFORE plugin B.
Plugin B can then import plugin A's generated Go code.

For example:

```
plugins/weather/          (no deps — protos compiled, binary built first)
plugins/dashboard/        (depends on weather — can import weatherconnect)
```

The Makefile `plugin-build-all` target wraps this whole process:
```makefile
# Proto compilation for a single plugin.
# Usage: make plugin-proto PLUGIN=weather
plugin-proto:
    @if [ -z "$(PLUGIN)" ]; then \
        echo "Usage: make plugin-proto PLUGIN=<name>"; \
        exit 1; \
    fi
    @if [ -d "$(PLUGIN_DIR)/$(PLUGIN)/proto/$(PLUGIN)" ]; then \
        cd $(PLUGIN_DIR)/$(PLUGIN) && \
        protoc --proto_path=. --go_out=. --go_opt=paths=source_relative \
            --connect-go_out=. --connect-go_opt=paths=source_relative \
            proto/$(PLUGIN)/*.proto; \
        echo "plugin '$(PLUGIN)' protos compiled."; \
    fi

# Build a single plugin (proto + Go).
plugin-build: plugin-proto
    @if [ -z "$(PLUGIN)" ]; then \
        echo "Usage: make plugin-build PLUGIN=<name>"; \
        exit 1; \
    fi
    @mkdir -p $(PLUGIN_CACHE_DIR)/$(PLUGIN)
    cd $(PLUGIN_DIR)/$(PLUGIN) && $(GO) build -o $(PLUGIN_CACHE_DIR)/$(PLUGIN)/$(PLUGIN) .
    @echo "plugin '$(PLUGIN)' built."

# Build all plugins in dependency order (proto + Go).
plugin-build-all:
    @echo "building plugins in dependency order..."
    # 1. Resolve dependency order from go.mod files
    # 2. For each plugin in order:
    #    a. "cd plugins/<name> && protoc ..."   (if proto/ exists)
    #    b. "cd plugins/<name> && go build ..."
```

This ensures that when plugin B depends on plugin A, plugin A's proto files are
compiled and its Go binary is built first — making the generated client code
available for plugin B to import at compile time.

---

## 13. Complete Proto Files

### `plugin_registry.proto` (daemon-known, compile-time)

```protobuf
syntax = "proto3";
package dotfilesd.v1;
import "proto/dotfilesd/v1/dotfilesdv1/session.proto";

service PluginRegistryService {
  rpc GetPlugin(RegistryGetPluginRequest) returns (RegistryGetPluginResponse);
  rpc ListPlugins(RegistryListPluginsRequest) returns (RegistryListPluginsResponse);
}
message RegistryGetPluginRequest { Session session = 100; string plugin_name = 1; }
message RegistryGetPluginResponse {
  string name = 1; string display_name = 2; string version = 3;
  string description = 4; string url = 5; repeated string services = 6;
}
message RegistryListPluginsRequest { Session session = 100; }
message RegistryListPluginsResponse { repeated RegistryGetPluginResponse plugins = 1; }
```

### `documentation.proto` (daemon+SDK, compile-time)

```protobuf
syntax = "proto3";
package dotfilesd.v1;

service DocumentationService {
  rpc GetDocumentation(DocumentationRequest) returns (DocumentationResponse);
}
message DocumentationRequest { string service_name = 1; }
message DocumentationResponse { string format = 1; string content = 2; }
```

### Plugin proto (per-plugin, NOT known to daemon at compile time)

```protobuf
syntax = "proto3";
package weather;
option go_package = "plugins/weather/proto/weather";

service WeatherService {
  rpc Forecast(ForecastRequest) returns (ForecastResponse);
}
```

---

## 14. SDK Implementation Details

### `plugin/serve.go`

```go
import (
    "connectrpc.com/grpcreflect"
    dotfilesdv1connect "dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
)

func Serve(cfg Config) {
    ctxClient := newContextClient(ctxURL, ctxToken, sessionID, cfg.Name)
    mux := http.NewServeMux()

    // Collect all service names for the reflector.
    names := []string{"dotfilesd.v1.DocumentationService"}
    for _, svc := range cfg.Services {
        names = append(names, svc.Name)
    }

    // Mount grpcreflect handlers — daemon discovers ALL services via this.
    reflector := grpcreflect.NewStaticReflector(names...)
    mux.Handle(grpcreflect.NewHandlerV1(reflector))
    mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))

    // Mount DocumentationService (SDK default).
    docsSvc := &documentationServiceServer{...}
    path, handler := dotfilesdv1connect.NewDocumentationServiceHandler(docsSvc)
    mux.Handle(path, handler)

    // Mount all custom services.
    for _, svc := range cfg.Services {
        mux.Handle(svc.Path, svc.Handler)
    }

    // Context injection middleware.
    wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        r = r.WithContext(WithContext(r.Context(), ctxClient))
        mux.ServeHTTP(w, r)
    })

    // Listen on random port, handshake, serve, block on SIGTERM.
}
```

### `plugin/context.go`

`contextClient` calls daemon's `ExecService`, `FeedbackService`, `IOService`, `ScriptService`
with `X-Dotfiles-Context-Token` header. These are the ONLY daemon-facing APIs.

No `CallPlugin`, `CallPluginStream`, `pluginClient` — those are gone.
No `streamingContext` — no ExtensionService to support.

---

## 15. Files to Create/Modify

### Delete (old protocols)
| File | Reason |
|------|--------|
| `proto/.../extension.proto` | Old tool dispatch protocol |
| `proto/.../plugin.proto` | Old CallPluginTool/ListPlugins |
| `proto/.../plugin_base.proto` | GetInfo/ListServices now redundant |
| `internal/pkg/daemon/plugin_svc.go` | Old PluginService handler |
| `internal/pkg/plugin/builder.go` | Old builder (merged into manager) |
| `internal/pkg/plugin/extension_client.go` | Old ExtensionService client |
| `internal/pkg/plugin/registry.go` | Old registry |
| `internal/pkg/plugin/runtime.go` | Old runtime |
| `internal/pkg/plugin/supervisor.go` | Old supervision |

### Create
| File | Purpose |
|------|---------|
| `proto/.../documentation.proto` | DocumentationService (daemon+SDK known) |
| `plugin/docs.go` | Default DocumentationService server impl |

### Update
| File | Action |
|------|--------|
| `plugin/serve.go` | Remove Tool/Tools, mount grpcreflect + DocumentationService |
| `plugin/context.go` | Remove streamingContext, pluginClient |
| `internal/pkg/plugin/manager.go` | grpcreflect-based service discovery, handshake identity |
| `internal/pkg/daemon/registry_svc.go` | Backed by PluginInfo from manager |
| `internal/pkg/daemon/plugin.go` | InitPlugins creates manager, loads plugins |
| `internal/pkg/daemon/server.go` | Mount RegistryService |
| `internal/pkg/cli/plugin.go` | Use PluginRegistryService |
| `internal/pkg/cli/client.go` | Add Registry client |
| `internal/pkg/cli/mcp.go` | Discover via Registry + DocumentationService |
| `Makefile` | Update proto targets: add `documentation.proto`, add `plugin-proto` target for per-plugin proto compilation |
| `plugins/weather/main.go` | Custom RPC service (proto + generated code + handler) |
| `plugins/resources/main.go` | Custom RPC service (proto + generated code + handler) |

---

## 16. Implementation Order

> This is a **coordinated rewrite**. Steps 1-4 tear out the old architecture;
> steps 5-13 build the new daemon & SDK; steps 14-15 build the new plugins.
> Nothing is additive — every change is a replacement or deletion.

0. **Scaffold new plugin directories**: Create `plugins/weather/proto/`, `plugins/resources/proto/`, `plugins/tmuxbar/` with fresh `go.mod` files
1. **Delete ALL old plugin code**: `extension.proto`, `plugin.proto`, `plugin_base.proto`, old `plugins/weather/main.go` (Tool-based), old `plugins/resources/main.go` (Tool-based), old `plugin/serve.go` Tool stubs
2. **Create `documentation.proto`** with DocumentationService
3. **Keep `plugin_registry.proto`** (already exists, no changes needed)
4. **Run `make proto`** to regenerate
5. **Rewrite `plugin/serve.go`**: Remove Tool/Tools, mount grpcreflect handlers + DocumentationService
6. **Rewrite `plugin/context.go`**: Remove streamingContext, pluginClient, CallPlugin
7. **Create `plugin/docs.go`**: Default DocumentationService implementation
8. **Build SDK**: `go build ./plugin/...`
9. **Rewrite `internal/pkg/plugin/manager.go`**: grpcreflect-based discovery, handshake identity, proto compilation step
10. **Rewrite `internal/pkg/daemon/plugin.go`** and **`server.go`**
11. **Rewrite `internal/pkg/daemon/registry_svc.go`**
12. **Update `internal/pkg/cli/`**: Registry instead of PluginService
13. **Build daemon**: `go build ./...`
14. **Rewrite plugins from scratch**: weather → proto + code, resources → proto + code
15. **Update `Makefile`**: proto targets, plugin-build-all with deps
16. **Build all**: `make build && make plugin-build-all`
17. **Install, test, commit**

---

## 17. Current Plugins: Feature Inventory & Rewrite Plan

> **⚠️ This is NOT a migration.** Every plugin listed here is deleted and
> **rewritten from scratch** as a Connect RPC service. The old Tool-based
> implementation is discarded entirely — we keep only the *concept* of what
> the plugin does. The new plugin shares the same purpose but has zero code
> in common with the old version.

This section inventories ALL current plugin features and defines the rewrite
plan for the new RPC-based architecture. Each plugin is built anew to expose
its own Connect RPC services with type-safe generated clients.

### 17.1 Weather Plugin (Forecast)

**Current state (DELETED):** Tool-based plugin using `plugin.NewTool()`.

**Rewrite:**

**RPC Service (new architecture):**
```protobuf
service WeatherService {
  rpc Forecast(ForecastRequest) returns (ForecastResponse);
}

message ForecastRequest {
  string location = 1;
  string format = 2;  // "brief", "full", "json"
}

message ForecastResponse {
  string report = 1;
  int32 exit_code = 2;
  string error_message = 3;
}
```

**Behavior:**
- Fetches weather from wttr.in via `ctx.Exec("curl ...")`
- When `RenderOutput=true` (CLI): prints formatted weather report to `Stdout()`
- When `RenderOutput=false` (plugin-to-plugin): returns raw report string

### 17.2 Resources Plugin (System Monitoring)

**Current state (DELETED):** Tool-based plugin with background collector goroutine and 4 tools (current, top, ps, history).

**Rewrite:**
```protobuf
service ResourcesService {
  rpc Current(CurrentRequest) returns (CurrentResponse);
  rpc Top(TopRequest) returns (TopResponse);
  rpc PS(PSRequest) returns (PSResponse);
  rpc History(HistoryRequest) returns (HistoryResponse);
}
```

**Data model** (shared types used by all RPCs):
```protobuf
message RAMSnapshot {
  double total_mb = 1;
  double used_mb = 2;
  double available_mb = 3;
  double percent = 4;
}

message CPUSnapshot {
  double total_percent = 1;
  double user_percent = 2;
  double system_percent = 3;
  double iowait_percent = 4;
  int32 num_cores = 5;
}

message DiskSnapshot {
  string mount_point = 1;
  double total_gb = 2;
  double used_gb = 3;
  double avail_gb = 4;
  double percent = 5;
}

message DiskIOSnapshot {
  string device = 1;
  double reads_per_sec = 2;
  double writes_per_sec = 3;
  double read_bytes_per_sec = 4;
  double write_bytes_per_sec = 5;
}
```

**Background collector** remains via `Config.Background`:
- Collects RAM, CPU, disk, disk I/O every 3 seconds
- Stores snapshots in a ring buffer (100 entries)
- All RPCs read from the shared state (instant responses)

### 17.3 TmuxBar Plugin (NEW — Proof of Concept)

A new plugin (no old version to delete) that implements tmux status bar widgets
using the `resources` plugin for data. This demonstrates **plugin-to-plugin
dependency**: `tmuxbar` imports `resources`'s generated client and calls its
RPCs for data, handling only presentation/formatting.

**Dependency setup:**
```go
// plugins/tmuxbar/go.mod
module plugins/tmuxbar
go 1.26.3
replace (
    dotfilesd => /home/manu343726/dotfilesd
    plugins/resources => ../resources
)
require (
    dotfilesd v0.0.0
    plugins/resources v0.0.0
)
```

**RPC Service:**
```protobuf
service TmuxBarService {
  // RAM usage widget: "RAM 6.2/15.9 GB 39%"
  rpc RAMWidget(RAMWidgetRequest) returns (RAMWidgetResponse);

  // CPU usage widget: "CPU 24% ▁▃▅▇▆▄▃"
  rpc CPUWidget(CPUWidgetRequest) returns (CPUWidgetResponse);

  // CPU temperature widget: "🌡 65°C"
  rpc CPUTempWidget(CPUTempWidgetRequest) returns (CPUTempWidgetResponse);

  // Battery widget: "🔋 85%" or "⚡charging"
  rpc BatteryWidget(BatteryWidgetRequest) returns (BatteryWidgetResponse);

  // Combined compact status: "CPU 24% | RAM 39% | 65°C"
  rpc StatusBar(StatusBarRequest) returns (StatusBarResponse);
}

message RAMWidgetRequest {
  // Optional: override default format string
  string format = 1;
}
message RAMWidgetResponse {
  string text = 1;        // e.g. "RAM 6.2/15.9 GB 39%"
  double percent = 2;     // raw percentage for bar rendering
}

message CPUWidgetRequest {}
message CPUWidgetResponse {
  string text = 1;        // e.g. "CPU 24%"
  double percent = 2;     // raw percentage
}

message CPUTempWidgetRequest {
  string unit = 1;        // "celsius" or "fahrenheit"
}
message CPUTempWidgetResponse {
  string text = 1;        // e.g. "🌡 65°C"
  double temperature = 2; // raw value
}

message BatteryWidgetRequest {}
message BatteryWidgetResponse {
  string text = 1;        // e.g. "🔋 85%" or "⚡charging"
  double percent = 2;
  bool charging = 3;
}

message StatusBarRequest {}
message StatusBarResponse {
  string text = 1;        // e.g. "CPU 24% | RAM 39% | 65°C"
}
```

**Implementation pattern — each widget calls `resources` plugin:**

```go
type tmuxBarServer struct {
    resourcesClient resourcesconnect.ResourcesServiceClient
}

func (s *tmuxBarServer) RAMWidget(ctx context.Context, req *connect.Request[pb.RAMWidgetRequest]) (*connect.Response[pb.RAMWidgetResponse], error) {
    // Get data from resources plugin via type-safe RPC.
    r, _ := s.resourcesClient.Current(ctx, &connect.Request{
        Msg: &resourcespb.CurrentRequest{},
    })
    ram := r.Msg.Ram
    text := fmt.Sprintf("RAM %.1f/%.1f GB %.0f%%", ram.UsedMb/1024, ram.TotalMb/1024, ram.Percent)
    return connect.NewResponse(&pb.RAMWidgetResponse{Text: text, Percent: ram.Percent}), nil
}

func (s *tmuxBarServer) CPUWidget(ctx context.Context, req *connect.Request[pb.CPUWidgetRequest]) (*connect.Response[pb.CPUWidgetResponse], error) {
    r, _ := s.resourcesClient.Current(ctx, &connect.Request{
        Msg: &resourcespb.CurrentRequest{},
    })
    cpu := r.Msg.Cpu
    bar := renderSparkline(cpu.TotalPercent)
    text := fmt.Sprintf("CPU %.0f%% %s", cpu.TotalPercent, bar)
    return connect.NewResponse(&pb.CPUWidgetResponse{Text: text, Percent: cpu.TotalPercent}), nil
}
```

**How discovery works at runtime:**

```go
func initResourcesClient(daemonURL string) resourcesconnect.ResourcesServiceClient {
    // 1. Discover resources plugin via daemon's registry.
    reg := dotfilesdv1connect.NewPluginRegistryServiceClient(&http.Client{}, daemonURL)
    info, _ := reg.GetPlugin(context.Background(), &connect.Request{
        Msg: &dotfilesdv1.RegistryGetPluginRequest{PluginName: "resources"},
    })

    // 2. Build type-safe client to resources plugin.
    return resourcesconnect.NewResourcesServiceClient(&http.Client{}, info.Msg.Url)
}
```

**Tmux integration:**

The tmux config uses `run-shell` to call the plugin:
```sh
# In ~/.tmux.conf.local:
set -g status-right "... #(dotfilesctl tmuxbar cpu-widget) #(dotfilesctl tmuxbar ram-widget) ..."
```

Or better, via the CLI's `--format=json` for structured output that a tmux script
can parse and format with oh-my-tmux compatible symbols.

### 17.4 Plugin Dependency Graph (Build Order)

```
plugins/weather/          (no deps)
plugins/resources/        (no deps)
plugins/tmuxbar/          (depends on resources -> resources built first)
plugins/dashboard/        (depends on resources + weather -> both built first)
```

The daemon's `LoadPlugins()` function resolves this automatically from `go.mod`.

### 17.5 Summary of Plugin RPC Services

| Plugin | Service Name | RPCs | Depends On |
|--------|-------------|------|------------|
| weather | `weather.WeatherService` | Forecast | — |
| resources | `resources.ResourcesService` | Current, Top, PS, History | — |
| tmuxbar | `tmuxbar.TmuxBarService` | RAMWidget, CPUWidget, CPUTempWidget, BatteryWidget, StatusBar | resources |

---

## 18. Development Workflow

> **Rules for the AI agent implementing this document.** These rules prevent
> irrecoverable mistakes and keep every change reviewable and revertible.

### 18.1 One Commit Per Implementation Step

Implementation step = one section of §16 (Implementation Order). For example:

| Step | Commit |
|------|--------|
| Step 0: Scaffold plugin dirs | `feat: scaffold weather/resources/tmuxbar plugin directories` |
| Step 1: Delete old plugin code | `feat: remove old Tool-based plugin system` |
| Step 2: Create documentation.proto | `feat: add DocumentationService proto` |
| ... | ... |

**Rules:**
- Each step is exactly **one commit**. No combining steps, no splitting a step
  into multiple commits.
- After each commit, update the changelog (see §18.2).
- After each commit, **push** so every step is on the remote.
- If a step fails (build breaks, tests fail), **fix it before moving to the
  next step.** Do not leave the tree broken.
- Always check the current state before starting a step — read files, check
  for errors, verify the build.

### 18.2 Changelog

A file `CHANGELOG.md` lives in the repository root (`~/dotfilesd/CHANGELOG.md`).
It tracks detailed progress per step.

**Format:**
```markdown
# Changelog — Plugin RPC Architecture Rewrite

## [Step N] — Short Name

**Commit:** `<hash>`
**Date:** YYYY-MM-DD

### Changes
- List each file modified, created, or deleted
- One bullet per file with what was done

### State
- [x] Build passes
- [ ] Tests pass (N/A)
- [ ] Daemon starts
- [ ] Plugins load

### Notes
Any decisions made, deviations from the document, or things to revisit.
```

**Rules:**
- The changelog is updated **immediately after each commit**, before starting
  the next step.
- Every file touched in a step is listed. If a step deletes files, list them.
- The "State" checkboxes reflect what was verified for that step. If something
  wasn't tested, leave it unchecked.
- The changelog is committed as part of the step's commit (alongside the code
  changes).

### 18.3 When in Doubt, Stop and Ask

The AI agent **must not** make assumptions or decisions that deviate from this
document. Specifically:

- If a section of this document is ambiguous or incomplete, **stop and ask.**
  Do not fill in the blanks yourself.
- If a command fails with an unexpected error, **stop and ask.** Do not attempt
  creative workarounds.
- If two possible approaches exist and the document doesn't specify which,
  **stop and ask.**
- If a file referenced in this document doesn't exist at the expected path,
  **stop and ask.**

The agent should phrase questions as yes/no or multiple-choice so the user can
answer quickly. Example: "The file `proto/dotfilesd/v1/dotfilesdv1/extension.proto`
does not exist. Should I create it from the spec in §13, or skip this step?"

### 18.4 Safe Rollback

Because every step is a single commit, rolling back is always `git revert <hash>`.
The changelog makes it easy to find which commit to revert.

- To roll back one step: `git revert HEAD`
- To roll back N steps: `git revert HEAD~N..HEAD`
- After reverting, update the changelog to mark the step as reverted.

### 18.5 Pre-Flight Checklist

Before running any command that modifies files (including edit tools):

- [ ] I have read the relevant section of this document in full.
- [ ] I understand what the step produces (files created, files deleted,
      files modified).
- [ ] I have checked the current state of every file I will touch.
- [ ] I know what to do if the build breaks (fix before next step).
- [ ] If I am unsure about any detail, I will ask instead of deciding.
