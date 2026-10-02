# PCGORDO — HASS.Agent (Home Assistant integration on the Salon PC)

How the Salon PC publishes to and talks to Home Assistant, and what breaks it.

## The two moving parts

HASS.Agent is split into a **satellite** and a **client**, and they fail
independently. Diagnose them separately — they have separate configs, separate
logs and separate log verbs.

| | satellite | client |
|---|---|---|
| binary | `HASS.Agent.Satellite.Service.exe` | `HASS.Agent.exe` |
| runs as | Windows service `hass.agent.svc` (LocalSystem) | tray app, autostarted from `HKCU\...\Run` |
| install | `C:\Program Files\HASS.Agent Satellite Service\Service\` | `C:\Users\Manu3\AppData\Local\HASS.Agent\Client\` |
| config | `…\Service\config\{servicesettings,servicemqttsettings,commands}.json` | `…\Client\config\appsettings.json`, `commands.json`, `sensors.json` |
| logs | `…\Service\logs\` | `…\Client\logs\` |
| MQTT device id | `PCGORDO-satellite` | `PCGORDO` |
| HA entities | `button.pcgordo_satellite_*` | `sensor.pcgordo_*`, `button.pcgordo_*`, `media_player.pcgordo`, `camera.pcgordo_screenshot` |

The satellite only has 3 entities (the power commands in `commands.json`). Every
sensor comes from the **client**. If the sensors are `unavailable`, the client is
the thing to look at, not the satellite.

## Transport: MQTT plus a REST token

Two separate channels, and both must be right:

- **MQTT** carries all entity state and commands. Both apps publish/consume
  directly on the HA broker (`core-mosquitto` add-on, port 1883) as
  `pcgordomqtt`/`pcgordomqtt`, discovery prefix `homeassistant`.
- **REST** is used by the client only, log verb `[HASS_API]`, for "fetching HA
  config", version reporting and notifications. That is where the long-lived
  access token is used.

The satellite has **no** token — its `servicesettings.json` only carries
`AuthId` and `DeviceName`. A broken satellite is never a token problem; look at
`[MQTT]` and the broker address.

## HA long-lived token

- Client name: **`PC Gordo HASS Agent`**, lifespan 3650 days.
- Stored on pcgordo at `C:\Users\Manu3\.hass-agent\ha-token.txt`, and mirrored
  into `HassToken` in the client's `appsettings.json`.
- **HA refuses duplicate `client_name`s.** Creating a token whose name already
  exists fails with `unknown_error`, so a rotation is always
  delete-then-create, never create-then-create. Rotate with the WebSocket API
  (`auth/refresh_tokens` to list, `auth/delete_refresh_token` to delete,
  `auth/long_lived_access_token` to create — it needs `client_name` *and*
  `lifespan`, and the result is a bare string, not an object).
- The old token was last used 2025-12-06, i.e. only at pairing time. Nothing
  uses it at runtime, so rotating it cannot by itself take the agent down.

## Addresses — the thing that actually breaks

The agent reached HA at **`192.168.100.148`** (old-home LAN). That address died
in the Sep 2026 network move, and the agent was still failing on 2026-10-02,
four days later, in three separate places:

| setting | was | now |
|---|---|---|
| satellite `MqttAddress` | `192.168.100.148` | `172.25.219.62` |
| client `HassUri` | `http://192.168.100.148:8123` | `http://172.25.219.62:8123` |
| client `MqttAddress` | `192.168.100.148` | `172.25.219.62` |

Everything now points at the **ZeroTier** address of the HA VM, not the real
LAN, precisely because the LAN is what moved. `192.168.0.41` also works if the
ZeroTier path is ever the problem — never go back to a `192.168.100.x` address,
that subnet is gone.

Changing the satellite config does **not** take effect live: it is read at
startup only. Either restart the service or push new settings from the client
UI, which triggers `[MQTT] Reloading configuration ..`. The client's own config
is re-read on restart.

## Fixing it from the laptop (ssh pcgordo-wsl)

Editing `C:\Program Files\...` needs elevation, so the patch is a PowerShell
script run with UAC. From WSL:

