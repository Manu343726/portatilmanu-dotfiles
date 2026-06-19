# Agent instructions

This is a dotfiles repo for the `portatilmanu` machine (ASUS ROG Flow X13 running Manjaro i3).

**Important: The workspace root `/home/manu343726/` is the real `$HOME` of this machine — not a cloned repo in a project directory. Git was init'd directly in `~` to track dotfiles, with `.gitignore` set to `/*` by default so only explicitly whitelisted files are tracked.**

## Rules

1. **Always commit changes to dotfiles.** After modifying any tracked dotfile (`.zshrc`, `.i3/config`, `.config/tmux/`, `.config/kitty/`, etc.), stage and commit with a descriptive message, then push.

2. **Check `.agents/skills/`** for project-specific skills before working. Load any relevant skill with the skills tool.

3. Keep changes idempotent — reloading configs should work cleanly.

4. Follow the Monokai palette (`#272822` bg, `#A6E22E` accent, etc.) when adding visual configs.
