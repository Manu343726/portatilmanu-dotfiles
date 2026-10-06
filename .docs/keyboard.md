# Keyboard layout

## Layouts

| Layout | Description |
|--------|-------------|
| `us` | US QWERTY (default) |
| `es` | Spanish QWERTY |

Toggle between layouts with **Alt+Shift**.

## System config

Set via `localectl` and persisted in `/etc/X11/xorg.conf.d/00-keyboard.conf`:

```
XkbLayout:  us,es
XkbOptions: grp:alt_shift_toggle
```

## Indicators

- **sbxkb** — tray icon showing current layout (`us` / `es`)
- **Tmux layout_info** — shows current layout in status bar via `xkb-switch`

## i3 integration

The i3 config runs `exec_always setxkbmap -layout us,es -option grp:alt_shift_toggle` so the layout survives i3 reloads.

## Backlight

The keyboard backlight is **not** driven through `/sys/class/leds/asus::kbd_backlight`,
because that LED never exists on this machine.

### Symptom

`hid-asus` fails to register it at every boot:

```
asus 0003:0B05:19B6.0002: Fixing up Asus N-Key report descriptor
asus 0003:0B05:19B6.0002: using HID for asus::kbd_backlight
asus 0003:0B05:19B6.0002: Asus failed to request functions: -75
asus 0003:0B05:19B6.0002: Failed to initialize backlight.
```

and `asusd` reports the same thing from userspace:

```
[ERROR asusd::aura_types] Keyboard backlight error: MissingFunction("KeyboardLed:new(), asus::kbd_backlight not found")
```

### Cause

Two independent things collide.

**1. The probe fails.** `asus_use_hid_led_dmi_ids` in
`drivers/platform/x86/asus-wmi-leds-ids.h` matches `DMI_PRODUCT_FAMILY = "ROG Flow"`, so
`asus_kbd_wmi_led_control_present()` deliberately returns false and forces HID control even
though the WMI path exists. Then a 6.12.y refactor made `asus_kbd_register_leds()` call
`asus_kbd_get_functions()` for **every** keyboard, including ROG N-KEY ones — the 6.12.0 code
had a `QUIRK_ROG_NKEY_KEYBOARD` branch that skipped it. That probe issues a `GET_REPORT` on
feature report `0x5a` asking for `FEATURE_KBD_REPORT_SIZE` (16) bytes and gets `-EOVERFLOW`,
so the driver bails out with "Failed to initialize backlight" before
`devm_led_classdev_register()`.

**2. The early return also skips the LED endpoint init — this is the part that matters.**
The two problems are not independent. Comparing the two versions of `asus_kbd_register_leds()`:

```c
/* 6.12.0 — N-KEY branch, get_functions never called */
if (quirks & QUIRK_ROG_NKEY_KEYBOARD) {
        asus_kbd_init(hdev, FEATURE_KBD_REPORT_ID);        /* 0x5a */
        asus_kbd_init(hdev, FEATURE_KBD_LED_REPORT_ID1);   /* 0x5d */
        asus_kbd_init(hdev, FEATURE_KBD_LED_REPORT_ID2);   /* 0x5e */
} else {
        asus_kbd_init(hdev, FEATURE_KBD_REPORT_ID);
        asus_kbd_get_functions(hdev, &kbd_func, FEATURE_KBD_REPORT_ID);
        ...
}

/* 6.12.y — get_functions moved first, and it returns early on error */
ret = asus_kbd_init(hdev, FEATURE_KBD_REPORT_ID);        /* 0x5a */
ret = asus_kbd_get_functions(hdev, &kbd_func, FEATURE_KBD_REPORT_ID);
if (ret < 0)
        return ret;                                       /* <-- we bail out here */
...
if (quirks & QUIRK_ROG_NKEY_KEYBOARD) {
        asus_kbd_init(hdev, FEATURE_KBD_LED_REPORT_ID1);   /* 0x5d — NEVER RUNS */
        asus_kbd_init(hdev, FEATURE_KBD_LED_REPORT_ID2);   /* 0x5e — NEVER RUNS */
}
```

hid-asus labels `0x5d` and `0x5e` *"The LED endpoint is initialised in two HID"*, and a
brightness report is **silently ignored while they stay dormant**. So the `-EOVERFLOW` does
not merely cost us a sysfs file — it leaves the keyboard's LED endpoints uninitialised for
the whole session. That is why writing `5a ba c5 c4 <level>` on its own does nothing, and it
is the thing the workaround below actually has to work around.

