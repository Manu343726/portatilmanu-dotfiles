# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [dotfilesd.v1.PluginRegistryService](#dotfilesdv1pluginregistryservice)
    - [GetPlugin](#getplugin)
    - [ListPlugins](#listplugins)
    - [LoadPlugin](#loadplugin)
    - [UnloadPlugin](#unloadplugin)
    - [ReloadPlugins](#reloadplugins)
  - [dotfilesd.v1.PluginExecutorService](#dotfilesdv1pluginexecutorservice)
    - [CallPlugin](#callplugin)
- [Messages](#messages)
  - [RegistryGetPluginRequest](#registrygetpluginrequest)
  - [RegistryGetPluginResponse](#registrygetpluginresponse)
  - [RegistryListPluginsRequest](#registrylistpluginsrequest)
  - [RegistryListPluginsResponse](#registrylistpluginsresponse)
  - [RegistryLoadPluginRequest](#registryloadpluginrequest)
  - [RegistryLoadPluginResponse](#registryloadpluginresponse)
  - [RegistryUnloadPluginRequest](#registryunloadpluginrequest)
  - [RegistryUnloadPluginResponse](#registryunloadpluginresponse)
  - [RegistryReloadPluginsRequest](#registryreloadpluginsrequest)
  - [RegistryReloadPluginsResponse](#registryreloadpluginsresponse)
  - [CallPluginMessage](#callpluginmessage)
  - [WindowSize](#windowsize)
  - [FieldSchema](#fieldschema)
  - [EnumValue](#enumvalue)
  - [EnumSchema](#enumschema)
  - [MessageSchema](#messageschema)
  - [MethodSchema](#methodschema)
  - [ServiceSchema](#serviceschema)
- [Enums](#enums)
  - [FieldKind](#fieldkind)
  - [FieldLabel](#fieldlabel)

## Services

### dotfilesd.v1.PluginRegistryService

PluginRegistryService is exposed by the daemon so plugins can discover
and connect to each other's custom RPC services.

This is a READ-ONLY service. It serves as the single source of truth for
plugin metadata. The daemon populates the registry at plugin load time via
grpcreflect, extracting full type introspection data (service names,
methods, request/response message schemas with all fields, types, enums).
Clients (CLI, MCP bridge, plugins) read from the registry exclusively —
they never need to perform their own grpcreflect against plugin processes.

#### GetPlugin

GetPlugin returns connection info for a named plugin.

- **Request:** `dotfilesd.v1.RegistryGetPluginRequest`
- **Response:** `dotfilesd.v1.RegistryGetPluginResponse`

#### ListPlugins

ListPlugins returns all registered plugins.

- **Request:** `dotfilesd.v1.RegistryListPluginsRequest`
- **Response:** `dotfilesd.v1.RegistryListPluginsResponse`

#### LoadPlugin

LoadPlugin loads a plugin by name, including its dependencies.

- **Request:** `dotfilesd.v1.RegistryLoadPluginRequest`
- **Response:** `dotfilesd.v1.RegistryLoadPluginResponse`

#### UnloadPlugin

UnloadPlugin stops a plugin by name.

- **Request:** `dotfilesd.v1.RegistryUnloadPluginRequest`
- **Response:** `dotfilesd.v1.RegistryUnloadPluginResponse`

#### ReloadPlugins

ReloadPlugins rescans the plugins directory, loading new plugins
and removing plugins whose directories no longer exist.

- **Request:** `dotfilesd.v1.RegistryReloadPluginsRequest`
- **Response:** `dotfilesd.v1.RegistryReloadPluginsResponse`

### dotfilesd.v1.PluginExecutorService

PluginExecutorService proxies RPC calls to plugins. The CLI opens a
bidi stream, sends the request, and receives real-time stdout/stderr
chunks from the plugin followed by the final response.

#### CallPlugin

CallPlugin opens a bidi stream between a client (CLI/MCP) and a plugin.
1. Client sends CallPluginRequest (plugin, service, method, body)
2. Daemon forwards the call to the plugin, connecting the client's
stdin/stdout/stderr to the plugin's via the daemon
3. Plugin responds with chunks (stdout, stderr, final response)
4. Client can send stdin chunks for interactive plugins

- **Request:** `dotfilesd.v1.CallPluginMessage`
- **Response:** `dotfilesd.v1.CallPluginMessage`


## Messages

### RegistryGetPluginRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `plugin_name` | string | Name of the plugin to look up. |

### RegistryGetPluginResponse

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Plugin identifier name (e.g. "weather"). |
| `display_name` | string | Human-readable display name (e.g. "Weather Service"). |
| `version` | string | Plugin version string. |
| `description` | string | Plugin description from its front-matter. |
| `url` | string | Base URL of the plugin's RPC server (e.g. "http://127.0.0.1:12345"). |
| `services` | repeated string | Names of custom RPC services exposed by this plugin. |
| `schemas` | repeated dotfilesd.v1.ServiceSchema | Full type introspection data for every service exposed by this plugin. Populated by the daemon at load time via grpcreflect. |

### RegistryListPluginsRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### RegistryListPluginsResponse

| Field | Type | Description |
|-------|------|-------------|
| `plugins` | repeated dotfilesd.v1.RegistryGetPluginResponse |  |

### RegistryLoadPluginRequest

| Field | Type | Description |
|-------|------|-------------|
| `plugin_name` | string |  |

### RegistryLoadPluginResponse

| Field | Type | Description |
|-------|------|-------------|
| `plugin` | dotfilesd.v1.RegistryGetPluginResponse | The loaded plugin's metadata. |
| `loaded_deps` | repeated string | Dependencies loaded alongside this plugin. |
| `error` | string | Error message if the plugin could not be loaded (empty on success). |

### RegistryUnloadPluginRequest

| Field | Type | Description |
|-------|------|-------------|
| `plugin_name` | string |  |

### RegistryUnloadPluginResponse

| Field | Type | Description |
|-------|------|-------------|
| `error` | string | Error message if the plugin could not be unloaded (empty on success). |

### RegistryReloadPluginsRequest

### RegistryReloadPluginsResponse

| Field | Type | Description |
|-------|------|-------------|
| `loaded` | repeated string | Names of plugins that were loaded during the reload. |
| `unloaded` | repeated string | Names of plugins that were unloaded during the reload. |
| `error` | string | Error message if the reload encountered issues (empty on success). |

### CallPluginMessage

| Field | Type | Description |
|-------|------|-------------|
| `plugin_name` | string | Target plugin name (e.g. "weather"). |
| `service` | string | Target service name (e.g. "WeatherService"). |
| `method` | string | Target method name (e.g. "Forecast"). |
| `request_body` | bytes | Serialized request body (protobuf binary). |
| `client_id` | string | Unique client identifier for stream multiplexing. |
| `render_output` | bool | When true, plugin writes human-readable output to stdout. When false, returns structured JSON. |
| `stdout_chunk` | bytes | Stdout chunk streamed from the plugin. |
| `stderr_chunk` | bytes | Stderr chunk streamed from the plugin. |
| `stdin_chunk` | bytes | Stdin chunk sent to the plugin (for interactive sessions). |
| `window_size` | dotfilesd.v1.WindowSize | Terminal resize notification from the client. |
| `response_body` | bytes | Serialized response body (protobuf binary). |
| `error` | string | Error message if the call failed (empty on success). |

### WindowSize

WindowSize carries terminal dimension changes from the CLI caller
to the plugin's PTY-backed TTYConn.

| Field | Type | Description |
|-------|------|-------------|
| `width` | int32 |  |
| `height` | int32 |  |

### FieldSchema

FieldSchema describes a single field in a protobuf message.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Field name (e.g. "location"). |
| `description` | string | Human-readable description of the field. |
| `kind` | dotfilesd.v1.FieldKind | Protobuf field type kind. |
| `label` | dotfilesd.v1.FieldLabel | Whether the field is optional, required, or repeated. |
| `type_name` | string | For message/enum kinds, the fully-qualified type name. e.g. "weather.Unit" or "weather.ForecastRequest". Clients can look up the full schema by correlating with MessageSchema/enum names in the enclosing ServiceSchema tree. |
| `enum_schema` | dotfilesd.v1.EnumSchema | Inline enum schema when this field is of enum kind. This saves clients from having to cross-reference a separate type registry. |

### EnumValue

EnumValue describes a single enum value.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Enum value name (e.g. "COLOR_RED"). |
| `number` | int32 | Numeric value of this enum entry. |
| `description` | string | Description extracted from proto source comments. |

### EnumSchema

EnumSchema describes a protobuf enum type.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Fully-qualified enum name (e.g. "weather.Unit"). |
| `description` | string | Human-readable description of the enum. |
| `values` | repeated dotfilesd.v1.EnumValue | Enum value definitions. |

### MessageSchema

MessageSchema describes a protobuf message type, with full recursive
field metadata. Nested messages and enums are inlined so clients have
everything they need without additional lookups.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Fully-qualified message name (e.g. "weather.ForecastRequest"). |
| `description` | string | Human-readable description of the message. |
| `fields` | repeated dotfilesd.v1.FieldSchema | Fields declared directly on this message. |
| `enums` | repeated dotfilesd.v1.EnumSchema | Enum types nested inside this message. |
| `messages` | repeated dotfilesd.v1.MessageSchema | Message types nested inside this message. |

### MethodSchema

MethodSchema describes an RPC method with full request/response schemas.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Method name (e.g. "Forecast"). |
| `description` | string | Human-readable description of the method. |
| `request` | dotfilesd.v1.MessageSchema | Full schema of the request message. |
| `response` | dotfilesd.v1.MessageSchema | Full schema of the response message. |
| `needs_interactive_stdin` | bool | True when this method requires interactive stdin (e.g. TUI games). The daemon uses this hint to decide whether to set up raw terminal mode and stdin forwarding for CLI calls to this method. |

### ServiceSchema

ServiceSchema describes a service — the full introspection result for one
service exposed by a plugin.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Fully-qualified service name (e.g. "weather.WeatherService"). |
| `description` | string | Human-readable description of the service. |
| `methods` | repeated dotfilesd.v1.MethodSchema | RPC methods exposed by this service. |


## Enums

### FieldKind

FieldKind mirrors protobuf field types.

| Name | Number | Description |
|------|--------|-------------|
| `FIELD_KIND_UNSPECIFIED` | 0 |  |
| `FIELD_KIND_DOUBLE` | 1 |  |
| `FIELD_KIND_FLOAT` | 2 |  |
| `FIELD_KIND_INT64` | 3 |  |
| `FIELD_KIND_UINT64` | 4 |  |
| `FIELD_KIND_INT32` | 5 |  |
| `FIELD_KIND_FIXED64` | 6 |  |
| `FIELD_KIND_FIXED32` | 7 |  |
| `FIELD_KIND_BOOL` | 8 |  |
| `FIELD_KIND_STRING` | 9 |  |
| `FIELD_KIND_BYTES` | 10 |  |
| `FIELD_KIND_UINT32` | 11 |  |
| `FIELD_KIND_SFIXED32` | 15 |  |
| `FIELD_KIND_SFIXED64` | 16 |  |
| `FIELD_KIND_SINT32` | 17 |  |
| `FIELD_KIND_SINT64` | 18 |  |
| `FIELD_KIND_ENUM` | 13 |  |
| `FIELD_KIND_MESSAGE` | 14 |  |

### FieldLabel

FieldLabel indicates whether a field is optional, required, or repeated.

| Name | Number | Description |
|------|--------|-------------|
| `FIELD_LABEL_UNSPECIFIED` | 0 |  |
| `FIELD_LABEL_OPTIONAL` | 1 |  |
| `FIELD_LABEL_REQUIRED` | 2 |  |
| `FIELD_LABEL_REPEATED` | 3 |  |

