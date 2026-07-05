# Shutdown Hang: D-state udev-worker blocked on NVIDIA GPU

## Symptoms

- Shutdown/poweroff hangs indefinitely
- System doesn't respond to power button
- May need forced hard reset (holding power button)

## Diagnosis

One or more `udev-worker` processes stuck in **D-state** (uninterruptible sleep) since boot, writing to the NVIDIA GPU's PCI power control:

```
root         492  0.0  0.0  42384 10116 ?        D    09:44   0:00 (udev-worker)
```

Stack trace of the stuck worker:
```
[<0>] control_store+0x31/0xa0     ← writing to power/control sysfs
[<0>] kernfs_fop_write_iter+0x170/0x220
[<0>] vfs_write+0x26f/0x570
[<0>] ksys_write+0x7d/0x120
[<0>] do_syscall_64+0x7d/0x1a0
```

The worker has an open write fd to:
`/sys/devices/pci0000:00/0000:00:01.1/0000:01:00.0/power/control`

This is the **NVIDIA dGPU** (PCI 01:00.0, 10DE:25A2). The write to `power/control` hung at boot and the process has been stuck since, blocking systemd's shutdown sequence.

## Root Cause (Updated)

The chain of events at boot:

1. **Kernel auto-loads nvidia module** — The kernel detects the NVIDIA GPU via PCI modalias (`pci:v000010DEd*sv*sd*bc03sc00i00*`) and loads `nvidia`, `nvidia_drm`, `nvidia_modeset`, `nvidia_uvm` automatically.

2. **`90-supergfxd-nvidia-pm.rules` fires** — supergfxctl installs this udev rule (`/usr/lib/udev/rules.d/90-supergfxd-nvidia-pm.rules`) which writes `auto` to `/sys/.../power/control` on NVIDIA `bind` events:
   ```
   ACTION=="bind", SUBSYSTEM=="pci", ATTR{vendor}=="0x10de", ATTR{class}=="0x030000", TEST=="power/control", ATTR{power/control}="auto"
   ```

3. **supergfxctl tries to switch to Integrated** — supergfxctl starts, reads its config (mode=Integrated), and tries to `rmmod nvidia_drm` but **fails** because the module is "in use":
   ```
   [ERROR supergfxctl::controller] Action thread errored:
   Modprobe error: rmmod nvidia_drm failed: "rmmod: ERROR: Module nvidia_drm is in use\n"
   ```

4. **Write hangs** — The udev-worker's write to `power/control` coincides with supergfxctl's failed teardown attempt, leaving the GPU driver in a broken transitional state. The write never completes → process stuck in D-state → shutdown blocked.

### Why the existing `60-nvidia.rules` override isn't enough

The empty override at `/etc/udev/rules.d/60-nvidia.rules` correctly disables the **nvidia package's** udev rule (which calls `nvidia-modprobe`). But the **kernel auto-loads the nvidia module directly** via PCI modalias — no udev rule needed. And `90-supergfxd-nvidia-pm.rules` (from supergfxctl itself) still fires on the bind event and writes to `power/control`.

## Permanent Fix

Prevent the nvidia modules from auto-loading at boot. Create a modprobe blacklist:

**`/etc/modprobe.d/blacklist-nvidia.conf`**:
```
# Prevent auto-loading of nvidia modules at boot.
# supergfxctl manages loading/unloading explicitly when
# switching between Integrated/Hybrid/NVIDIA modes.
blacklist nvidia
blacklist nvidia_drm
blacklist nvidia_modeset
blacklist nvidia_uvm
```

After creating this file, **rebuild initramfs** and reboot:
```sh
sudo mkinitcpio -P
sudo reboot
```

Without this blacklist, the kernel auto-loads the nvidia driver for the dGPU via PCI modalias before supergfxctl can prevent it. The blacklist ensures only supergfxctl controls when the driver is loaded.

### Verify after reboot

```sh
lsmod | grep nvidia        # should show nothing
supergfxctl -g             # should show "Integrated"
ps aux | grep "D.*udev"    # should show nothing
systemctl status supergfxd --no-pager | tail -10
```

## Recovery (when currently stuck, run as sudo)

### Option A: Remove the GPU PCI device (unstick the write)

```sh
echo 1 > /sys/devices/pci0000:00/0000:00:01.1/0000:01:00.0/remove
```

This detaches the NVIDIA GPU from the PCI bus. The stuck write should complete/abort, freeing the D-state process and allowing shutdown to proceed.

### Option B: Hard reboot via SysRq

If option A doesn't help (D-state can be very stubborn):

1. Hold **Alt + SysRq** (Print Screen)
2. While holding, slowly type: **R E I S U B** (with pauses between each letter)
   - `R` — Switch keyboard from raw mode (XLATE)
   - `E` — Send SIGTERM to all processes
   - `I` — Send SIGKILL to all processes
   - `S` — Sync all filesystems
   - `U` — Remount all filesystems read-only
   - `B` — Reboot

Or in one command (if a root shell is available):

```sh
echo 1 > /proc/sys/kernel/sysrq && echo b > /proc/sysrq-trigger
```

### Option C: Power cycle (last resort)

Hold the physical power button for 10+ seconds.

## Also needed in case of nvidia-driver reinstall

The empty override at `/etc/udev/rules.d/60-nvidia.rules` prevents the nvidia package's `nvidia-modprobe` udev rule from running. If it's ever removed (e.g., nvidia-driver reinstall), recreate it:

```sh
sudo tee /etc/udev/rules.d/60-nvidia.rules <<'EOF'
# Intentionally empty: supergfxctl handles NVIDIA GPU power state transitions in
# Integrated mode. The default system rule (60-nvidia.rules from nvidia package)
# calls nvidia-modprobe on every bind event, which conflicts with supergfxctl.
# If you switch to Hybrid or NVIDIA mode via supergfxctl, device nodes will be
# created by the normal driver initialization triggered by the mode switch.
EOF
```

And the modprobe blacklist after a reinstall:

```sh
sudo tee /etc/modprobe.d/blacklist-nvidia.conf <<'EOF'
blacklist nvidia
blacklist nvidia_drm
blacklist nvidia_modeset
blacklist nvidia_uvm
EOF
sudo mkinitcpio -P
```

## Files involved

| File | Purpose |
|------|---------|
| `/etc/udev/rules.d/60-nvidia.rules` | Empty override — disables nvidia package's udev rule |
| `/usr/lib/udev/rules.d/60-nvidia.rules` | Nvidia package rule (overridden) — calls `nvidia-modprobe` |
| `/usr/lib/udev/rules.d/90-supergfxd-nvidia-pm.rules` | supergfxctl's PM rule — sets `power/control` on bind/unbind |
| `/etc/modprobe.d/blacklist-nvidia.conf` | **NEEDS CREATION** — prevents kernel auto-loading nvidia |
| `/usr/lib/modprobe.d/nvidia-sleep.conf` | NVreg settings for suspend |
| `/usr/lib/modprobe.d/nvidia-utils.conf` | Blacklist nouveau, NVreg settings |