```sh
scp fix-mqtt-address.ps1 pcgordo-wsl:/mnt/c/Users/Manu3/.hass-agent/
ssh pcgordo-wsl '/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe \
  -NoProfile -Command "Start-Process powershell -Verb RunAs \
  -ArgumentList \"-NoProfile\",\"-ExecutionPolicy\",\"Bypass\",\"-File\",\"C:\Users\Manu3\.hass-agent\fix-mqtt-address.ps1\""'
```

The script patches only the `MqttAddress` value (regex replace, every other byte
untouched), backups the file, restarts `hass.agent.svc` and prints the satellite
log tail. **WSL `sudo` is not enough** — interop processes run with the Windows
user token, which is non-elevated, so the UAC prompt is unavoidable. Have the
script write its output to a file and read that back instead of capturing stdout.

The client config lives under the user's AppData and needs no elevation. Its log
verb for the REST link is `[HASS_API]`; success looks like
`[HASS_API] System connected with http://172.25.219.62:8123`.

## A second, unrelated HA client: the dotfilesd `pcgordo` plugin

Do not confuse this with HASS.Agent. `~/.config/dotfilesd/plugins/pcgordo` runs
on **portatilmanu**, not on pcgordo — it drives the PC *through* HA. Separate
code, separate credentials, separate failure modes. Both were broken in
September 2026 for unrelated reasons.

- It reads `HA_MCP_URL` / `HA_MCP_TOKEN` from its environment, falling back to
  `~/.config/opencode/.env`.
- **It reads them once, at process start.** Rotating the HA token does not reach
  the running plugin: every call returns `{success:false, message:"unauthorized"}`,
  and because protojson omits `success:false` the tool result looks like a bare
  `{"message":"unauthorized"}` — easy to mistake for a *daemon* auth failure. Fix
  with `dotfilesctl config restart` (or the `config_restart` MCP tool). This is
  what really broke on 2026-09-28: `.env` was rotated at 20:26 while the daemon
  had been up since 2026-09-27 21:14.
- A daemon restart **rebuilds and relaunches all 11 plugins at ~10s each**, so
  allow ~2 minutes before the tools reappear. The plugin that rebuilt first wins
  the race if you poll too early.
- `Status.PcState` reports `PC_STATE_OFFLINE` even when the PC is up.
  `parsePCState` only accepts `on`/`online`, but the only entity it actually
  matches is `media_player.pcgordo_2`, whose states are `playing`/`paused`/`idle`.
  `sensor.pcgordo_pc_state`, which would carry a real value, does not exist.
  Judge liveness by `sensor.pcgordo_memoryusage` updating within a minute.
- **The `dotfilesctl mcp` stdio bridge does not survive a daemon restart.** The
  process dies and opencode keeps the stale tool list, so the entire `dotfilesd.*`
  namespace disappears until the MCP server is reconnected. Until then,
  `dotfilesctl pcgordo <cmd>` reaches the same plugin over the same Connect RPC on
  `127.0.0.1:9105`.
- `dotfilesctl` prints `source changed since build` after every commit;
  `--no-verify` silences it.

## Two pre-existing bugs, unrelated to any of the above

- `[VIRTDESKT] Could not load file or assembly 'WinRT.Runtime, Version=2.2.0.0'`
  every few seconds. The client auto-updated itself to 2.2.0 and its packaging is
  missing `WinRT.Runtime.dll`, so the virtual-desktop sensors
  (`sensor.pcgordo_activedesktop`, the desktop switches) are dead. The satellite
  is still on 2.1.1.
- `[WUPDATE] … Windows update seems to be corrupt: 0x8024802A` — the Windows
  Update COM searcher fails, so the `pcgordo_windowsupdates_*` sensors report 0
  rather than a real count.

## Verifying

`[MQTT] Connected` + `[MQTT] Initial registration completed` in both logs, then
in HA: `sensor.pcgordo_memoryusage` should start updating within a minute. The
sensors that only change on event show one timestamp from the registration
snapshot and then go quiet — that is normal, not staleness.