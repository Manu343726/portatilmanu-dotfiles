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

This is the **NVIDIA dGPU** (PCI 01:00.0). The write to `power/control` hung at boot and the process has been stuck since, blocking systemd's shutdown sequence.

## Root Cause

The NVIDIA driver modules (`nvidia`, `nvidia_drm`, `nvidia_modeset`, `nvidia_uvm`) are loaded on this system. The system udev rules at `/usr/lib/udev/rules.d/60-nvidia.rules` trigger `nvidia-modprobe` on GPU bind events, which conflicts with **supergfxctl** in Integrated mode.

A permanent fix is already in place — an empty override file at:

```
/etc/udev/rules.d/60-nvidia.rules
```

This prevents the nvidia package's rule from executing on future boots. However, the **currently stuck udev-worker** is a leftover from this boot (stuck since boot time) and must be resolved to shut down.

## Recovery (run as sudo)

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

Or in one command (if you can open a root shell before the hang gets bad):

```sh
echo 1 > /proc/sys/kernel/sysrq && echo b > /proc/sysrq-trigger
```

### Option C: Power cycle (last resort)

Hold the physical power button for 10+ seconds.

## After Recovery

Once rebooted cleanly, verify:

1. The override is working:
   ```sh
   udevadm test /sys/devices/pci0000:00/0000:00:01.1/0000:01:00.0 2>&1 | grep -i nvidia
   ```
   Should show no `RUN` commands from `60-nvidia.rules`.

2. No D-state udev-workers:
   ```sh
   ps aux | grep "D.*udev"
   ```

3. NVIDIA modules are not loaded (in Integrated mode):
   ```sh
   lsmod | grep nvidia
   ```

4. supergfxctl shows the correct mode:
   ```sh
   supergfxctl -g
   ```

## Prevention

The override at `/etc/udev/rules.d/60-nvidia.rules` should prevent recurrence after a clean boot. If it's ever removed (e.g., nvidia-driver reinstall), recreate it:

```sh
sudo tee /etc/udev/rules.d/60-nvidia.rules <<'EOF'
# Intentionally empty: supergfxctl handles NVIDIA GPU power state transitions in
# Integrated mode. The default system rule (60-nvidia.rules from nvidia package)
# calls nvidia-modprobe on every bind event, which conflicts with supergfxctl
# trying to rmmod nvidia_drm to switch to Integrated mode. This leaves the GPU
# driver in a broken state and causes a udev-worker to hang in D-state,
# blocking shutdown.
#
# If you switch to Hybrid or NVIDIA mode via supergfxctl, device nodes will be
# created by the normal driver initialization triggered by the mode switch.
EOF
```
