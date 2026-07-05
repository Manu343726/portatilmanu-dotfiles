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

## Services

### pcgordo.PcgordoService

PcgordoService controls and monitors the PCGORDO desktop PC (Salon)
via Home Assistant. Supports power management (poweron, shutdown,
restart, hibernate), monitor control, satellite management, status
queries, screen captures, and Windows Update monitoring.

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
| `device_class` | string | Device class if set (e.g., "temperature", "power"). |

### StatusResponse

StatusResponse contains the full PC status from Home Assistant.

| Field | Type | Description |
|-------|------|-------------|
| `pc_state` | string | Power state: "online", "offline", or Home Assistant state. |
| `last_boot` | string | Timestamp of last boot (ISO 8601). |
| `last_active` | string | Timestamp of last user activity (ISO 8601). |
| `cpu_load` | double | CPU load as percentage (0-100). |
| `gpu_load` | double | GPU load as percentage (0-100). |
| `gpu_temp` | double | GPU temperature in Celsius. |
| `memory_usage` | double | Memory usage as percentage (0-100). |
| `active_window` | string | Title of the currently active window. |
| `active_desktop` | string | Name of the currently active virtual desktop. |
| `monitor_power` | string | Monitor power state ("on", "off", or "unknown"). |
| `zerotier_ping` | string | ZeroTier network connectivity status. |
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

