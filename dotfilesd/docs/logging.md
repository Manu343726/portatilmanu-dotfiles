# Logging System

dotfilesd has a structured, hierarchical logging system with YAML configuration,
colored output, rotating file sinks, and full integration with the plugin ecosystem.

## Architecture

```
                           Log call
                              │
                              ▼
                    ┌──────────────────┐
                    │   namedLogger    │
                    │                  │
                    │  ┌────────────┐  │
                    │  │  Enabled?  │──┼── no ──► return
                    │  └─────┬──────┘  │
                    │        │ yes     │
                    │        ▼         │
                    │  ┌────────────┐  │
                    │  │ buildEntry │  │  ← format entry with timestamp,
                    │  └─────┬──────┘  │    level, module, source, attrs
                    │        │         │
                    │        ▼         │
                    │  ┌────────────┐  │
                    │  │   Sinks    │──┼──► stdout
                    │  │  (foreach) │──┼──► rotating file
                    │  └────────────┘  │     (lumberjack)
                    └──────────────────┘
                              │
                              ▼
                    ┌──────────────────┐
                    │   slog.Handler   │  ← bridge so existing slog code
                    │   (slogHandler)  │     routes through our system
                    └──────────────────┘
```

### Components

| Component | File | Purpose |
|-----------|------|---------|
| `Logger` interface | `logger.go` | Core logging API — `Trace`, `Debug`, `Info`, `Warn`, `Error`, `Fatal`, `Child`, `WithAttrs`, `Enabled` |
| `namedLogger` | `logger.go` | Standard implementation with level filtering, sink routing, and attribute merging |
| `Manager` | `manager.go` | Holds config, sink registry, formatter; creates and caches named loggers |
| `Formatter` | `formatter.go` | Produces colored log lines with timestamp, level, module, source location, and key-value attributes |
| `Sink` interface | `sink.go` | Output destination — `stdoutSink`, `stderrSink`, `rotatingFileSink` (lumberjack) |
| `Config` | `config.go` | YAML-based configuration with loggers, sinks, and formatter settings |
| `slogHandler` | `manager.go` | `slog.Handler` bridge so `log/slog` calls route through our system |
| `Level` | `level.go` | Severity levels: `trace`, `debug`, `info`, `warn`, `error`, `fatal` |

## Log levels

| Level | slog equivalent | Numeric value | Color |
|-------|-----------------|---------------|-------|
| `trace` | `Level(-8)` | `-8` | Gray |
| `debug` | `LevelDebug` | `-4` | Cyan |
| `info` | `LevelInfo` | `0` | Green |
| `warn` | `LevelWarn` | `4` | Yellow |
| `error` | `LevelError` | `8` | Red |
| `fatal` | — | `12` | Red bold |

Each logger can be configured with a minimum level. Messages below that level are
dropped before formatting and sink I/O.

## Log format

Output format:

```
TIMESTAMP LEVEL [module] [file:line] message key1=val1 key2=val2 ...
```

Example:

```
15:04:05.000 INFO  [daemon] loading plugins dir=/home/user/.config/dotfilesd/plugins
15:04:05.001 DEBUG [plugin.weather] forecast requested location=madrid format=brief
15:04:06.500 INFO  [daemon] plugins loaded count=2
```

Each section uses a dedicated ANSI color when color is enabled:
- **Timestamp**: dim white
- **Level**: level-specific (FATAL=red bold, ERROR=red, WARN=yellow, INFO=green, DEBUG=cyan, TRACE=gray)
- **Module** `[name]`: bright blue
- **Source** `[file:line]`: yellow dim (when enabled)
- **Message**: default (reset)
- **Attributes**: dim white/gray

## Configuration

### Daemon setup

Logging is configured in `internal/pkg/daemon/logging.go`. The daemon's `setupLogging()`
method builds a `logging.Config` from the daemon's `Config` struct and calls
`logging.Configure(cfg)`:

