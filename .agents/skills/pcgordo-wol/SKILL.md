---
name: pcgordo
description: Operations for PCGORDO (the Salon desktop PC): power on/off, hibernate, restart, monitor control, status, screenshots, Windows updates.
---

# PCGORDO Operations

PCGORDO is a desktop PC in the Salón area. All operations are available via the dotfilesd MCP tools — use these instead of raw curl/HA API calls whenever possible.

## Available MCP tools

| Operation | Tool | Notes |
|-----------|------|-------|
| Power on (WOL) | `dotfilesd_pcgordo_WOL` (mac=`bc:fc:e7:b2:e1:f5`) | Works when PC is off |
| Status | `dotfilesd_pcgordo_Status` | Shows power state, CPU/GPU, active window, monitor power |
| Shutdown | `dotfilesd_pcgordo_Shutdown` | Graceful shutdown |
| Restart | `dotfilesd_pcgordo_Restart` | Reboot |
| Hibernate | `dotfilesd_pcgordo_Hibernate` | Save state to disk |
| Monitor sleep | `dotfilesd_pcgordo_MonitorSleep` | Turn off displays |
| Monitor wake | `dotfilesd_pcgordo_MonitorWake` | Turn on displays |
| Screenshot | `dotfilesd_pcgordo_Screenshot` | Returns camera image URL |
| Windows updates | `dotfilesd_pcgordo_WindowsUpdates` | Query available updates |
| Satellite hibernate | `dotfilesd_pcgordo_SatelliteHibernate` | Salon HTPC satellite |
| Satellite restart | `dotfilesd_pcgordo_SatelliteRestart` | Salon HTPC satellite |
| Satellite shutdown | `dotfilesd_pcgordo_SatelliteShutdown` | Salon HTPC satellite |

## Legacy — HA REST API (fallback)

If MCP tools are unavailable, use the HA REST API directly:

```bash
source /home/manu343726/.config/opencode/.env
curl -s -X POST \
  -H "Authorization: Bearer $HA_MCP_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "button.wake_on_lan_bc_fc_e7_b2_e1_f5"}' \
  "http://172.25.219.62:8123/api/services/button/press"
```

## Notes

- All PCGORDO sensors and buttons (except WOL) are only available when the PC is online. They show as `unavailable` when the PC is off.
- The automation `automation.power_off_pcgordo_screens_after_pc_poweron` handles turning off monitors after the PC boots.
- The IP/host in the URL is defined in `~/.config/opencode/.env` as `HA_MCP_URL`.
- The WOL button entity (`button.wake_on_lan_bc_fc_e7_b2_e1_f5`) is not exposed to the HA conversation API, so `HassTurnOn` won't find it — but the MCP tool has no such limitation.
