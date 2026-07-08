# tmuxbar

## Table of Contents

- [Services](#services)
  - [tmuxbar.TmuxBarService](#tmuxbartmuxbarservice)
    - [RAMWidget](#ramwidget)
    - [CPUWidget](#cpuwidget)
    - [CPUTempWidget](#cputempwidget)
    - [BatteryWidget](#batterywidget)
    - [StatusBar](#statusbar)
    - [AsusProfileWidget](#asusprofilewidget)
    - [GPUProfileWidget](#gpuprofilewidget)
    - [LayoutWidget](#layoutwidget)
    - [WiFiWidget](#wifiwidget)
- [Messages](#messages)
  - [RAMWidgetRequest](#ramwidgetrequest)
  - [RAMWidgetResponse](#ramwidgetresponse)
  - [CPUWidgetRequest](#cpuwidgetrequest)
  - [CPUWidgetResponse](#cpuwidgetresponse)
  - [CPUTempWidgetRequest](#cputempwidgetrequest)
  - [CPUTempWidgetResponse](#cputempwidgetresponse)
  - [BatteryWidgetRequest](#batterywidgetrequest)
  - [BatteryWidgetResponse](#batterywidgetresponse)
  - [StatusBarRequest](#statusbarrequest)
  - [StatusBarResponse](#statusbarresponse)
  - [AsusProfileWidgetRequest](#asusprofilewidgetrequest)
  - [AsusProfileWidgetResponse](#asusprofilewidgetresponse)
  - [GPUProfileWidgetRequest](#gpuprofilewidgetrequest)
  - [GPUProfileWidgetResponse](#gpuprofilewidgetresponse)
  - [LayoutWidgetRequest](#layoutwidgetrequest)
  - [LayoutWidgetResponse](#layoutwidgetresponse)
  - [WiFiWidgetRequest](#wifiwidgetrequest)
  - [WiFiWidgetResponse](#wifiwidgetresponse)
- [Enums](#enums)
  - [TemperatureUnit](#temperatureunit)

## Services

### tmuxbar.TmuxBarService

TmuxBarService provides formatted status bar widgets for tmux via the
resources plugin. Each RPC returns a pre-formatted text string suitable
for display in a tmux status line. The StatusBar RPC combines multiple
widgets into a single compact output.

#### RAMWidget

RAMWidget returns a formatted RAM usage string like "RAM 6.2/15.9 GB 39%".
The percent value is also returned as a structured field for custom
formatting or threshold-based color coding.

- **Request:** `tmuxbar.RAMWidgetRequest`
- **Response:** `tmuxbar.RAMWidgetResponse`

#### CPUWidget

CPUWidget returns a formatted CPU usage string like "CPU 24%".

- **Request:** `tmuxbar.CPUWidgetRequest`
- **Response:** `tmuxbar.CPUWidgetResponse`

#### CPUTempWidget

CPUTempWidget returns a formatted CPU temperature string like "🌡 65°C".
Supports Celsius and Fahrenheit units.

- **Request:** `tmuxbar.CPUTempWidgetRequest`
- **Response:** `tmuxbar.CPUTempWidgetResponse`

#### BatteryWidget

BatteryWidget returns a formatted battery status string like "🔋 85%"
or "⚡charging". Includes structured percent and charging state for
custom formatting.

- **Request:** `tmuxbar.BatteryWidgetRequest`
- **Response:** `tmuxbar.BatteryWidgetResponse`

#### StatusBar

StatusBar returns a combined compact status line that merges CPU, RAM,
and other widgets into a single string like "CPU 24% | RAM 39% | 65°C".
Intended as a one-call solution for tmux status-right configuration.

- **Request:** `tmuxbar.StatusBarRequest`
- **Response:** `tmuxbar.StatusBarResponse`

#### AsusProfileWidget

AsusProfileWidget returns a formatted ASUS performance profile indicator
like "#[fg=#E8871A]PERF#[default] " with Monokai-colored output.

- **Request:** `tmuxbar.AsusProfileWidgetRequest`
- **Response:** `tmuxbar.AsusProfileWidgetResponse`

#### GPUProfileWidget

GPUProfileWidget returns a formatted GPU mode indicator like
"#[fg=#A6E22E]HYBRID#[default] " with Monokai-colored output.

- **Request:** `tmuxbar.GPUProfileWidgetRequest`
- **Response:** `tmuxbar.GPUProfileWidgetResponse`

#### LayoutWidget

LayoutWidget returns the current keyboard layout (e.g., "us" or "es").

- **Request:** `tmuxbar.LayoutWidgetRequest`
- **Response:** `tmuxbar.LayoutWidgetResponse`

#### WiFiWidget

WiFiWidget returns a formatted WiFi signal indicator like
"#[fg=#A6E22E]65% (MyNetwork) ◼◼◼◼◼◼◻◻◻◻#[default]".

- **Request:** `tmuxbar.WiFiWidgetRequest`
- **Response:** `tmuxbar.WiFiWidgetResponse`


## Messages

### RAMWidgetRequest

RAMWidgetRequest configures the RAM widget output.

| Field | Type | Description |
|-------|------|-------------|
| `format` | string | Optional format string (not yet implemented, reserved for future use). |

### RAMWidgetResponse

RAMWidgetResponse contains the RAM widget result.

| Field | Type | Description |
|-------|------|-------------|
| `text` | string | Formatted text string for display (e.g., "RAM 6.2/15.9 GB 39%"). |
| `percent` | double | RAM usage percentage for custom formatting or thresholds. |

### CPUWidgetRequest

CPUWidgetRequest is currently empty.

### CPUWidgetResponse

CPUWidgetResponse contains the CPU widget result.

| Field | Type | Description |
|-------|------|-------------|
| `text` | string | Formatted text string for display (e.g., "CPU 24%"). |
| `percent` | double | CPU usage percentage for custom formatting or thresholds. |

### CPUTempWidgetRequest

CPUTempWidgetRequest configures the temperature unit.

| Field | Type | Description |
|-------|------|-------------|
| `unit` | tmuxbar.TemperatureUnit | Temperature unit. Unset means Celsius. |

### CPUTempWidgetResponse

CPUTempWidgetResponse contains the temperature widget result.

| Field | Type | Description |
|-------|------|-------------|
| `text` | string | Formatted text string for display (e.g., "🌡 65°C"). |
| `temperature` | double | Temperature value in the requested unit. |

### BatteryWidgetRequest

BatteryWidgetRequest is currently empty.

### BatteryWidgetResponse

BatteryWidgetResponse contains the battery widget result.

| Field | Type | Description |
|-------|------|-------------|
| `text` | string | Formatted text string for display (e.g., "🔋 85%" or "⚡charging"). |
| `percent` | double | Battery charge percentage. |
| `charging` | bool | Whether the battery is currently charging. |

### StatusBarRequest

StatusBarRequest configures the combined status bar output.

| Field | Type | Description |
|-------|------|-------------|
| `max_width` | int32 | Maximum visible width in terminal columns. When set, the server renders widgets in priority order and drops/compacts widgets that don't fit. High-priority widgets (CPU, RAM) are kept, lower-priority ones (WiFi, profiles) are dropped or compacted. 0 means no limit (render all widgets in full form, backward-compatible default). |

### StatusBarResponse

StatusBarResponse contains the combined status line.

| Field | Type | Description |
|-------|------|-------------|
| `text` | string | Combined compact status line (e.g., "CPU 24% | RAM 39% | 65°C"). |

### AsusProfileWidgetRequest

AsusProfileWidgetRequest is empty.

### AsusProfileWidgetResponse

AsusProfileWidgetResponse contains the ASUS performance profile indicator.

| Field | Type | Description |
|-------|------|-------------|
| `text` | string | Formatted text with tmux color sequences (e.g., "#[fg=#A6E22E]BAL#[default] "). |
| `profile` | string | Raw profile name ("PERF", "BAL", "QUIET", or ""). |

### GPUProfileWidgetRequest

GPUProfileWidgetRequest is empty.

### GPUProfileWidgetResponse

GPUProfileWidgetResponse contains the GPU mode indicator.

| Field | Type | Description |
|-------|------|-------------|
| `text` | string | Formatted text with tmux color sequences (e.g., "#[fg=#A6E22E]HYBRID#[default] "). |
| `profile` | string | Raw profile name ("EGPU", "NVIDIA", "IGPU", "HYBRID", or ""). |

### LayoutWidgetRequest

LayoutWidgetRequest is empty.

### LayoutWidgetResponse

LayoutWidgetResponse contains the keyboard layout.

| Field | Type | Description |
|-------|------|-------------|
| `text` | string | Keyboard layout identifier (e.g., "us" or "es"). |
| `layout` | string | Same as text, the layout name. |

### WiFiWidgetRequest

WiFiWidgetRequest is empty.

### WiFiWidgetResponse

WiFiWidgetResponse contains the WiFi signal indicator.

| Field | Type | Description |
|-------|------|-------------|
| `text` | string | Formatted text with tmux color sequences (e.g., "#[fg=#A6E22E]65% (MyNetwork) ◼◼◼◼◼◼◻◻◻◻#[default]"). |
| `percent` | double | Signal quality percentage (0-100). |
| `ssid` | string | WiFi SSID of the connected network. |


## Enums

### TemperatureUnit

Temperature unit for CPU temperature display.

| Name | Number | Description |
|------|--------|-------------|
| `TEMPERATURE_UNIT_UNSPECIFIED` | 0 |  |
| `TEMPERATURE_UNIT_CELSIUS` | 1 | Degrees Celsius. |
| `TEMPERATURE_UNIT_FAHRENHEIT` | 2 | Degrees Fahrenheit. |

