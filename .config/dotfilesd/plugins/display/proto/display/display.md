# display

## Table of Contents

- [Services](#services)
  - [display.DisplayService](#displaydisplayservice)
    - [GetOutputs](#getoutputs)
    - [SetLayout](#setlayout)
    - [AutoExternal](#autoexternal)
    - [AutorandrTrigger](#autorandrtrigger)
- [Messages](#messages)
  - [Output](#output)
  - [GetOutputsRequest](#getoutputsrequest)
  - [GetOutputsResponse](#getoutputsresponse)
  - [SetLayoutRequest](#setlayoutrequest)
  - [SetLayoutResponse](#setlayoutresponse)
  - [AutoExternalRequest](#autoexternalrequest)
  - [AutoExternalResponse](#autoexternalresponse)
  - [AutorandrTriggerRequest](#autorandrtriggerrequest)
  - [AutorandrTriggerResponse](#autorandrtriggerresponse)
- [Enums](#enums)
  - [DisplayLayout](#displaylayout)
  - [OutputStatus](#outputstatus)

## Services

### display.DisplayService

DisplayService manages display outputs via xrandr. Handles internal/external
display detection across GPU modes (Integrated: eDP, Mux/DGPU: DP-0).

#### GetOutputs

GetOutputs returns the current state of all display outputs via xrandr.
Returns per-output connection/primary status and detects which output is
the internal laptop panel and which is the first external monitor.

- **Request:** `display.GetOutputsRequest`
- **Response:** `display.GetOutputsResponse`

#### SetLayout

SetLayout switches to a display layout (laptop-only, external-only,
extended, or mirror). Performs all xrandr operations in a single call
for atomic screen configuration.

- **Request:** `display.SetLayoutRequest`
- **Response:** `display.SetLayoutResponse`

#### AutoExternal

AutoExternal is the boot-time auto-detection handler. Sleeps 2 seconds
for display driver enumeration, then switches to external-only if an
external monitor is connected. Called from i3 config via
exec --no-startup-id.

- **Request:** `display.AutoExternalRequest`
- **Response:** `display.AutoExternalResponse`

#### AutorandrTrigger

AutorandrTrigger is the udev hotplug handler called on DRM change events.
Detects display state after a 1-second settle and switches to
external-only or laptop-only accordingly.

- **Request:** `display.AutorandrTriggerRequest`
- **Response:** `display.AutorandrTriggerResponse`


## Messages

### Output

Information about a single display output as reported by xrandr.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | xrandr output name (e.g., "eDP", "DP-0", "DisplayPort-1-1"). |
| `status` | display.OutputStatus | Connection and enablement status. |
| `primary` | bool | Whether this output is currently the primary display. |
| `resolution` | string | Current resolution as reported by xrandr (e.g., "1920x1200"). Empty if the output is disabled or disconnected. |
| `internal` | bool | Whether this output is detected as the internal laptop panel. |

### GetOutputsRequest

### GetOutputsResponse

| Field | Type | Description |
|-------|------|-------------|
| `outputs` | repeated display.Output | All display outputs known to xrandr. |
| `internal` | string | Name of the detected internal display (e.g., "eDP" or "DP-0"). Empty if no internal display is detected. |
| `external` | string | Name of the first detected external display (e.g., "DisplayPort-1-1"). Empty if no external display is connected. |
| `active_layout` | display.DisplayLayout | The currently active layout based on output states. |

### SetLayoutRequest

| Field | Type | Description |
|-------|------|-------------|
| `layout` | display.DisplayLayout | The layout to apply. |

### SetLayoutResponse

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool | Whether the layout was applied successfully. |
| `message` | string | Human-readable status message (e.g., "Switched to external only" or error description). |

### AutoExternalRequest

| Field | Type | Description |
|-------|------|-------------|
| `settle_seconds` | int32 | Seconds to wait before probing (default 2). |

### AutoExternalResponse

| Field | Type | Description |
|-------|------|-------------|
| `switched` | bool | Whether an external was detected and the layout was switched. |
| `external` | string | Name of the external display that was enabled, if any. |
| `message` | string | Human-readable status message. |

### AutorandrTriggerRequest

| Field | Type | Description |
|-------|------|-------------|
| `settle_seconds` | int32 | Seconds to wait before probing (default 1). |

### AutorandrTriggerResponse

| Field | Type | Description |
|-------|------|-------------|
| `external_connected` | bool | Whether an external display was detected. |
| `layout_applied` | display.DisplayLayout | The layout that was applied (external-only or laptop-only). |
| `message` | string | Human-readable status message. |


## Enums

### DisplayLayout

DisplayLayout represents a display configuration profile.

| Name | Number | Description |
|------|--------|-------------|
| `DISPLAY_LAYOUT_UNSPECIFIED` | 0 | No layout change — return current state. |
| `DISPLAY_LAYOUT_LAPTOP_ONLY` | 1 | Internal laptop panel only, external displays disabled. |
| `DISPLAY_LAYOUT_EXTERNAL_ONLY` | 2 | External display only, internal laptop panel disabled. |
| `DISPLAY_LAYOUT_EXTENDED` | 3 | Both displays enabled, external placed to the right of internal. |
| `DISPLAY_LAYOUT_MIRROR` | 4 | Both displays enabled with mirrored content (same-as clone). |

### OutputStatus

Status of a single display output.

| Name | Number | Description |
|------|--------|-------------|
| `OUTPUT_STATUS_UNSPECIFIED` | 0 | Unknown or unspecified status. |
| `OUTPUT_STATUS_CONNECTED` | 1 | Output is physically connected and enabled. |
| `OUTPUT_STATUS_CONNECTED_DISABLED` | 2 | Output is physically connected but disabled (e.g., turned off by xrandr). |
| `OUTPUT_STATUS_DISCONNECTED` | 3 | Output is disconnected (no monitor attached). |

