# Protobuf RPC Design Patterns

> **Always read this before designing or modifying any proto file.**
> Violations (stringly-typed options, comma-separated fields, missing enums,
> undocumented messages) must be flagged in code review.

## 1. Use enums for fixed sets of options

**Bad** — stringly-typed, no validation at the proto level, callers must guess values:

```protobuf
message ListRequest {
  string status = 1;  // "online", "offline", "authorized", "unauthorized"
}
```

**Good** — enum enforces valid values, self-documenting, CLI generates `--status` flag with `ValidArgs`:

```protobuf
enum MemberStatus {
  MEMBER_STATUS_UNSPECIFIED = 0;  // first value = 0, semantically "not set"
  MEMBER_STATUS_ONLINE = 1;
  MEMBER_STATUS_OFFLINE = 2;
  MEMBER_STATUS_AUTHORIZED = 3;
  MEMBER_STATUS_UNAUTHORIZED = 4;
}

message ListRequest {
  MemberStatus status = 1;  // empty = no filter
}
```

### Enum naming conventions

- `PascalCase` for the enum type name.
- `UPPER_SNAKE_CASE` for values.
- First value is always `ENUM_TYPE_UNSPECIFIED = 0` (proto3 requirement for zero-value semantics).
- The `_UNSPECIFIED` value means "not provided" / "no filter" — do NOT name it `_NONE` or `_ANY`.

## 2. Use `repeated` for multi-value fields, not comma-separated strings

**Bad** — caller must split manually, no proto-level type safety:

```protobuf
message ListRequest {
  string fields = 2;  // comma-separated, e.g. "name,ip,status"
}
```

**Good** — type-safe, repeated, each value validated by the enum:

```protobuf
enum Column {
  COLUMN_UNSPECIFIED = 0;
  COLUMN_NODE_ID = 1;
  COLUMN_NAME = 2;
  COLUMN_IP = 3;
  COLUMN_STATUS = 4;
}

message ListRequest {
  repeated Column fields = 2;  // empty = default set
}
```

## 3. Use `bool` for binary toggles, not strings

**Bad:**

```protobuf
message Request {
  string detailed = 1;  // "true" / "false"
}
```

**Good:**

```protobuf
message Request {
  bool detailed = 1;
}
```

## 4. Use messages to group related options

**Bad** — flat namespace of unrelated fields:

```protobuf
message ListRequest {
  string network_id = 1;
  string status = 2;
  string name_filter = 3;
  string fields = 4;
  string output = 5;
}
```

**Good** — group filter options and display options into sub-messages:

```protobuf
message MemberFilter {
  MemberStatus status = 1;
  string name_substring = 2;        // free-text filter is fine as string
}

message DisplayOptions {
  repeated Column fields = 1;       // empty = default set
  OutputFormat format = 2;          // enum: TABLE, RAW
}

message ListMembersRequest {
  string network_id = 1;
  MemberFilter filter = 2;
  DisplayOptions display = 3;
}
```

## 5. Document every field, value, and RPC

Proto comments are the PRIMARY source of CLI help text and MCP tool descriptions.
Every element must be documented:

- **Service** — what this service provides, any prerequisites (e.g. API tokens).
- **RPC method** — what the method does, when to use it, what the response contains.
- **Message** — what data this message represents.
- **Field** — the field's purpose, format, acceptable values, units, edge cases.
- **Enum** — what the enum represents, how each value behaves.
- **Enum value** — when to use this value over others, any side effects.

### Bad — no documentation, incomprehensible CLI help:

```protobuf
message Member {
  string id = 1;
  string name = 2;
  bool authorized = 3;
}
```

CLI help becomes:
```
--id string    id
--name string  name
--authorized   authorized
```

### Good — fully documented, CLI help is self-explanatory:

```protobuf
// A member (peer node) of a ZeroTier network.
message Member {
  // Composite member ID in the form "<networkId>-<nodeId>".
  string id = 1;
  // Human-readable name assigned in ZeroTier Central (e.g. "my-laptop").
  string name = 2;
  // Whether this member is authorized to communicate on the network.
  // Unauthorized members cannot send or receive traffic but may still
  // appear in the member list with a pending status.
  bool authorized = 3;
}
```

