# pcgordo

## Table of Contents

- [Services](#services)
  - [PcgordoService](#pcgordoservice)
    - [WOL](#wol)
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
  - [WOLRequest](#wolrequest)
  - [WOLResponse](#wolresponse)
  - [ActionResult](#actionresult)
  - [EntityState](#entitystate)
  - [StatusResponse](#statusresponse)
  - [ScreenshotResponse](#screenshotresponse)
  - [WindowsUpdatesResponse](#windowsupdatesresponse)

## Services

### PcgordoService

#### WOL

- **Request:** `pcgordo.WOLRequest`
- **Response:** `pcgordo.WOLResponse`

#### Shutdown

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### Restart

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### Hibernate

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### MonitorSleep

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### MonitorWake

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### SatelliteHibernate

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### SatelliteRestart

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### SatelliteShutdown

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ActionResult`

#### Status

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.StatusResponse`

#### Screenshot

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.ScreenshotResponse`

#### WindowsUpdates

- **Request:** `pcgordo.Empty`
- **Response:** `pcgordo.WindowsUpdatesResponse`


## Messages

### Empty

### WOLRequest

| Field | Type | Description |
|-------|------|-------------|
| `mac` | string |  |

### WOLResponse

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool |  |
| `message` | string |  |

### ActionResult

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool |  |
| `message` | string |  |

### EntityState

| Field | Type | Description |
|-------|------|-------------|
| `entity_id` | string |  |
| `friendly_name` | string |  |
| `state` | string |  |
| `unit` | string |  |
| `device_class` | string |  |

### StatusResponse

| Field | Type | Description |
|-------|------|-------------|
| `pc_state` | string |  |
| `last_boot` | string |  |
| `last_active` | string |  |
| `cpu_load` | double |  |
| `gpu_load` | double |  |
| `gpu_temp` | double |  |
| `memory_usage` | double |  |
| `active_window` | string |  |
| `active_desktop` | string |  |
| `monitor_power` | string |  |
| `zerotier_ping` | string |  |
| `entities` | repeated pcgordo.EntityState |  |

### ScreenshotResponse

| Field | Type | Description |
|-------|------|-------------|
| `available` | bool |  |
| `image_url` | string |  |
| `error` | string |  |

### WindowsUpdatesResponse

| Field | Type | Description |
|-------|------|-------------|
| `available_software_updates` | int32 |  |
| `pending_software_updates` | int32 |  |
| `available_driver_updates` | int32 |  |
| `pending_driver_updates` | int32 |  |