```go
func (d *Daemon) setupLogging() {
    cfg := logging.Config{
        Formatter: logging.FormatterConfig{
            TimeFormat:     "15:04:05.000",
            Color:          boolPtr(!isCI()),
            SourceLocation: true,
        },
        Sinks: []logging.SinkConfig{
            {Name: "stdout", Type: "stdout"},
            {Name: "file", Type: "rotating_file",
             Path: logDir + "/dotfilesd.log",
             MaxSizeMB: d.config.LogMaxMB, MaxBackups: d.config.LogBackup, MaxAgeDays: d.config.LogAge},
        },
        Loggers: []logging.LoggerConfig{
            {Name: "root", Level: d.config.LogLevel, Sinks: []string{"stdout", "file"}},
        },
    }
    logging.Configure(cfg)
    d.logger = logging.Global()
}
```

### Default configuration

When no config file is present, the system uses `DefaultConfig()`:

```yaml
formatter:
  time_format: "2006-01-02 15:04:05"
  color: true
  source_location: false
sinks:
  - name: stdout
    type: stdout
loggers:
  - name: root
    level: info
    sinks: [stdout]
default_max_size_mb: 10
default_max_backups: 5
default_max_age_days: 30
```

### Config file (YAML)

Full configuration structure:

```yaml
formatter:
  time_format: "15:04:05.000"       # Go time layout
  color: true                        # ANSI color output
  source_location: true              # [file:line] in output (per-logger override)
  no_color_prefix: false             # suppress color prefix in logs

sinks:
  - name: stdout                     # stdout sink
    type: stdout
  - name: stderr                     # stderr sink
    type: stderr
  - name: file                       # rotating file sink
    type: rotating_file
    path: "/home/user/dotfilesd/logs/dotfilesd.log"
    max_size_mb: 10                  # rotate after 10 MB
    max_backups: 5                   # keep 5 rotated files
    max_age_days: 30                 # delete after 30 days

loggers:
  # Root logger — catch-all defaults
  - name: root
    level: info                      # minimum level: info
    sinks: [stdout, file]            # write to both sinks
    source: false

  # Daemon module — verbose logging
  - name: daemon
    level: debug
    sinks: [stdout, file]

  # Plugin sub-logger — trace level for plugin debugging
  - name: plugin.weather
    level: trace
    sinks: [stdout, file]

default_max_size_mb: 10
default_max_backups: 5
default_max_age_days: 30
```

### Logger name resolution

Loggers are resolved by **longest-prefix match**. When you request a logger named
`"plugin.weather"`, the manager looks for:

1. An exact match on `"plugin.weather"`
2. The closest prefix match: `"plugin"` is a better match than `"root"`
3. Falls back to the `"root"` logger config

This allows you to set fine-grained levels per module:

```yaml
loggers:
  - name: root              # default: info level
    level: info
  - name: daemon            # daemon internals at debug
    level: debug
  - name: daemon.session    # sessions at trace
    level: trace
  - name: plugin            # all plugins at debug
    level: debug
  - name: plugin.weather    # weather plugin at trace
    level: trace
```

## Usage in code

### Package-level logging

```go
import "dotfilesd/internal/pkg/logging"

// Log at various levels
logging.Info("server started", "port", 9105)
logging.Debug("connecting to plugin", "name", pluginName, "pid", pid)

// Attribute helper
logging.Info("request complete", logging.A("duration_ms", 42, "status", 200)...)
```

### Creating a package logger

```go
// internal/pkg/daemon/logger.go
var log = logging.NewPackageLogger("daemon")

// Types can create child loggers:
func (d *Daemon) log() logging.Logger { return log.Child("server") }
```

### The Logger interface

```go
type Logger interface {
    Trace(msg string, attrs ...any)
    Debug(msg string, attrs ...any)
    Info(msg string, attrs ...any)
    Warn(msg string, attrs ...any)
    Error(msg string, attrs ...any)
    Fatal(msg string, attrs ...any)   // calls os.Exit(1) after logging

    Child(name string) Logger         // create sub-logger: "parent.child"
    WithAttrs(attrs ...any) Logger    // attach fixed attributes to all entries
    Enabled(level Level) bool
}
```

### Hierarchical loggers

