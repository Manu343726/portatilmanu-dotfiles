# gpu

## Table of Contents

- [Services](#services)
  - [gpu.GpuService](#gpugpuservice)
    - [GetProfile](#getprofile)
    - [SetProfile](#setprofile)
    - [ListProfiles](#listprofiles)
- [Messages](#messages)
  - [GpuProfile](#gpuprofile)
  - [GetProfileRequest](#getprofilerequest)
  - [GetProfileResponse](#getprofileresponse)
  - [SetProfileRequest](#setprofilerequest)
  - [SetProfileResponse](#setprofileresponse)
  - [ListProfilesRequest](#listprofilesrequest)
  - [ListProfilesResponse](#listprofilesresponse)
- [Enums](#enums)
  - [GpuMode](#gpumode)
  - [GpuModeStatus](#gpumodestatus)

## Services

### gpu.GpuService

GpuService manages the GPU mode (supergfxctl) on ASUS ROG laptops.
Wraps supergfxctl to get/set graphics mode (Integrated, Hybrid,
AsusMuxDgpu, AsusEgpu) and detect eGPU availability. Mode changes
require a logout or reboot to fully apply.

#### GetProfile

GetProfile returns the current and pending GPU mode, plus the
human-readable status message from supergfxctl.

- **Request:** `gpu.GetProfileRequest`
- **Response:** `gpu.GetProfileResponse`

#### SetProfile

SetProfile switches the GPU mode. Returns success/failure and a
message indicating whether a logout or reboot is required.

- **Request:** `gpu.SetProfileRequest`
- **Response:** `gpu.SetProfileResponse`

#### ListProfiles

ListProfiles returns all available GPU modes with their current
status (active, pending, or unavailable). Also detects eGPU
availability from the ASUS WMI sysfs interface.

- **Request:** `gpu.ListProfilesRequest`
- **Response:** `gpu.ListProfilesResponse`


## Messages

### GpuProfile

Information about a single GPU mode.

| Field | Type | Description |
|-------|------|-------------|
| `mode` | gpu.GpuMode | The GPU mode identifier. |
| `display_name` | string | Human-readable name (e.g., "AsusMuxDgpu", "Integrated"). |
| `status` | gpu.GpuModeStatus | Current status of this mode. |
| `egpu_connected` | bool | Whether an eGPU is currently detected (only relevant for AsusEgpu). |

### GetProfileRequest

### GetProfileResponse

| Field | Type | Description |
|-------|------|-------------|
| `current` | gpu.GpuMode | The current active or pending GPU mode. |
| `current_display_name` | string | The pending GPU mode that will apply after next logout/reboot. May be the same as current if no change is pending. |
| `status_message` | string | Human-readable status (e.g., "No action required", "A reboot is required to complete the mode change"). |

### SetProfileRequest

| Field | Type | Description |
|-------|------|-------------|
| `mode` | gpu.GpuMode | Target GPU mode to switch to. |

### SetProfileResponse

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool | Whether the mode change was accepted. |
| `message` | string | Human-readable message (e.g., "Graphics mode changed to Integrated" or error description). |

### ListProfilesRequest

### ListProfilesResponse

| Field | Type | Description |
|-------|------|-------------|
| `profiles` | repeated gpu.GpuProfile | All available GPU profiles with their status. |
| `current` | gpu.GpuMode | The currently active GPU mode. |
| `current_display_name` | string | Display name of the current mode. |


## Enums

### GpuMode

GPU mode as defined by supergfxctl.

| Name | Number | Description |
|------|--------|-------------|
| `GPU_MODE_UNSPECIFIED` | 0 | Unknown or unspecified GPU mode. |
| `GPU_MODE_INTEGRATED` | 1 | Integrated only: AMD iGPU, NVIDIA dGPU powered off. |
| `GPU_MODE_HYBRID` | 2 | Hybrid: dynamic switching between iGPU and dGPU. |
| `GPU_MODE_ASUS_MUX_DGPU` | 3 | AsusMuxDgpu: NVIDIA dGPU drives internal display, highest performance. |
| `GPU_MODE_ASUS_EGPU` | 4 | AsusEgpu: external GPU connected via USB4 / XG Mobile. |

### GpuModeStatus

Status of a GPU mode.

| Name | Number | Description |
|------|--------|-------------|
| `GPU_MODE_STATUS_UNSPECIFIED` | 0 | Unknown or unspecified status. |
| `GPU_MODE_STATUS_ACTIVE` | 1 | This mode is currently active. |
| `GPU_MODE_STATUS_PENDING` | 2 | This mode is pending — will apply on next logout/reboot. |
| `GPU_MODE_STATUS_AVAILABLE` | 3 | This mode is available for switching but not currently active. |
| `GPU_MODE_STATUS_UNAVAILABLE` | 4 | This mode is not available on this system. |

