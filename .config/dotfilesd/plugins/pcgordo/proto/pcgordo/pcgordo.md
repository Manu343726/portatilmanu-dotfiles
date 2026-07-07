# pcgordo

## Table of Contents

- [Services](#services)
  - [pcgordo.PcgordoService](#pcgordopcgordoservice)
    - [Poweron](#poweron)
    - [Shutdown](#shutdown)
    - [Restart](#restart)
    - [Hibernate](#hibernate)
    - [MonitorSleep](#monitorsleep)
    - [MonitorWake](#monitorwake)
    - [SatelliteHibernate](#satellitehibernate)
    - [SatelliteRestart](#satelliterestart)
    - [SatelliteShutdown](#satelliteshutdown)
    - [Status](#status)
    - [Screenshot](#screenshot)
    - [WindowsUpdates](#windowsupdates)
- [Messages](#messages)
  - [Empty](#empty)
  - [PoweronRequest](#poweronrequest)
  - [PoweronResponse](#poweronresponse)
  - [ActionResult](#actionresult)
  - [EntityState](#entitystate)
  - [StatusResponse](#statusresponse)
  - [ScreenshotResponse](#screenshotresponse)
  - [WindowsUpdatesResponse](#windowsupdatesresponse)
- [Enums](#enums)
  - [DeviceClass](#deviceclass)
  - [PCState](#pcstate)
  - [MonitorPowerState](#monitorpowerstate)

## Services

### pcgordo.PcgordoService

PcgordoService controls and monitors the PCGORDO desktop PC (Salon)
via Home Assistant. Supports power management (poweron, shutdown,
restart, hibernate), monitor control, satellite management, status
queries, screen captures, and Windows Update monitoring.
Requires a Home Assistant API token and URL configured in the daemon's
secrets file.

#### Poweron

Poweron sends a Wake-on-LAN magic packet to wake the PC from off state.

- **Request:** `pcgordo.PoweronRequest`
- **Response:** `pcgordo.PoweronResponse`

#### Shutdown

Shutdown gracefully shuts down the main PC.

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### Restart

Restart reboots the main PC.

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### Hibernate

Hibernate puts the main PC into hibernation (saves state to disk).

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### MonitorSleep

MonitorSleep turns off the PC's display monitors.

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### MonitorWake

MonitorWake turns on the PC's display monitors.

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### SatelliteHibernate

SatelliteHibernate hibernates the salon HTPC satellite.

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### SatelliteRestart

SatelliteRestart reboots the salon HTPC satellite.

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### SatelliteShutdown

SatelliteShutdown shuts down the salon HTPC satellite.

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### Status

Status queries the full system status from Home Assistant.
Returns power state, CPU/GPU metrics, active window, monitor power,
and all available entity states.

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.StatusResponse`

#### Screenshot

Screenshot captures the current screen of the PC and returns a URL
to the camera entity image. Returns available=false if the PC is
offline or the camera is unavailable.

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ScreenshotResponse`

#### WindowsUpdates

WindowsUpdates queries available and pending Windows updates.
Returns separate counts for software and driver updates.

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.WindowsUpdatesResponse`


## Messages

### Empty

Generic empty message for RPCs that take no parameters.

### PoweronRequest

PoweronRequest contains the MAC address to wake.

| Field | Type | Description |
|-------|------|-------------|
| `mac` | string | MAC address of the target machine (e.g., "bc:fc:e7:b2:e1:f5"). |

### PoweronResponse

PoweronResponse confirms the power-on packet was sent.

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool | Whether the power-on magic packet was successfully sent. |
| `message` | string | Status message ("poweron packet sent" or error description). |

### ActionResult

ActionResult reports the outcome of a power management action.

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool | Whether the action was successfully triggered. |
| `message` | string | Status message ("ok" or error description). |

### EntityState

EntityState represents a single Home Assistant entity.

| Field | Type | Description |
|-------|------|-------------|
| `entity_id` | string | Entity ID (e.g., "binary_sensor.pcgordo_zerotier_ping"). |
| `friendly_name` | string | Human-readable name from Home Assistant. |
| `state` | string | Current state value. |
| `unit` | string | Unit of measurement if applicable. |
| `device_class` | pcgordo.DeviceClass | Device class if set. |

### StatusResponse

StatusResponse contains the full PC status from Home Assistant.

| Field | Type | Description |
|-------|------|-------------|
| `pc_state` | pcgordo.PCState | Power state of the PC. |
| `last_boot` | int64 | Unix timestamp (seconds since epoch) of last boot. |
| `last_active` | int64 | Unix timestamp (seconds since epoch) of last user activity. |
| `cpu_load` | double | CPU load as percentage (0-100). |
| `gpu_load` | double | GPU load as percentage (0-100). |
| `gpu_temp` | double | GPU temperature in Celsius. |
| `memory_usage` | double | Memory usage as percentage (0-100). |
| `active_window` | string | Title of the currently active window. |
| `active_desktop` | string | Name of the currently active virtual desktop. |
| `monitor_power` | pcgordo.MonitorPowerState | Monitor power state. |
| `zerotier_ping_reachable` | bool | Whether the PC is reachable via ZeroTier network ping. |
| `entities` | repeated pcgordo.EntityState | All entity states available for this PC. |

### ScreenshotResponse

ScreenshotResponse provides a URL to a screen capture image.

| Field | Type | Description |
|-------|------|-------------|
| `available` | bool | Whether a screenshot is currently available. |
| `image_url` | string | URL to the screenshot image from the Home Assistant camera entity. |
| `error` | string | Error message if the screenshot is unavailable. |

### WindowsUpdatesResponse

WindowsUpdatesResponse reports available Windows updates.

| Field | Type | Description |
|-------|------|-------------|
| `available_software_updates` | int32 | Number of available software (non-driver) updates. |
| `pending_software_updates` | int32 | Number of pending software updates awaiting reboot. |
| `available_driver_updates` | int32 | Number of available driver updates. |
| `pending_driver_updates` | int32 | Number of pending driver updates awaiting reboot. |


## Enums

### DeviceClass

Home Assistant device class for entity state values.

| Name | Number | Description |
|------|--------|-------------|
| `DEVICE_CLASS_UNSPECIFIED` | 0 |  |
| `DEVICE_CLASS_TEMPERATURE` | 1 |  |
| `DEVICE_CLASS_POWER` | 2 |  |
| `DEVICE_CLASS_HUMIDITY` | 3 |  |
| `DEVICE_CLASS_PRESSURE` | 4 |  |
| `DEVICE_CLASS_BATTERY` | 5 |  |
| `DEVICE_CLASS_ENERGY` | 6 |  |
| `DEVICE_CLASS_CURRENT` | 7 |  |
| `DEVICE_CLASS_VOLTAGE` | 8 |  |
| `DEVICE_CLASS_SPEED` | 9 |  |
| `DEVICE_CLASS_DURATION` | 10 |  |
| `DEVICE_CLASS_MONETARY` | 11 |  |

### PCState

Power state of the managed PC.

| Name | Number | Description |
|------|--------|-------------|
| `PC_STATE_UNSPECIFIED` | 0 |  |
| `PC_STATE_ONLINE` | 1 | PC is powered on and reachable. |
| `PC_STATE_OFFLINE` | 2 | PC is powered off or unreachable. |

### MonitorPowerState

Power state of the PC's display monitors.

| Name | Number | Description |
|------|--------|-------------|
| `MONITOR_POWER_STATE_UNSPECIFIED` | 0 |  |
| `MONITOR_POWER_STATE_ON` | 1 | Monitors are powered on. |
| `MONITOR_POWER_STATE_OFF` | 2 | Monitors are powered off. |
| `MONITOR_POWER_STATE_UNKNOWN` | 3 | Monitor power state is unknown or the PC is offline. |