```go
root := logging.NewPackageLogger("daemon")      // name: "daemon"
srv := root.Child("server")                      // name: "daemon.server"
sess := srv.Child("session")                     // name: "daemon.server.session"

sess.Info("session created", "id", sessionID)
// Output:
// 15:04:05.000 INFO [daemon.server.session] session created id=abc123
```

### Fixed attributes

```go
log := logging.NewPackageLogger("daemon").WithAttrs("component", "executor")
log.Info("running command", "cmd", "ls -la")
// Output:
// 15:04:05.000 INFO [daemon] running command component=executor cmd="ls -la"
```

### slog bridge

Existing code that uses the standard library `log/slog` is automatically bridged
to our logging system via `slogHandler`:

```go
// In daemon/logging.go:
slogHandler := logging.NewSlogHandler(logging.NewPackageLogger("daemon"))
slog.SetDefault(slog.New(slogHandler))

// Any slog call now routes through the logging system:
slog.Info("legacy slog message", "key", "val")
// → 15:04:05.000 INFO [daemon] legacy slog message key=val
```

### Level checking

```go
if log.Enabled(logging.LevelDebug) {
    // Expensive computation for debug attributes
    log.Debug("detailed state", "data", computeExpensiveDebugData())
}
```

## Sinks

### stdoutSink

Writes to `os.Stdout`. Used for primary log output.

### stderrSink

Writes to `os.Stderr`. Separate stream for diagnostics.

### rotatingFileSink

