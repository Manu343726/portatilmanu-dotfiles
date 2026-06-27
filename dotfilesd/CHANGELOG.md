# Changelog — Plugin RPC Architecture Rewrite

## [Step 0.0] — Document: Development Workflow Rules

**Commit:** `37786d6`
**Date:** 2026-06-27

### Changes
- `docs/plugin-rpc-architecture.md`: Added §18 (Development Workflow) with rules
  for one-commit-per-step, changelog updates, ask-when-unsure, safe rollback,
  and pre-flight checklist.
- `CHANGELOG.md`: Created this file.

### State
- [x] Document updated
- [ ] Build passes (N/A — doc-only change)
- [ ] Daemon starts (N/A)

### Notes
First changelog entry. Previous commits (a26978c, 0faa6a3) are the document
foundation but are not tracked here since the changelog didn't exist yet.

---

## [Step 0] — Scaffold Plugin Directories

**Commit:** `e6a9e01`
**Date:** 2026-06-27

### Changes
- `.config/dotfilesd/plugins/resources/proto/resources/.gitkeep`: Created proto
  directory for resources plugin.
- `.config/dotfilesd/plugins/tmuxbar/`: Created new plugin directory.
- `.config/dotfilesd/plugins/tmuxbar/go.mod`: Module `plugins/tmuxbar` with
  replace directives for `dotfilesd` and `plugins/resources`.
- `.config/dotfilesd/plugins/tmuxbar/proto/tmuxbar/.gitkeep`: Created proto
  directory for tmuxbar plugin.

### State
- [ ] Build passes (N/A — no code yet, directories only)
- [ ] Daemon starts (N/A)

### Notes
Weather plugin directory was already scaffolded (proto/ and go.mod existed from
earlier work). Resources plugin had go.mod but no proto/ — created it. Tmuxbar
is entirely new. Empty proto dirs use `.gitkeep` so git tracks the structure.

---

## [Step 1] — Delete ALL Old Plugin Code

**Commit:** `0c0b30c`
**Date:** 2026-06-27

### Changes
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/plugin_base.proto`: Deleted (old
  PluginBaseService proto).
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/plugin_base.pb.go`: Deleted (generated).
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/plugin_base.connect.go`: Deleted (generated).
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/extension.pb.go`: Deleted (generated
  from extension.proto, which was already deleted).
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/extension.connect.go`: Deleted (generated).
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/plugin.pb.go`: Deleted (generated
  from plugin.proto, which was already deleted).
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/plugin.connect.go`: Deleted (generated).
- `.config/dotfilesd/plugins/resources/main.go`: Deleted (old Tool-based plugin).
- `dotfilesd/plugin/serve.go`: Removed Tool interface, NewTool, simpleTool,
  extensionServiceServer, pluginBaseServiceServer, streamWriter, Config.Tools
  field, and all PluginBaseService/ExtensionService mounting code.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/plugin_registry.proto`: Updated —
  removed import of plugin_base.proto, flattened GetInfoResponse fields into
  RegistryGetPluginResponse, changed `repeated ServiceDescriptor services` to
  `repeated string services`.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/plugin_registry.pb.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/plugin_registry.connect.go`: Regenerated.

### State
- [x] Plugin SDK (`plugin/...`) builds
- [x] Proto package (`proto/...`) builds
- [ ] Full daemon (`./...`) builds — expected to fail, manager.go and CLI files
      reference deleted types and are scheduled for rewrite in steps 9-13.

### Notes
The daemon's `internal/pkg/plugin/manager.go` and `internal/pkg/cli/` packages
still compile against deleted types. They will be fully rewritten in steps 9-13.
The weather plugin main.go was NOT deleted — it already uses the new RPC
architecture with `plugin.Serve()` and Connect handlers, not the old Tool API.

---

## [Steps 2-4] — Documentation Proto + Regenerate

**Commit:** `f8b57c2`
**Date:** 2026-06-27

### Changes
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/documentation.proto`: Created with
  DocumentationService, DocumentationRequest, DocumentationResponse messages.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/documentation.pb.go`: Generated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/documentation.connect.go`: Generated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/config.connect.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/dotfiles.connect.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/exec.connect.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/feedback.connect.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/log.connect.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/plugin_registry.connect.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/script.connect.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/session.connect.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/system.connect.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/config.pb.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/documentation.pb.go`: Generated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfiles.pb.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/exec.pb.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/feedback.pb.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/log.pb.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/plugin_registry.pb.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/script.pb.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/session.pb.go`: Regenerated.
- `dotfilesd/proto/dotfilesd/v1/dotfilesdv1/system.pb.go`: Regenerated.

### State
- [x] Proto package (`proto/...`) builds

### Notes
Step 3 (Keep plugin_registry.proto) was already handled in Step 1 — the proto
was updated to remove the plugin_base import, and was regenerated. The generated
files are gitignored so only the .proto source is tracked.

---

## [Steps 5+7] — Rewrite serve.go + Create docs.go

**Commit:** `437b6ea`
**Date:** 2026-06-27

