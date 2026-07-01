---
name: pcgordo-wol
description: Remembers how to turn on PCGORDO (the Salon desktop PC) via Wake-on-LAN. The WOL button entity is not exposed to the HA conversation API, so the HA REST API must be called directly.
---

# PCGORDO Wake-on-LAN

PCGORDO is a desktop PC in the Salón area. It can be powered on via a WOL button in Home Assistant.

## Entity

- **Entity ID:** `button.wake_on_lan_bc_fc_e7_b2_e1_f5`
- **Friendly name:** PC Gordo WOL
- **Service:** `button.press`

## How to turn on

The button is not exposed to the Home Assistant conversation/assist API, so `HassTurnOn` will not find it. Use the HA REST API directly:

```bash
source /home/manu343726/.config/opencode/.env
curl -s -X POST \
  -H "Authorization: Bearer $HA_MCP_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "button.wake_on_lan_bc_fc_e7_b2_e1_f5"}' \
  "http://172.25.219.62:8123/api/services/button/press"
```

## Notes

- All PCGORDO sensors and buttons (monitor control, hibernate, shutdown, restart, etc.) are only available when the PC is online. They show as `unavailable` when the PC is off.
- The automation `automation.power_off_pcgordo_screens_after_pc_poweron` handles turning off monitors after the PC boots.
- The IP/host in the URL is defined in `~/.config/opencode/.env` as `HA_MCP_URL`.
