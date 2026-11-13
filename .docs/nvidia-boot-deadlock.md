# NVIDIA driver deadlock at boot (`rm_acpi_nvpcf_notify` vs `rm_init_adapter`)

Seen 2026-11-11 18:26 and 2026-11-13 15:49 on `portatilmanu`, both with
**nvidia-dkms 610.57.04 + linux612 6.12.108**. It looked like "Moonlight hangs
and never starts", but the app was a victim, not the cause.

## Symptom

- Moonlight (or anything GPU-related) sits forever with no window/no error.
- `supergfxctl -g` never answers, so `~/.local/bin/moonlight` blocks **before**
  `exec /usr/bin/moonlight` — that is why nothing appears at all.
- Load average climbs (~10) on an idle machine; `nvidia-smi` also hangs.

## Evidence

Only `/dev/nvidiactl` exists, **no `/dev/nvidia0`** → the dGPU never finished
initialising. Kernel hung-task dumps after 122 s:

```
INFO: task modprobe:1675 blocked for more than 122 seconds.
 task:modprobe state:D  ppid:1630            <- supergfxd's modprobe nvidia_drm
 nv_open_device -> nv_start_device -> rm_init_adapter -> os_acquire_rwlock_write

INFO: task kworker/4:0 blocked for more than 122 seconds.
 Workqueue: kacpi_notify acpi_os_execute_deferred
 acpi_ev_notify_dispatch -> rm_acpi_nvpcf_notify -> os_acquire_rwlock_write
```

and Moonlight's `QThread`s plus rustdesk blocked in `nvidia_unlocked_ioctl` →
`rm_ioctl` on the same rwlock. `supergfxd` itself is *alive* (not crashed), just
stuck in `modprobe nvidia_drm`, so restarting the service does not help — the
lock lives in kernel space.

Deadlock shape: the ACPI NVPF power notification handler (GPU power change
during boot, alongside the `pciehp` slot-0 events) takes the write side of the
RM global lock, while `nvidia_drm`'s probe holds it inside `rm_init_adapter`
waiting on the GPU. Neither can progress. 2 of the last 6 boots hit it.

## Fix applied (2026-11-13)

Driver/kernel bump, since this is a driver-branch bug:

```
pacman -Syu            # nvidia-dkms 610.57.04 -> 615.71.09-2
                       # linux612    6.12.108-1  -> 6.12.112-1
```

DKMS built the module for the new kernel (`dkms status` →
`nvidia/615.71.09, 6.12.112-1-MANJARO, x86_64: installed`), initramfs and
`grub.cfg` regenerated. **Do not apply** `/etc/default/grub.pacnew` or
`/etc/mkinitcpio.conf.pacnew` from that update: the vendor defaults would drop
`resume=UUID=...` (hibernate) and `apparmor=1`.

Also hardened `~/.local/bin/moonlight`: the `supergfxctl -g` probe is now
`timeout 3 supergfxctl -g`. A wedged `supergfxd` can no longer stop Moonlight
from starting, and with no answer the wrapper falls back to the iGPU launch.
Note this only covers a mute daemon — if the driver is deadlocked the launch
would still block on `nvidia0`, so the real fix is the driver.

## How to diagnose it again

```
ls -l /dev/nvidia0                      # missing while /dev/nvidiactl exists
ps -eo pid,stat,wchan:24,cmd | awk '$2 ~ /D/'
journalctl -b -k | grep -E "rm_acpi_nvpcf_notify|rm_init_adapter|blocked for more"
supergfxctl -g                          # hangs -> wrap it in timeout
```

## Recovery

There is none in the running session: the `D`-state tasks cannot be killed and
the lock is inside `rm_init_adapter`. Reboot. A normal `reboot` can itself hang
on those tasks, so the way out is SysRq. **`/proc/sys/kernel/sysrq` is `16` here,
which only enables `sync`** — the reboot key needs the mask opened first:

```
echo 1 > /proc/sys/kernel/sysrq   # as root
echo s > /proc/sysrq-trigger      # sync
echo b > /proc/sysrq-trigger      # reboot
```