This is fixed upstream by Antheas Kapenekakis' series *"HID: asus: Fix ASUS ROG Laptop's
Keyboard backlight handling"* (v11, Jan 2026), which moves backlight handling under
`asus-wmi`/`asus_hid` and deletes `asus-wmi-leds-ids.h`. It is in `master` (7.x) but not in
the 6.12 LTS this machine runs.

### The keys themselves are fine

The N-KEY HID device still advertises `KEY_KBDILLUMTOGGLE`, `KEY_KBDILLUMUP` and
`KEY_KBDILLUMDOWN`, and X maps them to:

| evdev code | X keycode | keysym |
|------------|-----------|--------|
| 228 | 236 | `XF86KbdLightOnOff` |
| 229 | 237 | `XF86KbdBrightnessDown` |
| 230 | 238 | `XF86KbdBrightnessUp` |

So the events arrive in X; there is simply nothing underneath to act on. Verified by
temporarily binding all three keysyms to a logger — 24 events landed in `~/.kbdhit.log`
from a handful of presses, so no extra `xbindkeys`/`libinput` layer is needed and the keys
were never the problem.

Note these are **Fn+F2** (down) and **Fn+F3** (up) on this model, not the usual F5/F6.

### Workaround

`~/.local/bin/kbd-backlight` replays the init sequence the driver never reached, then writes
the brightness report:

```
5a 41 53 55 53 20 54 65 63 68 2e 49 6e 63 2e 00   handshake, keyboard endpoint
5d 41 53 55 53 20 54 65 63 68 2e 49 6e 63 2e 00   handshake, LED endpoint 1
5e 41 53 55 53 20 54 65 63 68 2e 49 6e 63 2e 00   handshake, LED endpoint 2
5a ba c5 c4 <level>                                  # level 0-3
```

All four are `HIDIOCSFEATURE` on the same hidraw node. The handshake payload is the ASCII
string `"ASUS Tech.Inc."`. The three handshakes are idempotent and cost three USB control
transfers, so they are re-sent on every invocation rather than cached — the keyboard can be
reset independently of the host (unplug, `hid_asus` reload) and re-initialising is cheaper
than tracking that.

`kbd-backlight -v <cmd>` traces every report on stderr.

It resolves the node through the driver symlink
(`/sys/bus/hid/drivers/asus/0003:0B05:19B6.0002/hidraw/hidrawN`), so it always picks the
collection that owns the LED endpoint and not the media-key/keyboard/mouse interfaces.

Commands: `up`, `down`, `toggle`, `on`, `off`, `set N`, `get`, `restore`. `up` from 0 goes
to 1 instead of staying dark, and both ends clamp at 0/3. The level is cached in
`~/.local/state/asus-kbd-backlight` so it survives i3 reloads and reboots.
Bound in `~/.i3/config` to `XF86KbdBrightnessUp` / `XF86KbdBrightnessDown` /
`XF86KbdLightOnOff`.

`restore` exists as a suspend hook but nothing calls it: the kernel's own
`asus_resume()` was already a no-op here, because `drvdata->kbd_backlight` was NULL.

### Required udev rule

`/dev/hidraw1` is `crw------- root root`, so the seat user cannot open it unprivileged.
Install once:

```sh
sudo tee /etc/udev/rules.d/99-asus-kbd-backlight.rules <<'EOF'
# .docs/keyboard.md -- seat access to the ROG Flow X13 keyboard LED endpoint.
# hid-asus never registers asus::kbd_backlight here, so kbd-backlight writes
# feature report 0x5a on the hidraw node directly.
SUBSYSTEM=="hidraw", KERNELS=="0003:0B05:19B6.0002", MODE="0660", TAG+="uaccess"
EOF

sudo udevadm control --reload
sudo udevadm trigger --subsystem-match=hidraw
```

The trigger is only needed to apply the rule to the already-present node; on the next boot
udev creates the node and applies the rule by itself.

### If a newer kernel is adopted

Drop `~/.local/bin/kbd-backlight`, the i3 bindings and the udev rule once the running
kernel exposes the backlight again — check with:

```sh
ls /sys/class/leds/asus::kbd_backlight
```
