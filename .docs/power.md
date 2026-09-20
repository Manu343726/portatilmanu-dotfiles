# Power / Battery

Battery-critical behavior is handled by **UPower** (not logind), lid and
power-key actions are handled by **logind**, and **TLP** owns the power profile.

## Critical battery → hibernate

When the battery hits the critical-action threshold, the machine hibernates
instead of suspending.

**File:** `/etc/UPower/UPower.conf`
```
UsePercentageForPolicy=true
PercentageLow=20.0
PercentageCritical=5.0
PercentageAction=2.0
CriticalPowerAction=Hibernate
```

- The action fires when the battery drops to `PercentageAction` (2%), or
  `TimeAction` remaining if that comes first.
- UPower 1.91 calls `HibernateWithFlags(SD_LOGIND_SKIP_INHIBITORS)`, so no app
  inhibitor can block the hibernate.
- `CanHibernate` must report `yes` (logind), which requires a swap device at
  least the size of RAM plus the `resume` mkinitcpio hook. On this machine:
  swap partition `/dev/nvme0n1p3` (16.4G) > 14G RAM, `resume` hook present.

**Verify:**
```sh
busctl call org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager CanHibernate
# → s "yes"
systemd-inhibit --list   # upowerd should only hold the "Pause device polling" delay lock
```

To (re)apply after a fresh install:
```bash
sudo sed -i 's/^CriticalPowerAction=Auto$/CriticalPowerAction=Hibernate/' /etc/UPower/UPower.conf
sudo systemctl restart upower
```

## Lid close → hibernate

**File:** `/etc/systemd/logind.conf`
```
HandleLidSwitch=hibernate
HandleLidSwitchExternalPower=suspend
```

Lid-close hibernate is logind's own path and is independent of the UPower
critical action above.

## GPU modes (supergfxctl)

GPU switching is handled by **supergfxctl** (supergfxd). File:
`/etc/supergfxd.conf` (system file, not in this repo).

```
{
  "mode": "Integrated",      // current/persisted mode
  "always_reboot": true,     // IMPORTANT, see below
  "logout_timeout_s": 180,
  "hotplug_type": "None"
}
```

- This machine normally runs **Integrated** (dGPU off, best battery). Switch to
  **Hybrid** only when the dGPU is needed (e.g. Beyond All Reason).
- **`always_reboot: true` is required.** Without it, supergfxd waits for a
  logout before completing a mode switch, but its `wait_logout` **hardcodes a
  30s timeout** (`src/actions.rs`, still present upstream as of 5.2.7/master)
  and *ignores* `logout_timeout_s`. If no logout happens in 30s the switch
  errors and **reverts**, and `/etc/supergfxd.conf` is never updated to the new
  mode — so after a reboot you're back in Integrated. With `always_reboot: true`
  the switch runs immediately, persists `"mode"` to the config, and a later
  reboot keeps the chosen mode.
- The `resources`/`tmuxbar` plugins read the mode via `supergfxctl -g` and show
  it in the tmux bar (`gpu-profile-widget`: IGPU/HYBRID/NVIDIA/EGPU).

Switch (dmenu bound script `~/.local/bin/gpu-profile`, or
`dotfilesctl gpu set-profile --mode=HYBRID`), then reboot to complete:

```bash
supergfxctl -m Hybrid        # → "A reboot is required..."
systemctl restart supergfxd  # after editing /etc/supergfxd.conf
supergfxctl -g               # verify current mode
```

## Related

- TLP power profiles and CPU tuning live in `/etc/tlp.d/profiles.conf` — see
  `AGENTS.md` rule 11.
- The NVIDIA udev override (`/etc/udev/rules.d/60-nvidia.rules`) is documented
  in `AGENTS.md` rule 10.