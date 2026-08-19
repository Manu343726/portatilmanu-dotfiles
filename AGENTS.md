# Agent instructions

This is a dotfiles repo for the `portatilmanu` machine (ASUS ROG Flow X13 running Manjaro i3).

**Important: The workspace root `/home/manu343726/` is the real `$HOME` of this machine — not a cloned repo in a project directory. Git was init'd directly in `~` to track dotfiles, with `.gitignore` set to `/*` by default so only explicitly whitelisted files are tracked.**

## Rules

0. **Follow protobuf RPC design patterns.** Before designing or modifying any `.proto` file, read `~/dotfilesd/docs/proto-design.md`. Use enums for option sets, `repeated` for multi-value fields (never comma-separated strings), and sub-messages for grouping. Load the `proto-design` skill when working on proto files.

1. **Always commit and push changes to both remotes.** After modifying any tracked file (dotfiles, daemon source, CLI source, docs, etc.), stage and commit with a descriptive message, then push. The `origin` remote has two push URLs — GitHub (`git@github.com:Manu343726/portatilmanu-dotfiles`) and the internal LAN server. A plain `git push` pushes to **both**. Always verify the push succeeds.

2. **Read docs first.** Before doing any work, read `~/dotfilesd/README.md` and `~/dotfilesd/docs/` for context. Follow the development guide in `docs/development.md`.

3. **NEVER run shell commands with `sudo`.** The agent runs in a non-interactive shell; `sudo` triggers a password prompt that the agent cannot answer, and failed attempts will lock the sudo session. Use the `exec_run` MCP tool with `sudo: true` instead — it asks the user for the password through a secure feedback channel, then passes it to `sudo -S` without exposing it to the agent.

4. **Check `.agents/skills/`** for project-specific skills before working. Load any relevant skill with the skills tool.

5. Keep changes idempotent — reloading configs should work cleanly.

6. Follow the Monokai palette (`#272822` bg, `#A6E22E` accent, etc.) when adding visual configs.

7. **Always use the Makefile for building — never `go build`/`go run` directly.** Running `go build .` inside a plugin directory produces a binary that gets accidentally committed and pushed. Instead:
   - **Daemon/CLI:** use `make build` then `make install`.
   - **Plugin:** use `make plugin-build PLUGIN=<name>` (this compiles proto first, then builds to `~/.cache/dotfilesd/plugins/<name>/<name>` — outside the source tree).
   - **Plugin proto:** use `make plugin-proto PLUGIN=<name>`.
   - After modifying a plugin, run `make install` (restarts the daemon) or reload plugins manually.
   - Generated protobuf code (`*.pb.go`, `*connect/*.connect.go`, `*_docs.go`, `*_doc.pb`, `**/proto/**/*.md`), docs artifacts, and ELF binaries are all gitignored. Never `git add` them.

8. **Use the dotfilesd daemon for dotfiles operations.** The daemon runs as a systemd user service and exposes:
   - **MCP** via `dotfilesctl mcp` stdio — for AI agents (tools: `system_ping`, `system_info`, `system_sudo`, `dotfiles_status`, `dotfiles_git`, `exec_run`, `config_reload`).
   - **Connect RPC** at `http://127.0.0.1:9105` — accessible via the `dotfilesctl` CLI client (`~/dotfilesd/cmd/dotfilesctl/main.go`).
   - Use `exec_run` with `sudo: true` for privileged commands — the daemon asks the user for the password securely via the feedback system.
   - After modifying daemon/client code, run `make install` and restart the daemon if needed.
   - See `~/dotfilesd/docs/` for full daemon documentation.

