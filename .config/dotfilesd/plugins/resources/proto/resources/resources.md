# resources

## Table of Contents

- [Services](#services)
  - [resources.ResourcesService](#resourcesresourcesservice)
    - [Current](#current)
    - [Top](#top)
    - [PS](#ps)
    - [History](#history)
- [Messages](#messages)
  - [CurrentRequest](#currentrequest)
  - [CurrentResponse](#currentresponse)
  - [TopRequest](#toprequest)
  - [TopResponse](#topresponse)
  - [PSRequest](#psrequest)
  - [PSResponse](#psresponse)
  - [HistoryRequest](#historyrequest)
  - [HistoryResponse](#historyresponse)
  - [RAMSnapshot](#ramsnapshot)
  - [CPUSnapshot](#cpusnapshot)
  - [DiskSnapshot](#disksnapshot)
  - [DiskIOSnapshot](#diskiosnapshot)
  - [ProcessInfo](#processinfo)

## Services

### resources.ResourcesService

ResourcesService provides system resource monitoring data (RAM, CPU, disk,
disk I/O). A background collector samples stats every 3 seconds and stores
them in a ring buffer. All RPCs read from this shared state for instant
responses without blocking on shell commands.

#### Current

Current returns the latest snapshot of all system resources at once.
Includes RAM, CPU, disk usage, and disk I/O metrics. Use this for a
comprehensive overview or when you need all data in one call.

- **Request:** `resources.CurrentRequest`
- **Response:** `resources.CurrentResponse`

#### Top

Top returns the top N processes sorted by CPU or memory usage.
Useful for identifying resource-heavy processes on the system.

- **Request:** `resources.TopRequest`
- **Response:** `resources.TopResponse`

#### PS

PS returns detailed per-process metrics, optionally filtered to a
specific PID. Similar to the standard `ps` command but returns
structured data for programmatic consumption.

- **Request:** `resources.PSRequest`
- **Response:** `resources.PSResponse`

#### History

History returns historical resource usage data points from the ring
buffer. Use this to track resource usage trends over time.

- **Request:** `resources.HistoryRequest`
- **Response:** `resources.HistoryResponse`


## Messages

### CurrentRequest

CurrentRequest is empty. Current returns the latest snapshot.

### CurrentResponse

CurrentResponse contains the latest snapshot of all resources.

| Field | Type | Description |
|-------|------|-------------|
| `ram` | resources.RAMSnapshot | RAM usage snapshot. |
| `cpu` | resources.CPUSnapshot | CPU usage snapshot with per-type breakdown. |
| `disk` | resources.DiskSnapshot | Root disk partition usage. |
| `disk_io` | resources.DiskIOSnapshot | Primary block device I/O statistics. |

### TopRequest

Request to list top processes by resource usage.

| Field | Type | Description |
|-------|------|-------------|
| `count` | int32 | Number of processes to return (default: 10). |
| `sort` | string | Sort order: "cpu" for CPU usage, "mem" for memory usage (default: "cpu"). |

### TopResponse

TopResponse contains the list of top processes.

| Field | Type | Description |
|-------|------|-------------|
| `processes` | repeated resources.ProcessInfo | Top processes sorted by the requested metric. |

### PSRequest

Request to query detailed process information.

| Field | Type | Description |
|-------|------|-------------|
| `pid` | int32 | Filter to a specific process PID. Empty means list all processes. |
| `count` | int32 | Number of processes to return (default: 20). |
| `sort` | string | Sort order: "cpu" or "mem" (default: "cpu"). |

### PSResponse

PSResponse contains detailed process information.

| Field | Type | Description |
|-------|------|-------------|
| `processes` | repeated resources.ProcessInfo | Process list matching the query criteria. |

### HistoryRequest

Request for historical resource data.

| Field | Type | Description |
|-------|------|-------------|
| `resource` | string | Resource to query: "ram", "cpu", or "disk" (default: "ram"). |
| `count` | int32 | Number of data points to return (default: 20, max: 100). |

### HistoryResponse

HistoryResponse contains time-series data for a resource.

| Field | Type | Description |
|-------|------|-------------|
| `values` | repeated double | Data point values in chronological order (oldest first). |
| `resource` | string | The resource these values correspond to ("ram", "cpu", or "disk"). |
| `unit` | string | Unit of measurement for the values ("%" for all resources). |

### RAMSnapshot

RAMSnapshot captures memory usage at a point in time.

| Field | Type | Description |
|-------|------|-------------|
| `total_mb` | double | Total physical RAM in MB. |
| `used_mb` | double | Currently used RAM in MB (excluding buffers/cache). |
| `available_mb` | double | Available RAM in MB (includes buffers/cache reclaimable by apps). |
| `percent` | double | RAM usage as percentage of total. |

### CPUSnapshot

CPUSnapshot captures CPU utilization at a point in time.

| Field | Type | Description |
|-------|------|-------------|
| `total_percent` | double | Total CPU usage across all cores as percentage. |
| `user_percent` | double | CPU time spent in user space as percentage. |
| `system_percent` | double | CPU time spent in kernel space as percentage. |
| `iowait_percent` | double | CPU time waiting for I/O operations as percentage. |
| `num_cores` | int32 | Number of logical CPU cores detected. |

### DiskSnapshot

DiskSnapshot captures disk usage for the root partition.

| Field | Type | Description |
|-------|------|-------------|
| `mount_point` | string | Mount point path (e.g., "/"). |
| `total_gb` | double | Total disk capacity in GB. |
| `used_gb` | double | Used disk space in GB. |
| `avail_gb` | double | Available disk space in GB. |
| `percent` | double | Disk usage as percentage. |

### DiskIOSnapshot

DiskIOSnapshot captures block device I/O metrics.

| Field | Type | Description |
|-------|------|-------------|
| `device` | string | Block device name (e.g., "nvme0n1" or "sda"). |
| `reads_per_sec` | double | Read operations per second. |
| `writes_per_sec` | double | Write operations per second. |
| `read_bytes_per_sec` | double | Bytes read per second. |
| `write_bytes_per_sec` | double | Bytes written per second. |

### ProcessInfo

ProcessInfo describes a running process.

| Field | Type | Description |
|-------|------|-------------|
| `pid` | int32 | Process ID. |
| `name` | string | Process name (comm). |
| `cpu_percent` | double | CPU usage percentage. |
| `mem_percent` | double | Memory usage percentage. |
| `mem_mb` | double | Memory usage in MB. |
| `state` | string | Process state (R, S, D, Z, etc.). |