### Changes
- `dotfilesd/plugin/serve.go`: Added grpcreflect handler mounting
  (NewHandlerV1 + NewHandlerV1Alpha with static reflector listing all
  services). Added default DocumentationService mounting (auto-skipped if
  plugin provides its own). Added service name collection for reflector.
  Added imports for `connectrpc.com/grpcreflect` and
  `dotfilesdv1connect`.
- `dotfilesd/plugin/docs.go`: Created with default
  `documentationServiceServer` implementation. Returns markdown-formatted
  docs from Config fields at plugin level and per-service level.
- `dotfilesd/go.mod`: `connectrpc.com/grpcreflect` promoted from indirect
  to direct dependency.
- `dotfilesd/go.sum`: Updated by `go mod tidy`.

### State
- [x] Plugin SDK (`plugin/...`) builds

### Notes
Steps 5 and 7 are combined because step 5 (mount DocumentationService)
needs the type from step 7 (docs.go). Step 6 (context.go rewrite) is
next.

---

## [Step 6] — Rewrite context.go

**Commit:** `053c51c`
**Date:** 2026-06-27

### Changes
- `dotfilesd/plugin/context.go`: Removed `streamingContext` type and all
  its methods (`Stdout`, `Stderr`, `Log`, `ExecStream`, `BackgroundExec`).
  This was dead code after the old Tool-based `extensionServiceServer`
  was removed in Step 1. The `execStreamWithWriters` helper function is
  preserved — still used by `contextClient.ExecStream`.

### State
- [x] Plugin SDK (`plugin/...`) builds

### Notes
`pluginClient` and `CallPlugin` were already absent from the codebase
(removed in earlier sessions). Step 8 (Build SDK) is implicitly verified
by every build check in steps 5-7.

---

## [Steps 9-13] — Daemon-Side Rewrite (Manager + Registry + CLI)

**Commit:** `57e20ee`
**Date:** 2026-06-28

### Changes
- `internal/pkg/plugin/manager.go`:
  - Rewrote `PluginInfo` with flat fields (Name, DisplayName, Version,
    Description, URL, Services []string) instead of old
    `*dotfilesdv1.GetInfoResponse` / `[]*dotfilesdv1.ServiceDescriptor`.
  - Replaced `PluginBaseService.GetInfo/ListServices` calls with
    grpcreflect `NewClient().NewStream().ListServices()` discovery.
  - Added handshake struct with name/version/description fields.
  - Added `stepProto()` function for proto compilation before go build.
  - Removed unused imports.
- `internal/pkg/daemon/plugin.go`: Updated to use new flat PluginInfo
  fields (p.Version, p.DisplayName instead of p.Info.Version).
- `internal/pkg/daemon/registry_svc.go`: Updated to populate flat
  RegistryGetPluginResponse fields (DisplayName, Version, Description)
  instead of old GetInfoResponse/ServiceDescriptor.
- `internal/pkg/cli/plugin.go`:
  - Removed RunCallPluginTool, RunListPluginTools, ListPluginTools,
    CallPluginToolViaMCP, splitQualifiedName, containsString,
    propertyTypeToString, schemaTypeToString functions.
  - Updated RunListPlugins to use flat RegistryGetPluginResponse fields.
- `internal/pkg/cli/client.go`: Removed `Plugin` field from Clients
  struct and NewClients constructor.
- `internal/pkg/cli/mcp.go`:
  - Rewrote getPluginTools to use PluginRegistryService.
  - Removed old plugin tool dispatch in default case (CallPluginToolViaMCP).
- `cmd/dotfilesctl/root.go`:
  - Removed old ListPluginTree/registerPluginTreeEntry dispatch.
  - Simplified registerDynamicCommands to use Registry.ListPlugins.
  - Added registerPluginCommand for registry-based plugin listing.
  - Removed splitKeyValue function.
- `.config/dotfilesd/plugins/weather/go.mod` / `go.sum`: Updated by
  `go mod tidy`.

### State
- [x] Full daemon (`./...`) builds
- [x] Daemon binary (`cmd/dotfilesd`) builds
- [x] CLI binary (`cmd/dotfilesctl`) builds
- [x] Weather plugin builds
- [ ] Daemon starts and loads plugins (not yet tested)
- [ ] Plugin RPCs work (not yet tested)

### Notes
The dynamic cobra command generation from proto reflection (doc §5a)
is NOT yet implemented — plugins are registered as simple info-only
commands. The full proto-based flag generation will come in a later
phase. The MCP tool dispatch for plugins now shows plugins as
individual tools rather than exposing individual tool commands.

---

## [Steps 14-15] — Rewrite Plugins (resources + tmuxbar)

**Commit:** `1f270f7`
**Date:** 2026-06-28

### Changes
- `.config/dotfilesd/plugins/resources/proto/resources/resources.proto`: Created
  with ResourcesService (Current, Top, PS, History) and shared data types
  (RAMSapshot, CPUSnapshot, DiskSnapshot, DiskIOSnapshot, ProcessInfo).
- `.config/dotfilesd/plugins/resources/proto/resources/resources.pb.go`: Generated.
- `.config/dotfilesd/plugins/resources/proto/resources/resourcesconnect/resources.connect.go`: Generated.
- `.config/dotfilesd/plugins/resources/main.go`: Created from scratch with
  background collector (RAM, CPU, disk, disk I/O every 3s), ring buffer
  history, and RPC handlers reading from shared state.
