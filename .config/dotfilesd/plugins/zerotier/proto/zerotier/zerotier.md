# zerotier

## Table of Contents

- [Services](#services)
  - [zerotier.ZeroTierService](#zerotierzerotierservice)
    - [ListNetworks](#listnetworks)
    - [ListMembers](#listmembers)
- [Messages](#messages)
  - [ListNetworksRequest](#listnetworksrequest)
  - [Network](#network)
  - [ListNetworksResponse](#listnetworksresponse)
  - [ListMembersRequest](#listmembersrequest)
  - [Member](#member)
  - [ListMembersResponse](#listmembersresponse)

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

### Network

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | ZeroTier network ID (e.g. "8056c2e21c000001"). |
| `name` | string | Human-readable name configured in ZeroTier Central. |
| `description` | string | Description of the network. |
| `member_count` | int32 | Number of members currently authorized on this network. |
| `creation_time` | int64 | Unix timestamp of network creation time. |

### ListNetworksResponse

| Field | Type | Description |
|-------|------|-------------|
| `networks` | repeated zerotier.Network |  |

### ListMembersRequest

| Field | Type | Description |
|-------|------|-------------|
| `network_id` | string | Network ID to list members for (e.g. "8056c2e21c000001"). |

### Member

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | ZeroTier member/peer ID (same as node_id). |
| `node_id` | string |  |
| `name` | string | Human-readable name assigned in ZeroTier Central. |
| `description` | string |  |
| `authorized` | bool | Whether this member is authorized on the network. |
| `ip_assignments` | repeated string | Assigned IP addresses (e.g. ["10.147.20.1"]). |
| `last_online` | int64 | Unix timestamp of last online time. |
| `online` | bool | Whether the member is currently online (based on lastOnline recency). |
| `physical_address` | string | Physical internet address (e.g. "1.2.3.4:9993"). |
| `client_version` | string | ZeroTier client version (e.g. "1.16.0"). |

### ListMembersResponse

| Field | Type | Description |
|-------|------|-------------|
| `members` | repeated zerotier.Member |  |

