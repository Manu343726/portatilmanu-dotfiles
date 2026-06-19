# Zsh

## Framework

Oh My Zsh with the agnoster theme, customized with Monokai colors.

## Plugins

- `git` — Oh My Zsh git aliases
- `tmux` — auto-starts tmux (`ZSH_TMUX_AUTOSTART=true`)
- `zsh-autosuggestions` — fish-like autosuggestions
- `zsh-syntax-highlighting` — command syntax highlighting

## Aliases

```zsh
alias ls='eza --icons'
alias cat='bat'
alias grep='rg'
alias l='eza -l --icons'
alias ll='eza -la --icons'
alias lt='eza -T --icons'
alias ..='cd ..'
alias ...='cd ../..'
```

## Modern CLI tools

| Tool | Replaces |
|------|----------|
| `eza` | `ls` |
| `bat` | `cat` |
| `ripgrep` (rg) | `grep` |
| `fd` | `find` |
| `fzf` | fuzzy search |
| `zoxide` | `cd` (learns paths) |

## Key features

- Auto-starts tmux on shell login
- Agnoster theme shows git branch, status, venv
- Syntax highlighting as you type
- Fish-style autosuggestions from history
- Colorful file listing with eza
- Zoxide replaces cd with `z <partial path>`
