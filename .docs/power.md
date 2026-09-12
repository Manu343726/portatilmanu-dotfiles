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

## Related

- TLP power profiles and CPU tuning live in `/etc/tlp.d/profiles.conf` — see
  `AGENTS.md` rule 11.
- The NVIDIA udev override (`/etc/udev/rules.d/60-nvidia.rules`) is documented
  in `AGENTS.md` rule 10.