CLI help becomes:
```
--id string          Composite member ID in the form "<networkId>-<nodeId>".
--name string        Human-readable name assigned in ZeroTier Central (e.g. "my-laptop").
--authorized         Whether this member is authorized to communicate on the network.
```

### Documentation conventions

- Start with a capital letter, end with a period.
- Use the first line as a short summary (shown in `--help` summaries).
- Add a blank comment line then a longer explanation for details, edge cases,
  or examples. The CLI help shows the full text.
- Document units: `Unix timestamp (seconds since epoch)`, `duration in milliseconds`.
- Document constraints: `10 hex characters`, `between 0 and 100`.
- Document the zero/default value: `0 means never seen`, `empty string means no filter`.

### DocsProto is automatic

After compiling the proto with `make plugin-proto`, the generated
`proto/<name>/<name>_docs.go` file automatically registers the documentation
with `plugin.DefaultDocs` in its `init()` function. The SDK picks this up
automatically — plugins do NOT need to pass `DocsProto` explicitly.

## 6. Prefer `int32`/`int64` over `string` for numeric identifiers

**Bad:**

```protobuf
message Network {
  string creation_time = 1;  // "2024-01-15T10:30:00Z"
}
```

**Good:**

```protobuf
message Network {
  int64 creation_time = 1;  // Unix timestamp (seconds since epoch)
}
```

## 7. Use `OutputFormat` enum instead of a `string output`

```protobuf
enum OutputFormat {
  OUTPUT_FORMAT_UNSPECIFIED = 0;
  OUTPUT_FORMAT_TABLE = 1;
  OUTPUT_FORMAT_RAW = 2;
  OUTPUT_FORMAT_JSON = 3;
}
```

## 8. Default value conventions

- For enums: `UNSPECIFIED = 0` means "let the server choose the default."
- For bools: `false` is the zero value — design your proto so `false` is a safe default.
- For `repeated`: empty list means "no filter / use defaults." The server code should handle this without error.
- For `string` filters: empty string means "no filter."

## 9. Complete documented example