- `.config/dotfilesd/plugins/resources/go.mod` / `go.sum`: Updated.
- `.config/dotfilesd/plugins/tmuxbar/proto/tmuxbar/tmuxbar.proto`: Created
  with TmuxBarService (RAMWidget, CPUWidget, CPUTempWidget, BatteryWidget,
  StatusBar).
- `.config/dotfilesd/plugins/tmuxbar/proto/tmuxbar/tmuxbar.pb.go`: Generated.
- `.config/dotfilesd/plugins/tmuxbar/proto/tmuxbar/tmuxbarconnect/tmuxbar.connect.go`: Generated.
- `.config/dotfilesd/plugins/tmuxbar/main.go`: Created with widget RPCs
  that call resources plugin's ResourcesService.Current for data.
- `.config/dotfilesd/plugins/tmuxbar/go.mod` / `go.sum`: Updated.

### State
- [x] Daemon builds
- [x] CLI builds
- [x] Weather plugin builds
- [x] Resources plugin builds
- [x] Tmuxbar plugin builds
- [ ] Daemon starts and loads plugins (not yet tested)
- [ ] Plugin RPCs work (not yet tested)

### Notes
The weather plugin proto and main.go already existed from earlier work
and was not modified in this step. The tmuxbar plugin depends on the
resources plugin at runtime (discovers it via registry). The resources
plugin background collector requires daemon context (ctx.Exec) to work
— it will only start collecting after the plugin server starts and the
background goroutine is launched by plugin.Serve().

---

## [Step 16] — Update Makefile

**Commit:** `5d48361`
**Date:** 2026-06-28

### Changes
- `dotfilesd/Makefile`:
  - Added `plugin-proto` target for per-plugin proto compilation.
  - Made `plugin-build` depend on `plugin-proto`.
  - Updated `plugin-build-all` to use `$(MAKE) plugin-build` (which
    includes proto compilation) instead of direct `go build` calls.

### State
- [x] Daemon builds
- [x] CLI builds
- [x] All plugins build
- [ ] Daemon starts and loads plugins (not yet tested)

---

## [CLI] — Proto-Based Dynamic Command Generation

**Commit:** `934d68e`
**Date:** 2026-06-28

### Changes
- `dotfilesd/go.mod`: Added `github.com/fullstorydev/grpcurl` dependency for
  proto reflection-based CLI command generation.
- `dotfilesd/go.sum`: Updated by `go get` and `go mod tidy`.
- `dotfilesd/internal/pkg/cli/protoflags.go`: Created with:
  - `BuildPluginCommand()` — builds cobra command trees from plugin registry
    info using gRPC reflection to discover services, methods, and field types.
  - `addFlagsFromMessage()` — recursively generates typed cobra flags
    (string, int, float, bool, enum with completion, nested messages with dot
    notation, maps, repeated fields) from protobuf message descriptors.
  - `makeRunE()` — builds JSON request body from flags and POSTs to Connect
    RPC endpoint, pretty-printing the JSON response.
  - `camelToKebab()` — converts field names to kebab-case flag names.
  - Falls back to static info-only command if reflection is unavailable.
- `dotfilesd/cmd/dotfilesctl/root.go`: Replaced `registerPluginCommand()` with
  `cli.BuildPluginCommand()` which generates typed commands from proto schemas.
  Removed dead `registerPluginCommand` function and unused `"strings"` import.
- Formatting fixes in `.config/dotfilesd/plugins/resources/main.go`,
  `.config/dotfilesd/plugins/tmuxbar/main.go`, and proto files.

### State
- [x] Full build passes
- [x] `go vet` clean
- [x] `go mod tidy` clean
- [ ] Proto-based command generation tested (requires running plugins)

### Notes
The proto-based command generation connects to plugins via gRPC reflection
to get service/method/field descriptors. If a plugin is unreachable (not
loaded or not running), the command falls back to static info display. The
MCP tool listing still uses the simple one-tool-per-plugin approach; MCP
can be extended for per-method tool definitions in a future session.
**Date:** 2026-06-27

### Changes
- `.config/dotfilesd/plugins/resources/proto/resources/.gitkeep`: Created proto
  directory for resources plugin.
- `.config/dotfilesd/plugins/tmuxbar/`: Created new plugin directory.
- `.config/dotfilesd/plugins/tmuxbar/go.mod`: Module `plugins/tmuxbar` with
  replace directives for `dotfilesd` and `plugins/resources`.
- `.config/dotfilesd/plugins/tmuxbar/proto/tmuxbar/.gitkeep`: Created proto
  directory for tmuxbar plugin.

### State
- [ ] Build passes (N/A — no code yet, directories only)
- [ ] Daemon starts (N/A)

### Notes
Weather plugin directory was already scaffolded (proto/ and go.mod existed from
earlier work). Resources plugin had go.mod but no proto/ — created it. Tmuxbar
is entirely new. Empty proto dirs use `.gitkeep` so git tracks the structure.
