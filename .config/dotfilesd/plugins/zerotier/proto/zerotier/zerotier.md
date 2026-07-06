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

#### ListNetworks

ListNetworks returns all networks accessible by the configured API token.

- **Request:** `zerotier.ListNetworksRequest`
- **Response:** `zerotier.ListNetworksResponse`

#### ListMembers

ListMembers returns all members of a specific network with IPs and status.

- **Request:** `zerotier.ListMembersRequest`
- **Response:** `zerotier.ListMembersResponse`


## Messages

### ListNetworksRequest

### ListMembersRequest

| Field | Type | Description |
|-------|------|-------------|
| `network_id` | string | Network ID to list members for (e.g. "8056c2e21c000001"). If empty and the account has exactly one network, it is auto-detected. |
| `filter` | zerotier.MemberFilter | Optional filters to narrow the member list. |
| `display` | zerotier.DisplayOptions | Optional display options for human-readable output. When absent, defaults to table format with standard columns. |

### MemberFilter

Filters applied to the member list before returning.

| Field | Type | Description |
|-------|------|-------------|
| `status` | zerotier.MemberStatus | Only include members matching this connection status. Unset means no status filter. |
| `name_substring` | string | Case-insensitive substring match against the member name. Empty means no name filter. |

### DisplayOptions

Controls how members are rendered in human-readable output.

| Field | Type | Description |
|-------|------|-------------|
| `fields` | repeated zerotier.Column | Columns to include. Empty or unspecified means the server default: node_id, name, ip, status. |
| `format` | zerotier.OutputFormat | Output rendering format. Unset means table. |

### ListNetworksResponse

| Field | Type | Description |
|-------|------|-------------|
| `networks` | repeated zerotier.Network |  |

### Network

A ZeroTier network.

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | ZeroTier network ID (e.g. "8056c2e21c000001"). |
| `name` | string | Human-readable name configured in ZeroTier Central. |
| `description` | string | Description of the network. |
| `member_count` | int32 | Number of members currently authorized on this network. |
| `creation_time` | int64 | Unix timestamp of network creation time. |

### Member

A member (peer) of a ZeroTier network.

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | ZeroTier member/peer ID (10 hex characters, e.g. "3eb9637a20"). |
| `node_id` | string |  |
| `name` | string | Human-readable name assigned in ZeroTier Central. |
| `description` | string | Optional description for this member. |
| `authorized` | bool | Whether this member is authorized on the network. |
| `ip_assignments` | repeated string | Assigned IP addresses (e.g. ["10.147.20.1"]). Empty if the member has no IP assignment or is not authorized. |
| `last_online` | int64 | Unix timestamp (seconds) of last online time. 0 if never seen. |
| `online` | bool | Whether the member is currently online (heuristic: lastOnline < 5 min ago). |
| `physical_address` | string | Physical internet address (e.g. "1.2.3.4:9993"). |
| `client_version` | string | ZeroTier client version (e.g. "1.16.0"). |
| `network_id` | string | The ZeroTier network ID this member belongs to. |
| `protocol_version` | int32 | ZeroTier protocol version number. |
| `last_seen` | int64 | Timestamp (milliseconds) of last check-in with controller. |

### ListMembersResponse

| Field | Type | Description |
|-------|------|-------------|
| `members` | repeated zerotier.Member |  |


## Enums

### MemberStatus

Connection status filter for network members.

| Name | Number | Description |
|------|--------|-------------|
| `MEMBER_STATUS_UNSPECIFIED` | 0 |  |
| `MEMBER_STATUS_ONLINE` | 1 |  |
| `MEMBER_STATUS_OFFLINE` | 2 |  |
| `MEMBER_STATUS_AUTHORIZED` | 3 |  |
| `MEMBER_STATUS_UNAUTHORIZED` | 4 |  |

### Column

Columns available for table display output.

| Name | Number | Description |
|------|--------|-------------|
| `COLUMN_UNSPECIFIED` | 0 |  |
| `COLUMN_NODE_ID` | 1 |  |
| `COLUMN_NAME` | 2 |  |
| `COLUMN_IP` | 3 |  |
| `COLUMN_STATUS` | 4 |  |
| `COLUMN_DESCRIPTION` | 5 |  |
| `COLUMN_VERSION` | 6 |  |
| `COLUMN_PHYSICAL_ADDRESS` | 7 |  |
| `COLUMN_AUTHORIZED` | 8 |  |
| `COLUMN_NETWORK_ID` | 9 |  |
| `COLUMN_PROTOCOL_VERSION` | 10 |  |

### OutputFormat

Output format for human-readable display output.

| Name | Number | Description |
|------|--------|-------------|
| `OUTPUT_FORMAT_UNSPECIFIED` | 0 |  |
| `OUTPUT_FORMAT_TABLE` | 1 |  |
| `OUTPUT_FORMAT_RAW` | 2 |  |

