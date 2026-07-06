# zerotier

## Table of Contents

- [Services](#services)
  - [zerotier.ZeroTierService](#zerotierzerotierservice)
    - [ListNetworks](#listnetworks)
    - [ListMembers](#listmembers)
- [Messages](#messages)
  - [ListNetworksRequest](#listnetworksrequest)
  - [ListMembersRequest](#listmembersrequest)
  - [MemberFilter](#memberfilter)
  - [DisplayOptions](#displayoptions)
  - [ListNetworksResponse](#listnetworksresponse)
  - [Network](#network)
  - [Member](#member)
  - [ListMembersResponse](#listmembersresponse)
- [Enums](#enums)
  - [MemberStatus](#memberstatus)
  - [Column](#column)
  - [OutputFormat](#outputformat)

## Services

### zerotier.ZeroTierService

ZeroTierService provides access to ZeroTier Central API for listing
networks and their members with IP assignments.
Requires a ZeroTier Central API token stored in the daemon's secrets file.

#### ListNetworks

ListNetworks returns all networks accessible by the configured API token.
Each network includes its ID, name, description, and member count.
Use this to discover network IDs for subsequent ListMembers calls.

- **Request:** `zerotier.ListNetworksRequest`
- **Response:** `zerotier.ListNetworksResponse`

#### ListMembers

ListMembers returns all members of a specific network with IPs and status.
Supports filtering by connection status and name substring, as well as
column selection and output format control for human-readable display.

- **Request:** `zerotier.ListMembersRequest`
- **Response:** `zerotier.ListMembersResponse`


## Messages

### ListNetworksRequest

### ListMembersRequest

| Field | Type | Description |
|-------|------|-------------|
| `network_id` | string | Network ID to list members for (e.g. "8056c2e21c000001"). If empty and the account has exactly one network, it is auto-detected. If there are multiple networks, --network-id is required. |
| `filter` | zerotier.MemberFilter | Optional filters to narrow the member list before returning. Supports status-based filtering and name substring search. |
| `display` | zerotier.DisplayOptions | Optional display options for human-readable output. Controls which columns to show and in what format (table or raw). When absent, defaults to table format with default columns. |

### MemberFilter

Filters applied to the member list before returning results.
Filters are combined with AND logic (all conditions must match).

| Field | Type | Description |
|-------|------|-------------|
| `status` | zerotier.MemberStatus | Only include members matching this connection/authorization status. Unset (MEMBER_STATUS_UNSPECIFIED) means no status filter. |
| `name_substring` | string | Case-insensitive substring match against the member name. Only members whose name contains this substring are returned. Empty string means no name filter. |

### DisplayOptions

Controls how members are rendered in human-readable display output.

| Field | Type | Description |
|-------|------|-------------|
| `fields` | repeated zerotier.Column | Columns to include in the output. When empty or unspecified, a default set is used: node_id, name, ip, status, authorized. Pass individual enum values: --display.fields=COLUMN_NAME |
| `format` | zerotier.OutputFormat | Output rendering format. Unset means table format. |

### ListNetworksResponse

| Field | Type | Description |
|-------|------|-------------|
| `networks` | repeated zerotier.Network | All networks accessible by the configured API token. |

### Network

A ZeroTier virtual network.

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | ZeroTier network ID (16 hex characters, e.g. "8056c2e21c000001"). |
| `name` | string | Human-readable name configured in ZeroTier Central (e.g. "home"). |
| `description` | string | Optional description of the network's purpose or scope. |
| `member_count` | int32 | Number of members currently authorized on this network. |
| `creation_time` | int64 | Unix timestamp (seconds since epoch) of network creation time. |

### Member

A member (peer node) of a ZeroTier network.

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Composite member ID in the form "<networkId>-<nodeId>". |
| `node_id` | string | ZeroTier node ID (10 hex characters, e.g. "3eb9637a20"). |
| `name` | string | Human-readable name assigned in ZeroTier Central. |
| `description` | string | Optional user-defined description for this member. |
| `authorized` | bool | Whether this member is authorized to communicate on the network. Unauthorized members cannot send or receive traffic. |
| `ip_assignments` | repeated string | Assigned IP addresses (e.g. ["10.147.20.1", "172.25.0.1"]). Empty if the member has no IP assignment or is not authorized. |
| `last_online` | int64 | Unix timestamp (milliseconds) of last online time. Returns 0 if the member has never been seen. |
| `online` | bool | Whether the member is currently online. Heuristic: true if last_online is within the last 5 minutes. |
| `physical_address` | string | External internet IP:port the member last connected from. May be empty if the member has never connected. |
| `client_version` | string | ZeroTier client version string (e.g. "1.16.0"). |
| `network_id` | string | The ZeroTier network ID this member belongs to. |
| `protocol_version` | int32 | ZeroTier protocol version number (e.g. 12). |
| `last_seen` | int64 | Timestamp (milliseconds since epoch) of last check-in with the network controller. More recent than last_online in some cases. |

### ListMembersResponse

| Field | Type | Description |
|-------|------|-------------|
| `members` | repeated zerotier.Member | All members of the requested network, after applying any filters. |


## Enums

### MemberStatus

Filter by member connection and authorization status.
ONLINE/OFFLINE check whether the member was recently seen by the network
controller (last 5 minutes). AUTHORIZED/UNAUTHORIZED check whether the
member is allowed to communicate on the network. These are independent
axes — a member can be online but unauthorized, or authorized but offline.

| Name | Number | Description |
|------|--------|-------------|
| `MEMBER_STATUS_UNSPECIFIED` | 0 | No status filter — return all members regardless of status. |
| `MEMBER_STATUS_ONLINE` | 1 | Only members currently online (seen within the last 5 minutes). |
| `MEMBER_STATUS_OFFLINE` | 2 | Only members currently offline (not seen within the last 5 minutes). |
| `MEMBER_STATUS_AUTHORIZED` | 3 | Only members that are authorized on the network. |
| `MEMBER_STATUS_UNAUTHORIZED` | 4 | Only members that are NOT authorized (pending approval). |

### Column

Columns available for table display output. Use with --display.fields
to control which columns appear in table or raw output. Pass as a
repeated flag: --display.fields=COLUMN_NAME --display.fields=COLUMN_IP

| Name | Number | Description |
|------|--------|-------------|
| `COLUMN_UNSPECIFIED` | 0 | No column specified — uses the server default set. |
| `COLUMN_NODE_ID` | 1 | ZeroTier node ID (10 hex characters, e.g. "3f2de3d43b"). |
| `COLUMN_NAME` | 2 | Human-readable member name assigned in ZeroTier Central. |
| `COLUMN_IP` | 3 | Assigned IP addresses (comma-separated if multiple). |
| `COLUMN_STATUS` | 4 | Connection status: online (green), offline (red), or unauthorized (dim). |
| `COLUMN_DESCRIPTION` | 5 | User-defined description of the member. |
| `COLUMN_VERSION` | 6 | ZeroTier client version string (e.g. "1.16.0"). |
| `COLUMN_PHYSICAL_ADDRESS` | 7 | External IP:port the member last connected from. |
| `COLUMN_AUTHORIZED` | 8 | Whether the member is authorized on the network (true/false). |
| `COLUMN_NETWORK_ID` | 9 | The ZeroTier network ID this member belongs to. |
| `COLUMN_PROTOCOL_VERSION` | 10 | ZeroTier protocol version number (e.g. v12). |

### OutputFormat

Output format for human-readable display output.

| Name | Number | Description |
|------|--------|-------------|
| `OUTPUT_FORMAT_UNSPECIFIED` | 0 | Default table format with aligned columns. |
| `OUTPUT_FORMAT_TABLE` | 1 | Formatted table with column headers and aligned values. |
| `OUTPUT_FORMAT_RAW` | 2 | Key: value lines per member, separated by blank lines. |

