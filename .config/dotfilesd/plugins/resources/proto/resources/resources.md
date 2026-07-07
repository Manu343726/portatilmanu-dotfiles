# resources

## Table of Contents

- [Services](#services)
  - [resources.ResourcesService](#resourcesresourcesservice)
    - [Current](#current)
    - [Top](#top)
    - [PS](#ps)
    - [History](#history)
    - [Watch](#watch)
- [Messages](#messages)
  - [CurrentRequest](#currentrequest)
  - [WatchRequest](#watchrequest)
  - [WatchFilter](#watchfilter)
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
  - [CPUTempSnapshot](#cputempsnapshot)
  - [BatterySnapshot](#batterysnapshot)
  - [WiFiSnapshot](#wifisnapshot)
  - [ProcessInfo](#processinfo)
- [Enums](#enums)
  - [SortOrder](#sortorder)
  - [ResourceType](#resourcetype)
  - [Unit](#unit)
  - [ProcessState](#processstate)
  - [BatteryStatus](#batterystatus)
  - [ASUSProfile](#asusprofile)
  - [GPUProfile](#gpuprofile)

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

#### Watch

Watch returns a stream of CurrentResponse snapshots, one per poll
cycle. Consumers (like tmuxbar) use this to react to fresh data
without polling.

- **Request:** `resources.WatchRequest`
- **Response:** `resources.CurrentResponse`


## Messages

### CurrentRequest

CurrentRequest is empty. Current returns the latest snapshot.

### WatchRequest

WatchRequest specifies which resources the client wants to monitor.
Only fields with filter=true are checked for changes; updates are
streamed when any watched field differs from the previous poll.

| Field | Type | Description |
|-------|------|-------------|
| `filter` | resources.WatchFilter | Filter controlling which resources trigger stream updates. |

### WatchFilter

WatchFilter selects which resources the client is interested in.
Only set fields to true for the resources you want to watch.

| Field | Type | Description |
|-------|------|-------------|
| `ram` | bool | RAM usage percentage changes. |
| `cpu` | bool | CPU usage percentage changes. |
| `disk` | bool | Root disk usage percentage changes. |
| `disk_io` | bool | Disk I/O statistics changes. |
| `cpu_temp` | bool | CPU temperature changes. |
| `battery` | bool | Battery level or status changes. |
| `wifi` | bool | WiFi signal or SSID changes. |
| `asus_profile` | bool | ASUS performance profile changes. |
| `gpu_profile` | bool | GPU mode changes. |
| `keyboard_layout` | bool | Keyboard layout changes. |
| `top_processes` | bool | Top CPU/memory process name changes. |

### CurrentResponse

CurrentResponse contains the latest snapshot of all resources.

| Field | Type | Description |
|-------|------|-------------|
| `ram` | resources.RAMSnapshot | RAM usage snapshot. |
| `cpu` | resources.CPUSnapshot | CPU usage snapshot with per-type breakdown. |
| `disk` | resources.DiskSnapshot | Root disk partition usage. |
| `disk_io` | resources.DiskIOSnapshot | Primary block device I/O statistics. |
| `cpu_temp` | resources.CPUTempSnapshot | CPU temperature snapshot from thermal sensors. |
| `battery` | resources.BatterySnapshot | Battery status snapshot (level, charging, plugged). |
| `wifi` | resources.WiFiSnapshot | WiFi signal snapshot (strength, SSID). |
| `asus_profile` | resources.ASUSProfile | ASUS performance profile. |
| `gpu_profile` | resources.GPUProfile | GPU mode. |
| `keyboard_layout` | string | Current keyboard layout (e.g., "us" or "es"). |
| `top_cpu_process` | string | Name of the process with highest CPU usage. |
| `top_mem_process` | string | Name of the process with highest memory usage. |
| `poll_count` | int64 | Monotonic counter incremented on each poll cycle. Tmuxbar plugin uses this to detect fresh data and trigger refresh-client. |

### TopRequest

Request to list top processes by resource usage.

| Field | Type | Description |
|-------|------|-------------|
| `count` | int32 | Number of processes to return (default: 10). |
| `sort` | resources.SortOrder | Sort order for process listing. Unset means CPU sort. |

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
| `sort` | resources.SortOrder | Sort order for process listing. Unset means CPU sort. |

### PSResponse

PSResponse contains detailed process information.

| Field | Type | Description |
|-------|------|-------------|
| `processes` | repeated resources.ProcessInfo | Process list matching the query criteria. |

### HistoryRequest

Request for historical resource data.

| Field | Type | Description |
|-------|------|-------------|
| `resource` | resources.ResourceType | Resource to query. Unset means RAM. |
| `count` | int32 | Number of data points to return (default: 20, max: 100). |

### HistoryResponse

HistoryResponse contains time-series data for a resource.

| Field | Type | Description |
|-------|------|-------------|
| `values` | repeated double | Data point values in chronological order (oldest first). |
| `resource` | resources.ResourceType | The resource these values correspond to. |
| `unit` | resources.Unit | Unit of measurement for the values. |

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

### CPUTempSnapshot

CPUTempSnapshot captures the CPU temperature from onboard thermal sensors.

| Field | Type | Description |
|-------|------|-------------|
| `temp_celsius` | double | CPU temperature in degrees Celsius. |

### BatterySnapshot

BatterySnapshot captures the battery status at a point in time.

| Field | Type | Description |
|-------|------|-------------|
| `percent` | double | Battery charge level as percentage (0-100). |
| `charging` | bool | Whether the battery is currently being charged. |
| `plugged` | bool | Whether the AC power adapter is plugged in. |
| `status` | resources.BatteryStatus | Raw battery charge/discharge status from sysfs. |
| `energy_now` | int64 | Current energy remaining in microamp-hours (µAh). |
| `energy_full` | int64 | Full charge energy capacity in microamp-hours (µAh). |
| `power_now` | int64 | Current power draw in microwatts (µW). Positive for discharge, negative is not used — interpret via `status`. |

### WiFiSnapshot

WiFiSnapshot captures the WiFi signal strength at a point in time.

| Field | Type | Description |
|-------|------|-------------|
| `interface` | string | Wireless interface name (e.g., "wlp6s0"). |
| `percent` | double | Signal quality as percentage (0-100). Higher is better. |
| `ssid` | string | Connected network SSID. |

### ProcessInfo

ProcessInfo describes a running process.

| Field | Type | Description |
|-------|------|-------------|
| `pid` | int32 | Process ID. |
| `name` | string | Process name (comm). |
| `cpu_percent` | double | CPU usage percentage. |
| `mem_percent` | double | Memory usage percentage. |
| `mem_mb` | double | Memory usage in MB. |
| `state` | resources.ProcessState | Process state. |


## Enums

### SortOrder

Sort order for process listing.

| Name | Number | Description |
|------|--------|-------------|
| `SORT_ORDER_UNSPECIFIED` | 0 |  |
| `SORT_ORDER_CPU` | 1 | Sort by CPU usage (highest first). |
| `SORT_ORDER_MEMORY` | 2 | Sort by memory usage (highest first). |

### ResourceType

Type of system resource for historical data queries.

| Name | Number | Description |
|------|--------|-------------|
| `RESOURCE_TYPE_UNSPECIFIED` | 0 |  |
| `RESOURCE_TYPE_RAM` | 1 | RAM/memory usage data. |
| `RESOURCE_TYPE_CPU` | 2 | CPU utilization data. |
| `RESOURCE_TYPE_DISK` | 3 | Disk usage data. |
| `RESOURCE_TYPE_CPU_TEMP` | 4 | CPU temperature data. |
| `RESOURCE_TYPE_BATTERY` | 5 | Battery level data. |

### Unit

Unit of measurement for metric values.

| Name | Number | Description |
|------|--------|-------------|
| `UNIT_UNSPECIFIED` | 0 |  |
| `UNIT_PERCENT` | 1 | Percentage value (0-100). |
| `UNIT_CELSIUS` | 2 | Degrees Celsius. |

### ProcessState

Linux process state as reported by the kernel.

| Name | Number | Description |
|------|--------|-------------|
| `PROCESS_STATE_UNSPECIFIED` | 0 |  |
| `PROCESS_STATE_RUNNING` | 1 | Currently running or runnable. |
| `PROCESS_STATE_SLEEPING` | 2 | Sleeping in an interruptible wait. |
| `PROCESS_STATE_DISK_SLEEP` | 3 | Uninterruptible disk sleep (D state). |
| `PROCESS_STATE_ZOMBIE` | 4 | Zombie — terminated but not yet reaped by parent. |
| `PROCESS_STATE_STOPPED` | 5 | Stopped (SIGSTOP or TTY input). |
| `PROCESS_STATE_TRACE_STOP` | 6 | Tracing stop (ptrace). |
| `PROCESS_STATE_DEAD` | 7 | Dead (should not be visible). |

### BatteryStatus

Battery charge/discharge status as reported by the power supply subsystem.

| Name | Number | Description |
|------|--------|-------------|
| `BATTERY_STATUS_UNSPECIFIED` | 0 |  |
| `BATTERY_STATUS_CHARGING` | 1 | Battery is currently charging. |
| `BATTERY_STATUS_DISCHARGING` | 2 | Battery is discharging (on battery power). |
| `BATTERY_STATUS_FULL` | 3 | Battery is fully charged. |
| `BATTERY_STATUS_NOT_CHARGING` | 4 | Battery is not charging but also not full (e.g. charge threshold). |

### ASUSProfile

ASUS performance profile as reported by asusctl.

| Name | Number | Description |
|------|--------|-------------|
| `ASUS_PROFILE_UNSPECIFIED` | 0 |  |
| `ASUS_PROFILE_PERF` | 1 | Performance mode (high power). |
| `ASUS_PROFILE_BAL` | 2 | Balanced mode. |
| `ASUS_PROFILE_QUIET` | 3 | Quiet mode (low power, low fan). |

### GPUProfile

GPU mode as detected from the ASUS nv-wmi driver.

| Name | Number | Description |
|------|--------|-------------|
| `GPU_PROFILE_UNSPECIFIED` | 0 |  |
| `GPU_PROFILE_EGPU` | 1 | External GPU connected via Thunderbolt. |
| `GPU_PROFILE_NVIDIA` | 2 | Dedicated NVIDIA GPU active (mux switched). |
| `GPU_PROFILE_IGPU` | 3 | Integrated GPU only (dGPU disabled). |
| `GPU_PROFILE_HYBRID` | 4 | Hybrid mode (iGPU + dGPU, dynamic switching). |