Backed by [lumberjack](https://github.com/natefinch/lumberjack) for log rotation:

| Setting | Description |
|---------|-------------|
| `max_size_mb` | Rotate when file reaches this size (default: 10) |
| `max_backups` | Number of rotated files to keep (default: 5) |
| `max_age_days` | Delete rotated files older than this (default: 30) |
| Compress | Always enabled — rotated files are gzipped |

### Plugin-specific sinks

The `Manager.AddPluginSink()` method creates a dedicated rotating file sink for
a plugin, writing to `<log_dir>/plugins/<name>.log`. This allows isolating plugin
log output from daemon logs (see [Plugin Logging](#plugin-logging) below).

## Plugin Logging

Plugin logging is a multi-hop pipeline that sends log entries from the plugin
subprocess to the daemon's logging system:

```
Plugin tool                               Daemon
───────────                               ──────
ctx.Log().Info("forecasting weather",     pluginLogger.log()
    "location", "Madrid")                      │
        │                                       │ send Log RPC (protobuf)
        │                                       ▼
        │                               pluginBackend.Log(ctx, "weather",
        │                                   "info", "forecasting weather",
        │                                   {"location": "Madrid"})
        │                                       │
        │                                       ▼
        │                               b.daemon.logger.Logger("plugin.weather")
        │                                       │
        │                                       ▼
        │                               namedLogger.log(LevelInfo,
        │                                   "forecasting weather",
        │                                   "location", "Madrid")
        │                                       │
        │                                       ▼
        │                               Sink: stdout / file / plugin.weather.log
```

### Plugin SDK

The plugin SDK (`dotfilesd/plugin/`) provides logging via `ctx.Log()`:

```go
func forecastFn(ctx plugin.Context, args map[string]string) error {
    ctx.Log().Info("forecasting weather", "location", args["location"])

    result, err := ctx.Exec("curl -s wttr.in/Madrid")
    if err != nil {
        ctx.Log().Error("fetch failed", "error", err)
        return err
    }

    ctx.Log().Debug("response received", "size", len(result.Stdout))
    return nil
}
```

The `pluginLogger` type implements `logging.Logger` by sending each log entry
to the daemon via the `ExecutionContext.Log` RPC. Key characteristics:

- **Level filtering is daemon-side**: the plugin always sends all log entries;
  the daemon decides what to keep based on its logger configuration.
- **Attributes are serialized as protobuf `map<string, string>`**: values are
  converted via `fmt.Sprintf()`.
- **Fixed attributes**: `pluginLogger.WithAttrs()` returns a new logger with
  attributes attached to every entry (useful for context propagation).
- **No blocking**: the Log RPC is fire-and-forget (`ctx.Log()` returns
  immediately, errors are logged server-side).

### Daemon-side routing

In `internal/pkg/daemon/plugin.go`, the `pluginBackend.Log()` method:

1. Receives the Log RPC from the plugin
2. Converts `attrs map[string]string` to `[]any` key-value pairs
3. Resolves a named logger: `b.daemon.logger.Logger("plugin." + pluginName)`
4. Routes to the appropriate level method on the named logger

### Level configuration for plugins

The root logger is typically configured at `info` level. That means plugin
`debug` and `trace` messages are filtered by default.

To see verbose plugin output, use `dotfilesctl config reconfigure`:

```sh
# See all plugin debug logs
dotfilesctl config reconfigure --log-level debug

# Or set back to info
dotfilesctl config reconfigure --log-level info
```

For permanent configuration, edit the daemon config to add a plugin-specific logger:

```yaml
loggers:
  - name: root
    level: info
  - name: plugin.weather       # weather plugin at debug
    level: debug
  - name: plugin               # all other plugins at info
    level: info
```

### Plugin log files

Each plugin can have its own dedicated log file. The `Manager.AddPluginSink()`
method creates a `rotatingFileSink` at `<log_dir>/plugins/<name>.log`:

```sh
tail -f ~/dotfilesd/logs/plugins/weather.log
tail -f ~/dotfilesd/logs/plugins/resources.log
```

### Plugin logging best practices

1. **Use structured attributes**, not string formatting:
   ```go
   // Good
   ctx.Log().Info("fetching weather", "location", loc, "format", fmt)
   // Bad
   ctx.Log().Info(fmt.Sprintf("fetching weather for %s in %s format", loc, fmt))
   ```

2. **Log at the right level**:
   - `Trace`: very detailed internal steps (e.g., parsed data, intermediate values)
   - `Debug`: detailed progress (e.g., "request sent", "response received")
   - `Info`: normal operation milestones (e.g., "forecast fetched")
   - `Warn`: unexpected but non-fatal (e.g., "curl returned non-zero exit")
   - `Error`: failures (e.g., "failed to fetch weather")

3. **Use `WithAttrs` for repeated context**:
   ```go
   log := ctx.Log().WithAttrs("plugin", "resources", "version", "1.0")
   log.Info("collecting data")
   log.Debug("cpu stats", "pct", cpuPct)
   ```

4. **Don't log sensitive information** — plugin logs go to the daemon's
   log files, which may be shared or persisted.

5. **Remember level filtering** — `debug` and `trace` messages won't appear
   unless the daemon's log level is set accordingly.

## Runtime log level control

The daemon supports changing the log level at runtime without restart:

```sh
dotfilesctl config reconfigure --log-level debug
dotfilesctl config reconfigure --log-level info
```

This reconfigures the root logger level via the `ConfigService.Reconfigure` RPC,
which also updates the slog bridge for backward compatibility.

## Debug stderr output

During development, the daemon and plugin SDK support diagnostic stderr output.
This is **never committed** and is used only for troubleshooting the logging
pipeline itself:

- Plugin-side: `fmt.Fprintf(os.Stderr, "plugin[%s] calling Log RPC: ...")`
- Daemon-side: `fmt.Fprintf(os.Stderr, "DEBUG_PLUGIN_LOG_BEFORE/AFTER: ...")`
- Logger: `fmt.Fprintf(os.Stderr, "DEBUG_NLOGGER_*: ...")`

These are removed from the codebase once the logging issue is resolved.

## Viewing logs

### Daemon logs (systemd)

```sh
journalctl --user -u dotfilesd -f            # follow
journalctl --user -u dotfilesd -n 100        # last 100 lines
journalctl --user -u dotfilesd --no-pager    # full dump
```

### Rotated log files

```sh
tail -f ~/dotfilesd/logs/dotfilesd.log
tail -f ~/dotfilesd/logs/plugins/weather.log    # plugin-specific (if configured)
```

### Filtering plugin logs

```sh
journalctl --user -u dotfilesd -f | grep -i '\[plugin\.'
# Or via the log file:
grep '\[plugin\.' ~/dotfilesd/logs/dotfilesd.log
```