```protobuf
syntax = "proto3";
package zerotier;
option go_package = "plugins/zerotier/proto/zerotier";

// ZeroTierService provides access to ZeroTier Central API for listing
// networks and their members with IP assignments.
service ZeroTierService {
  // ListNetworks returns all networks accessible by the configured API token.
  // Each network includes its ID, name, description, and member count.
  rpc ListNetworks(ListNetworksRequest) returns (ListNetworksResponse);
  // ListMembers returns all members of a specific network with IPs and status.
  // Supports filtering by connection status and name substring, as well as
  // column selection and output format control.
  rpc ListMembers(ListMembersRequest) returns (ListMembersResponse);
}

// Filter by member connection and authorization status.
enum MemberStatus {
  // No status filter — return all members.
  MEMBER_STATUS_UNSPECIFIED = 0;
  // Only members currently online (seen within the last 5 minutes).
  MEMBER_STATUS_ONLINE = 1;
  // Only members currently offline.
  MEMBER_STATUS_OFFLINE = 2;
  // Only authorized members.
  MEMBER_STATUS_AUTHORIZED = 3;
  // Only unauthorized members.
  MEMBER_STATUS_UNAUTHORIZED = 4;
}

// Columns available for table display output.
enum Column {
  COLUMN_UNSPECIFIED = 0;
  COLUMN_NODE_ID = 1;   // ZeroTier node ID (10 hex chars).
  COLUMN_NAME = 2;      // Human-readable member name.
  COLUMN_IP = 3;        // Assigned IP addresses.
  COLUMN_STATUS = 4;    // Connection status (online/offline/unauthorized).
  COLUMN_DESCRIPTION = 5;  // User-defined description.
  COLUMN_VERSION = 6;      // ZeroTier client version.
  COLUMN_PHYSICAL_ADDRESS = 7;  // External IP:port.
}

// Output format for display output.
enum OutputFormat {
  OUTPUT_FORMAT_UNSPECIFIED = 0;  // Default (table).
  OUTPUT_FORMAT_TABLE = 1;        // Formatted aligned columns.
  OUTPUT_FORMAT_RAW = 2;          // Key: value lines per member.
}

// Filters applied to the member list before returning.
message MemberFilter {
  // Only include members matching this status.
  // Unset means no status filter.
  MemberStatus status = 1;
  // Case-insensitive substring match against member name.
  // Empty means no name filter.
  string name_substring = 2;
}

// Controls how members are rendered in human-readable output.
message DisplayOptions {
  // Columns to include. Empty or unspecified means the server default.
  repeated Column fields = 1;
  // Output rendering format. Unset means table.
  OutputFormat format = 2;
}

message ListMembersRequest {
  // Network ID to list members for (e.g. "8056c2e21c000001").
  // If empty and exactly one network exists, auto-detected.
  string network_id = 1;
  // Optional filters to narrow the member list.
  MemberFilter filter = 2;
  // Optional display options for human-readable output.
  DisplayOptions display = 3;
}

// A member (peer node) of a ZeroTier network.
message Member {
  // Composite member ID in the form "<networkId>-<nodeId>".
  string id = 1;
  // ZeroTier node ID (10 hex characters, e.g. "3eb9637a20").
  string node_id = 2;
  // Human-readable name assigned in ZeroTier Central.
  string name = 3;
  // Optional user-defined description for this member.
  string description = 4;
  // Whether this member is authorized to communicate on the network.
  bool authorized = 5;
  // Assigned IP addresses (e.g. ["10.147.20.1"]).
  // Empty if not authorized or no IP assignment.
  repeated string ip_assignments = 6;
  // Unix timestamp (milliseconds) of last online time. 0 if never seen.
  int64 last_online = 7;
  // Whether the member is currently online (last 5 minutes).
  bool online = 8;
  // External IP:port the member last connected from.
  string physical_address = 9;
  // ZeroTier client version (e.g. "1.16.0").
  string client_version = 10;
}
```

## 10. Anti-pattern checklist

| Anti-pattern | Why it's bad | Fix |
|---|---|---|
| `string status` for a fixed set of values | No schema validation, callers must guess values | Replace with `enum` |
| `string fields` (comma-separated) | No type safety, fragile parsing, no autocomplete | Replace with `repeated Column` |
| `string output = "table"` | Same as above | Replace with `OutputFormat` enum |
| Flat field soup without sub-messages | Hard to extend, no logical grouping | Group into `Filter` / `Display` sub-messages |
| Missing field comments | CLI help shows raw field names, users can't understand flags | Add `//` comments on every field |
| Missing `DocsProto` in plugin.Serve | (Automatic since protoc-gen-docs registers via `plugin.DefaultDocs`) | N/A — handled by generated code |
| `string timestamp` instead of `int64` | No validation, parsing overhead, locale-dependent | Use `int64 unix_seconds` or `google.protobuf.Timestamp` |

## 11. Plugin proto directory structure

```
proto/<plugin_name>/
├── <plugin_name>.proto     # service + message definitions
└── <plugin_name>_docs.go   # go:embed of generated doc.pb (auto-generated)
```

Every plugin proto **must** have a `go_package` option pointing to its own directory:

```protobuf
option go_package = "plugins/zerotier/proto/zerotier";
```

After proto compilation, the generated `*_docs.go` file automatically
registers the documentation via `plugin.DefaultDocs`. No explicit
`DocsProto` field is needed in `plugin.Serve`.

## 12. Service naming

- Service name: `PascalCase` + `Service` suffix, e.g. `ZeroTierService`.
- RPC methods: `PascalCase`, imperative verbs, e.g. `ListMembers`, `GetNetwork`.
- Package name: lowercase, matching the plugin name, e.g. `package zerotier`.