9. **After a sudo session times out, the user will be prompted for their password again the next time `exec_run(sudo=true)` is called.** This is expected behavior — the agent does NOT need to call `sudo -k` or any reset command. The daemon re-requests auth via elicitation (or pkexec/terminal) each time sudo is needed and `sudo -S` fails. If `pkexec` is used instead of elicitation, `pkexec` caches credentials in its own session (15-minute PAM timestamp), so repeated sudo calls within that window won't prompt again. The agent should just silently retry the same `exec_run(sudo=true)` call — the user will be prompted through the appropriate feedback channel (MCP elicitation form, pkexec dialog, or terminal).

10. **Udev rule override at `/etc/udev/rules.d/60-nvidia.rules`.** The system nvidia package installs `60-nvidia.rules` that calls `nvidia-modprobe` on GPU bind, which auto-loads nvidia modules. This conflicts with supergfxctl in Integrated mode, causing a udev-worker to hang in D-state and block shutdown. An empty override file exists at `/etc/udev/rules.d/60-nvidia.rules` to disable this. If reinstalling, recreate it: `sudo tee /etc/udev/rules.d/60-nvidia.rules <<<'# Intentionally empty: see AGENTS.md'`.

11. **TLP power tuning — per-profile.** Power profile switching uses TLP (via `~/.local/bin/power-profile`, bound to `$mod+Ctrl+p`), controlled without root through `tlpctl` (the `tlp-pd` D-Bus client) — not `tlp` (needs root). TLP has 3 profiles, each with its own parameter set (suffix `_AC`/`_BAT`/`_SAV` maps to performance/balanced/power-saver respectively). The per-profile config lives in `/etc/tlp.d/profiles.conf` (system file, not in this repo — recreate on reinstall), which declares the default profile per power source:
   - **Pluggled in (AC)** → `performance` profile (full speed, uncapped: `CPU_BOOST_ON_AC=1`, `CPU_SCALING_MAX_FREQ_ON_AC=0`, `governor=performance`, `PLATFORM_PROFILE_ON_AC=performance`) — set via `TLP_PROFILE_AC=PRF`.
   - **On battery** → `power-saver` profile (aggressive 1 GHz cap: `CPU_BOOST_ON_SAV=0`, `CPU_SCALING_MAX_FREQ_ON_SAV=1000000`, `governor=powersave`, `PCIE_ASPM` powersupersave, `PLATFORM_PROFILE_ON_SAV=quiet`) — set via `TLP_PROFILE_BAT=SAV`. This keeps all-core load power flat (~8 W idle floor dominated by screen/WiFi/SSD).
   - **balanced** profile is deliberately left at TLP's light defaults (`PLATFORM_PROFILE_ON_BAT=balanced`).
   After editing, apply with `sudo tlp start`. **Caveat:** switching profiles can leave a stale `scaling_max_freq` (e.g. stuck at 1000000 kHz) in amd-pstate-epp that isn't cleared by TLP; if the CPU seems capped after a profile switch, release it with `echo 4785000 > /sys/devices/system/cpu/cpu0/cpufreq/scaling_max_freq` (or reboot). This machine is normally in **Integrated** GPU mode (supergfxctl) so the NVIDIA dGPU is fully off; don't re-enable unless needed.

   **asusd config** (`/etc/asusd/asusd.ron`): asusd's platform_profile switching is disabled (`change_platform_profile_on_ac/battery: false`) so TLP fully owns the platform profile. asusd still manages fan curves (`fan_curves.ron`), PPT power limits, keyboard backlight, and charge threshold. The ASUS platform supports only `quiet balanced performance` (not `low-power` which TLP uses as default for `PLATFORM_PROFILE_ON_SAV`, so it's overridden to `quiet`).

   **Daemon plugins**: The `resources` and `tmuxbar` plugins read the TLP power profile via `collectPowerProfile()` (calls `tlpctl get`), exposed as `PowerProfile` enum (`PERF/BAL/SAV`) in the protobuf API. The tmuxbar's `power-profile-widget` displays the current TLP profile in the status bar. The rofi menu at `$mod+Ctrl+p` uses `tlpctl get/set` (script: `~/.local/bin/power-profile`).
