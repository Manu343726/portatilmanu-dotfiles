# Multi-seat Moonlight/Apollo on PCGORDO — a separate Windows session per stream

Goal: **use PCGORDO locally while someone streams from it** (from the portátil, with
Artemis/Moonlight).

Today that is impossible, and not because of a setting: **Sunshine/Apollo always drive the
*console* session** — the desktop physically logged in on the machine. Apollo runs as a service
in SYSTEM and spawns its capture *and* its input injection into session 1. The physical keyboard
and mouse, and the streamed keyboard and mouse, are therefore hitting **the same input queue**.
That is exactly the symptom: the streamed mouse moves the local pointer and vice versa.

The built-in "Virtual Display" does **not** help. A virtual display is one more monitor *of the
same session*; it changes nothing about input. Neither does running a second Apollo instance
(Apollo's own wiki, *How to start multiple instances*, is about several monitors of one session).

The only fix is **a second Windows session**. Physical input stays in the console session;
Moonlight input is `SendInput`'d *inside* the second one. Two projects implement that; this
document covers the free one.

| | [MultiSeat](https://github.com/vibesoftwarecoder/MultiSeat) | [Duo](https://github.com/DuoStream/Duo) |
| --- | --- | --- |
| Licence / cost | MIT, free | Patreon (paid), closed |
| Last release | v0.6.20, 2026-10-13 | v1.6.0, 2026-08-14 |
| Stack | RDP loopback + SudoVDA + Apollo per seat, web dashboard | TermWrap + patched Sunshine, HDR-capable |
| HDR | no | yes (needs Memory Integrity off) |

MultiSeat was chosen because it is free, open, very actively maintained, and **additive**: it
installs its own Apollo (`C:\Program Files\ApolloVibe`) on its own port block, and **never touches
the standalone Apollo** — so the existing console-session streaming keeps working for solo play.

## What a "seat" actually is

1. A **local Windows account** (`MultiSeat01`, …), a standard user in `Remote Desktop Users`.
2. A **new interactive session** created by **RDP to `127.0.0.2`** (loopback) — the only reliable
   way for a Session 0 service to create one.
3. The session is **held Active for the whole seat lifetime** by a hidden `mstsc` running *in the
   console session*. Do not close it and do not disconnect it: Apollo calls `QueryDisplayConfig`
   at startup and on every client connect, and a Disconnected session answers `ERROR_ACCESS_DENIED`.
4. A **SudoVDA virtual display** created for the seat, set as that session's primary display, with
   the RDP adapter shrunk to 640×480 (this is what drops `TermService` CPU from ~70% to <5%).
5. Its **own Apollo instance** (`ApolloVibe`), one port block per seat.

The isolation is a *consequence of the architecture*, not a filter:

- **Physical keyboard/mouse → console session** (session 1).
- **Moonlight keyboard/mouse → injected inside the seat's session.**

The `EnableKeyboardMouseIsolation` setting and the `InputHook` DLL are a **no-op by design** (the
hooks run in Session 0, where `GetForegroundWindow()` returns NULL) — there is no cross-session
bleed to filter. A missing `MultiSeatInputHook.dll` is harmless.

### Ports

Each seat reserves 30 ports from `PortBase` (48100), which sits above a stock Apollo's block
(~47979–48010):

| Seat | Moonlight base port |
| --- | --- |
| 0 | 48100 |
| 1 | 48130 |
| 2 | 48160 |

Dashboard: `http://pcgordo:9550` (`ApiPort`). `MaxSeats` defaults to 4.

## PCGORDO as probed on 2026-10-14 (via `ssh pcgordo-wsl`)

| Item | Value |
| --- | --- |
| OS | Windows 11 **Pro** 25H2, build `26200.9457` (x64) — meets the 26100+ requirement |
| CPU / RAM | Ryzen 9 9900X (24 threads) / 61.7 GB — ample |
| dGPU | **NVIDIA RTX 5080**, driver **591.86** → `nvenc` (NVENC was live: 1 encode session, ~44 fps) |
| iGPU | **AMD Radeon Graphics** (integrated, Ryzen) — see *Gotchas specific to PCGORDO* |
| Addresses | Ethernet `192.168.0.29/24`, ZeroTier `172.25.209.143` |
| Existing streaming | **Apollo 0.4.6** in `C:\Program Files\Apollo`, service `ApolloService` = Running. `C:\Program Files\Sunshine` is an empty leftover (no exe) |
| Virtual display | **SudoMaker Virtual Display Adapter 1.10.9.289 already installed** (shipped by Apollo; SudoVDA is the same driver family) |
| Remote Desktop | `TermService` = Running, but **RDP is disabled**: `fDenyTSConnections=1`, no 3389 listener, firewall rules off, **no RDPWrap** |
| .NET | 6.0.20, 8.0.21 — **no .NET 9** (irrelevant: the release zip is self-contained) |
| PowerShell | Windows PowerShell **5.1 only** — no `pwsh` (MultiSeat's scripts require 7+) |
| Virtualisation | **VBS running + HVCI (Memory Integrity) enabled** |
| Power | **Fast Startup enabled** (`HiberbootEnabled=1`) — must be turned off |
| Local accounts | `Manu3` only |
| Autostart of note | Steam, Battle.net, SimHub + Simucube Tuner (simracing), Docker Desktop, ZeroTier, HASS.Agent, **RustDesk**, Ollama, LM Studio, Folding@home |

Also relevant: the console desktop is *currently* running on the **SudoMaker virtual display at
2560×1440@144** — i.e. the existing Apollo is used in "Virtual Display" mode inside the console
session. That is precisely the configuration that cannot isolate input.

## Everything runs elevated — from the PC, not from WSL

Driver installs and service registration need an **elevated Windows token**. WSL interop runs with
the normal user token, so `ssh pcgordo-wsl` **cannot** install this (an elevated `sudo` inside WSL
does not help — Windows interop still uses the non-elevated token). Run the scripts **on the
machine**, or remotely through RustDesk, which can answer UAC.

From the portátil, WSL is still the right place to *read* things and to copy files:

```sh
ssh pcgordo-wsl                     # 172.25.209.143:2222, user manu343726
scp file.ps1 pcgordo-wsl:/mnt/c/Users/Manu3/
```

To launch something elevated from WSL (a UAC prompt appears on PCGORDO's screen):

```sh
ssh pcgordo-wsl "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile \
  -Command \"Start-Process -Verb RunAs -FilePath pwsh -ArgumentList '-File','C:\\path\\to\\script.ps1'\""
```

## Install

Run everything below **as Administrator** on PCGORDO. Nobody should be streaming while installing
(the service restarts and every seat drops).

### Install kit already staged on PCGORDO (2026-10-14)

Everything that can be prepared without elevation is already on the machine, in
`C:\Users\Manu3\Downloads\multiseat\`:

| File | Notes |
| --- | --- |
| `multiseat-windows-x64.zip` | v0.6.20, SHA-256 `610fbe38…` — **verified against the release digest**; 383 entries, self-contained (`hostfxr.dll` present) |
| `PowerShell-7.6.7-win-x64.msi` | SHA-256 `ec575cef…` — verified; this host has only Windows PowerShell 5.1 |
| `1-prereqs.ps1` | step 1 (elevated): installs PS7 from that MSI and re-launches itself under `pwsh`, turns **Fast Startup off** (keeps Hibernate), extracts the zip, runs `install-prerequisites.ps1` |
| `2-service.ps1` | step 2 (elevated, after the reboot): `install-service.ps1 -FromZip`, writes `appsettings.local.json` (`nvenc`, `PerSession`) if absent, then runs the verifier |
| `verify-multiseat.ps1` | read-only checks, works non-elevated: version, log filters, RDPWrap offsets, **firewall rules vs the ZeroTier `Public` profile**, 3389 exposure, free space |

All three pass a PowerShell syntax check, and `verify-multiseat.ps1` was run against the host
pre-install (it correctly reports "not installed yet", `ZeroTier → Public`, and no 3389 listener).

The install is therefore two elevated commands with a reboot between them — see steps 1–4 below,
which the scripts automate. Free space was taken from 3.8 GB to ~188 GB beforehand
(see `pcgordo-disk.md`).

### 0. Before you start

- **Turn Fast Startup off** (Control Panel → Power Options → *Choose what the power buttons do* →
  *Change settings that are currently unavailable* → uncheck it), or at least always use
  **Restart** instead of **Shut down**. With Fast Startup, *Shut down* logs off every user session
  and hibernates the system session, so every seat is gone on the next power-on. MultiSeat recovers
  (auto-start seats are rebuilt), but the failure mode is confusing.
- Note that **RDP gets enabled** and **NLA is turned off on the `RDP-Tcp` listener**, machine-wide
  and for every client (a seat's logon cannot answer NLA's prompt, and the setting is not
  per-target). mstsc's certificate and unsigned-`.rdp` warnings are also suppressed by *machine
  policy*. See *Security* below — this does not get undone by an uninstall.

### 1. PowerShell 7

```powershell
winget install Microsoft.PowerShell
```

### 2. Download the release

Grab `multiseat-windows-x64.zip` from
<https://github.com/vibesoftwarecoder/MultiSeat/releases/latest> (v0.6.20, 56 MB), verify and
extract, e.g. to `C:\Users\Manu3\Downloads\multiseat\`:

```powershell
Get-FileHash .\multiseat-windows-x64.zip -Algorithm SHA256
# v0.6.20 expects:
# 610fbe3819f022d745773024e13d1e2ee72f5e95750da2ed655398d6792de4fc
```

Use the release zip (`-FromZip`) rather than a source build: it is **self-contained**, so the
missing .NET 9 runtime and Node.js do not matter.

### 3. Prerequisites

```powershell
.\prerequisites\install-prerequisites.ps1     # log: prerequisites\prereq.log
```

It installs/verifies: RDPWrap + `rdpwrap.ini`, ViGEmBus, HidHide, SudoVDA, ApolloVibe, and .NET 9
SDK + Node.js (source builds only). With the default `PerSession` audio mode it **skips VB-CABLE
and VoiceMeeter** — for a single seat, no virtual audio cables are needed at all.

**It verifies RDPWrap offsets instead of assuming.** RDPWrap patches `termsrv.dll` at byte offsets
keyed by DLL version; a Windows update moves them and multi-session RDP dies, taking every seat with
it. On new builds (this is 26200.9457, well past what the community ini usually covers) it
re-downloads `sebaxakerhtc/rdpwrap.ini`, and if the community has not caught up **it generates the
offsets locally** with `llccd/RDPWrapOffsetFinder`. Read-only check:

```powershell
.\scripts\check-rdpwrap-offsets.ps1            # 0 = covered, 1 = not, 2 = cannot tell
```

⚠️ `termsrv.dll` reports two versions and RDPWrap keys on the **raw** one
(`FileVersionRaw`), not `.VersionInfo.FileVersion`. Coverage also needs **both** `[version]` and
`[version-SLInit]`.

Then **reboot** (HidHide and RDPWrap require it) and re-run the script to confirm it is clean.

### 4. The service

```powershell
.\scripts\install-service.ps1 -FromZip .        # also accepts the .zip path itself
```

Registers `MultiSeatService` (SYSTEM, auto-start) and starts it. Nothing is written into the
install folder during the build: the payload is staged and verified first, and the previous
folder is backed up under `C:\ProgramData\MultiSeat\install-backups\`.

Confirm:

```powershell
& 'C:\Program Files\MultiSeat\MultiSeat.Service.exe' --config    # should print 0.6.20
```

### 5. Dashboard, account, seat

1. `http://localhost:9550` — read the API key with
   `Get-Content "C:\ProgramData\MultiSeat\api-key.txt"` and paste it in **Settings**.
2. **Accounts** → create a Windows local account for the seat (e.g. `MultiSeat01`).
3. **Seats** → **+ New Seat**: pick the account, resolution and FPS, and turn **Auto-start ON**
   (seats live in the service's memory; without auto-start a seat is gone after a restart).
4. Wait ~15 s for **Ready**.

### 6. Connect from the portátil

Add the host **manually** as `<pcgordo-ip>:48100` for seat 0 — `192.168.0.29:48100` on the LAN,
`172.25.209.143:48100` over ZeroTier. Automatic discovery does not cross the tunnel; see
*Streaming over ZeroTier* below. Pair as usual.

## Streaming over ZeroTier

The portátil reaches PCGORDO over **ZeroTier** (`172.25.209.143`, node `b6079f73c67c6a35`), not the
LAN. Measured on 2026-10-14: 0 % loss, RTT min/avg/max = 2.2/6.7/11.6 ms, path `dev ztyxa6ggpt`
(the portátil's ZeroTier interface). That is fine for streaming, and the existing Apollo stream over
the same tunnel is the proof.

Useful check — direct vs relayed path (a relayed path is what makes streaming feel bad):

```sh
sudo zerotier-cli peers | grep b6079f73c67c6a35     # "LEAF" = direct, otherwise relayed
```

Four things change because the client is on ZeroTier:

1. **No host discovery.** Moonlight's auto-discovery is LAN UDP broadcast; it does not cross
   ZeroTier. Enter the seat by hand as `172.25.209.143:48100` (and `:48130`, `:48160` for further
   seats). MoonlightVibe's seat auto-discovery reads the MultiSeat dashboard over HTTP, so it would
   need `http://172.25.209.143:9550` reachable — **do not expose that port** (see *Security*); add
   the host manually instead.

2. **Firewall profile mismatch — the one trap that fails silently.** On PCGORDO the **ZeroTier
   adapter is classified `Public`**, while the home **Ethernet is `Private`**. The rules that let
   you stream today are *program-scoped* and `profile=Any` — they name the **standalone Apollo's**
   binary under `C:\Program Files\Apollo\`, so they cover it on every profile, ZeroTier included.
   **MultiSeat's Apollo is a different executable** (`C:\Program Files\ApolloVibe\sunshine.exe`), so
   it needs its own rules. The prereq script opens them, but if they come out scoped to `Private`
   only, seats will answer **on the LAN and be unreachable over ZeroTier** — with nothing in the
   logs to say why.

   Verify after installing (do it before you need it):

   ```powershell
   Get-NetFirewallRule -Direction Inbound -Enabled True |
     Where-Object { $_.DisplayName -match 'ApolloVibe|MultiSeat' } |
     ForEach-Object {
       "$($_.DisplayName) | profile=$($_.Profile) | action=$($_.Action) | $((($_ | Get-NetFirewallApplicationFilter).Program))"
     }
   ```

   `profile` must include **Public** (or the rule must be program-scoped to
   `ApolloVibe\sunshine.exe`, like the existing Apollo rules). If not, add an inbound allow for the
   seat port block (`48100`–`48189`, TCP+UDP) or for that executable, on the Public profile.

3. **MTU.** The portátil's ZeroTier interface is already capped at **1350** by `zt-mtu-fix` (see
   `.docs/ssh-zerotier-mtu.md`) — the same large-packet behaviour that used to hang SSH KEX applies
   to stream handshakes and to RTP video packets. If pairing or the stream stalls while everything
   else looks healthy, check that first: `ip link show | grep zt` must say `mtu 1350`, and the
   `zt-mtu-fix.timer` must be active.

4. **3389 must stay closed on the tunnel.** The installer disables NLA on the RDP listener
   machine-wide, so an RDP port exposed on ZeroTier would be RDP-without-NLA. MultiSeat needs **no**
   inbound RDP rule (its sessions are loopback). Confirm from the portátil:

   ```sh
   nc -z -w 3 172.25.209.143 3389 && echo "OPEN — fix this" || echo "closed (good)"
   ```

## Configuration on this host

`C:\Program Files\MultiSeat\appsettings.local.json` is loaded **last**, so it outranks the shipped
`appsettings.json`, is never part of the repo, and is copied out and back byte-for-byte on every
deploy. This is where host-specific settings go:

```jsonc
{
  "MultiSeat": {
    "Encoder": "nvenc",          // already the default; RTX 5080. NOT needed: amdvce/quicksync
    "AudioMode": "PerSession",   // default: seat audio goes to the client, no cables, no mic
    "MaxSeats": 4,
    "ApolloLogLevel": "info",    // "verbose" ONLY while debugging a failed encoder — see below
    "GrantSeatAdministrator": false
  }
}
```

Notes:

- **Audio**: `PerSession` (default) routes each seat's audio over its own RDP channel — no
  VB-CABLE, no VoiceMeeter, and **no microphone** (mic only exists in `SharedHost`, which wedges
  the host's audio endpoints on every provision and is not worth it here).
- **Resolution** is set by `mstsc` **at session creation**, not by Apollo — a seat streams its RDP
  surface and a virtual display cannot join an RDP session's topology, so Apollo's
  `dd_resolution_option` does nothing inside a seat. To match a size the client asks for *after*
  connecting, `POST /api/seats/{id}/resolution`, or turn on `FollowClientResolution` (off by
  default: it briefly interrupts the stream).
- **Game launchers must not autostart in the seat.** Apollo creates the ViGEm pad only while a
  client is streaming, so a launcher started at login never sees the controller. Use
  `LaunchOnConnect` (empty = off) to start Steam Big Picture ~4 s after connect instead.
- **Encoder failures are silent by default.** If a seat log ends at `Creating encoder [...]`,
  set `ApolloLogLevel` to **`verbose`** (not `debug` — Apollo maps verbose=0/debug=1/info=2 and
  anything ≥1 silences FFmpeg), restart the service, re-provision, then set it back.
  ⛔ Never hand-edit a seat's `sunshine.conf`: it is regenerated on every provision.

## Gotchas specific to PCGORDO

- **Multi-GPU (AMD iGPU + RTX 5080) is untested by the project.** Its stated constraint is
  *"single GPU only"*, and seats capture the **RDP display, whose GPU Windows picks machine-wide**;
  Apollo filters adapters by `adapter_name` and then requires the captured output to be attached to
  that adapter. If a seat fails to capture or encodes on the wrong engine, that is the first
  suspect. Diagnose with `ApolloLogLevel: verbose` and the seat's `apollo.log`, then consider
  forcing the dGPU as the primary adapter in BIOS (or disabling the iGPU) — and re-test.
- **VBS / HVCI (Memory Integrity) is enabled here.** SudoVDA is signed and already works, so this is
  probably fine for MultiSeat; note that Duo requires Memory Integrity **off** for HDR, so HVCI is
  a variable to remember if HDR ever enters the picture.
- **Fast Startup is on** — see step 0.
- **The standalone Apollo 0.4.6 coexists** (own binary, own ports, and MultiSeat only reaps the
  Apollo processes *it* launched). One exception, not fixable: **tearing a seat down makes the
  standalone Apollo rebuild its encoder** (~266–690 ms measured; the person streaming did not
  notice). The cause is the RDP session's display appearing/disappearing — a desktop topology
  change that invalidates DXGI duplication on the host, not the Apollo processes themselves.
- **HidHide + the console Apollo make a seat's pad ambiguous.** Both create indistinguishable ViGEm
  pads, so per-seat gamepad cloaking (`EnableHidHideCloaking`) is **off by default** — on a host
  with a console-side Apollo, set `SeatPadDevicePaths` before enabling it. Check with
  `MultiSeat.Service.exe --hidhide`.
- **Separate Windows account ⇒ separate Steam.** Steam forbids two concurrent sessions of the same
  account, so the seat and the console user cannot both be in Steam at once. MultiSeat creates a
  shared library (`C:\MultiSeatGames\SteamLibrary` + `\ROMs`, `BUILTIN\Users` Modify) so nothing is
  re-downloaded; point the seat's Steam at it. Practical rule: the person at the PC does non-Steam
  work, or the seat uses a second Steam account with Family Sharing.
- **Seats are standard users** (no admin). Some games/installers need more →
  `GrantSeatAdministrator: true` (this does weaken the isolation that the standard account
  provides).
- **RustDesk is also installed** and, like Apollo, captures the console session. It does not conflict
  with MultiSeat, but remember that *both* RustDesk and the console Apollo show the console.
- **Simracing autostart (SimHub, Simucube Tuner) belongs to `Manu3`** and stays in the console
  session. It does not leak into seats.
- **NVENC session cap** is not a concern: one seat = one encode session (~10 % of the encoder for
  1080p60 HEVC on a 3080-class card). VRAM and shader share bind first, and the RTX 5080 has room.
  Live count: `nvidia-smi --query-gpu=encoder.stats.sessionCount --format=csv`.

## Security — read before installing

- The installer **disables NLA on the RDP listener** and suppresses mstsc warnings by machine
  policy. **This outlives an uninstall.**
- PCGORDO is reachable on **ZeroTier (`172.25.209.143`)** as well as the LAN (`192.168.0.29`), and
  the ZeroTier adapter is a **`Public`**-profile network. Keep **3389 closed** on both: MultiSeat
  needs no inbound rule at all (loopback is not filtered), so do not create one, and check that no
  existing rule opens it after the prerequisites script runs. (The existing `Apollo` rules are
  program-scoped with `profile=Any` on purpose — that is what makes streaming work over ZeroTier.)
- **Do not expose the dashboard (`9550`) on ZeroTier.** The API can create Windows accounts and
  launch executables; on a `Public`-profile adapter the key is the only thing standing in the way.
  Configure it on the PC, or forward the port over SSH (`ssh -L 9550:127.0.0.1:9550 pcgordo-wsl`)
  and use it locally.
- Seat accounts are standard users, but **any local administrator can read everything**, including
  seat passwords (admins can become SYSTEM). The ACLs stop non-admin users and copies taken off the
  machine, not a local admin. `ApiKey: "disabled"` is only for a fully trusted network.

## Verify / troubleshoot

```powershell
& 'C:\Program Files\MultiSeat\MultiSeat.Service.exe' --config        # version
& 'C:\Program Files\MultiSeat\MultiSeat.Service.exe' --log-filters   # do Information logs reach the Event Log?
& 'C:\Program Files\MultiSeat\MultiSeat.Service.exe' --hidhide       # pad cloak state
.\scripts\check-rdpwrap-offsets.ps1                                  # is termsrv.dll covered?
.\scripts\show-logs.ps1                                              # service + per-seat Apollo logs
```

- **The service writes no log files** — it logs to the **Windows Event Log** (Application, sources
  `MultiSeat.Service` and `MultiSeatService`). `C:\ProgramData\MultiSeat\logs\` only ever holds
  `audio-helper.log`, and stays empty until a seat has actually run.
- Per-seat Apollo logs: `C:\ProgramData\MultiSeat\apollo\<account>\apollo.log` (check the
  **timestamp** — a stale log from a previous run looks current).
- `Failed to initialize video capture` → the seat's RDP session went Disconnected; the health check
  recovers it in ~5 s.
- `Failed to Open Input Desktop [0x00000005]` → something is holding the Secure Desktop at logon
  (an elevation prompt; Armoury Crate's `ArmourySocketServer` is a known trigger on ASUS hosts).
  MultiSeat waits for the desktop to stabilise; turn on `DiagnoseInputDesktopReadiness` if it
  times out.
- A seat that is `Ready` but has nothing listening is the classic **AMD/AMF** signature — not
  applicable here (NVIDIA), but the fix would be `"Encoder": "nvenc"` (already correct).

## Undo

```powershell
.\scripts\install-service.ps1 -Uninstall
Remove-Item "C:\Program Files\MultiSeat" -Recurse -Force
Remove-Item "C:\ProgramData\MultiSeat"  -Recurse -Force
```

The RDP/NLA/machine-policy changes and RDPWrap stay — undo those by hand if they are not wanted.

## Status

Not installed yet as of 2026-10-14. The plan is validated against PCGORDO's actual hardware,
software and network (probed over `ssh pcgordo-wsl`; see the table above and *Streaming over
ZeroTier*), the release and the PowerShell MSI are **downloaded and hash-verified on the machine**,
and the three launcher scripts are staged and syntax-checked (see *Install kit already staged*).
Disk pressure was removed first (`pcgordo-disk.md`). The real unknown remains the **hybrid
AMD-iGPU + NVIDIA-dGPU** capture path (untested upstream); everything else has a known answer, and
the ZeroTier firewall-profile question has a check to run right after installing.
