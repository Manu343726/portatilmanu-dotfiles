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

## 5. Document every field and value

**Bad:**

```protobuf
message Member {
  string id = 1;
  string name = 2;
  bool authorized = 3;
}
```

**Good:**

```protobuf
// A member of a ZeroTier network.
message Member {
  // ZeroTier node ID (10 hex characters, e.g. "3eb9637a20").
  string id = 1;
  // Human-readable name assigned in ZeroTier Central.
  string name = 2;
  // Whether this member is authorized to join the network.
  // Unauthorized members have no IP assignment and cannot communicate.
  bool authorized = 3;
}
```

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
  int64 creation_time = 1;  // Unix timestamp
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

## 9. Complete example (good)

```protobuf
syntax = "proto3";
package zerotier;
option go_package = "plugins/zerotier/proto/zerotier";

enum MemberStatus {
  MEMBER_STATUS_UNSPECIFIED = 0;
  MEMBER_STATUS_ONLINE = 1;
  MEMBER_STATUS_OFFLINE = 2;
  MEMBER_STATUS_AUTHORIZED = 3;
  MEMBER_STATUS_UNAUTHORIZED = 4;
}

enum Column {
  COLUMN_UNSPECIFIED = 0;
  COLUMN_NODE_ID = 1;
  COLUMN_NAME = 2;
  COLUMN_IP = 3;
  COLUMN_STATUS = 4;
  COLUMN_DESCRIPTION = 5;
  COLUMN_VERSION = 6;
  COLUMN_PHYSICAL_ADDRESS = 7;
}

enum OutputFormat {
  OUTPUT_FORMAT_UNSPECIFIED = 0;
  OUTPUT_FORMAT_TABLE = 1;
  OUTPUT_FORMAT_RAW = 2;
}

message MemberFilter {
  MemberStatus status = 1;
  // Case-insensitive substring match against member name.
  string name_substring = 2;
}

message DisplayOptions {
  // Columns to show. Empty = server default (node_id, name, ip, status).
  repeated Column fields = 1;
  OutputFormat format = 2;
}

message ListMembersRequest {
  // Network ID. If empty and exactly one network exists, auto-detected.
  string network_id = 1;
  MemberFilter filter = 2;
  DisplayOptions display = 3;
}
```

## 10. Anti-pattern checklist

| Anti-pattern | Why it's bad | Fix |
|---|---|---|
| `string status` for a fixed set of values | No schema validation, callers must guess values | Replace with `enum` |
| `string fields` (comma-separated) | No type safety, fragile parsing, no autocomplete | Replace with `repeated Column` |
| `string output = "table"` | Same as above | Replace with `OutputFormat` enum |
| Flat field soup without sub-messages | Hard to extend, no logical grouping | Group into `Filter` / `Display` sub-messages |
| Missing field comments | Consumers don't know semantics | Add `//` comments on every field |
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

## 12. Service naming

- Service name: `PascalCase` + `Service` suffix, e.g. `ZeroTierService`.
- RPC methods: `PascalCase`, imperative verbs, e.g. `ListMembers`, `GetNetwork`.
- Package name: lowercase, matching the plugin name, e.g. `package zerotier`.
