#!/usr/bin/env zsh
# Generate MCP config files from templates, substituting env vars.
# Usage: source this script (so .env vars are available in the calling shell)
#   or run it directly to just write the files.

SCRIPT_DIR="${0:A:h}"
ENV_FILE="$SCRIPT_DIR/.env"
# Only what the templates above actually substitute. Home Assistant's token and the
# Vaultwarden account credentials used to be listed here, from when their MCP servers were
# local to this machine; both are served by the estate's gateway now, so the templates name
# `${LITELLM_MCP_URL}/<server>/mcp` and nothing else.
#
# HA_MCP_URL / HA_MCP_TOKEN stay in `.env` even though no template substitutes them: the
# dotfilesd `pcgordo` plugin reads them itself (getHAConfig in its main.go) and exits at
# startup without them, which costs a whole plugin silently. Pruning them on 2026-10-17 is
# exactly how pcgordo stayed dead from the next daemon restart until 2026-10-24.
VARS='${GITHUB_PERSONAL_ACCESS_TOKEN}${LITELLM_MCP_URL}${LITELLM_MCP_KEY}'

[ -f "$ENV_FILE" ] && source "$ENV_FILE"

# Fail loudly instead of silently substituting an empty value into a header.
missing=()
for name in GITHUB_PERSONAL_ACCESS_TOKEN LITELLM_MCP_URL LITELLM_MCP_KEY; do
  [ -z "${(P)name}" ] && missing+=("\$$name")
done
if (( ${#missing} )); then
  print -u2 "error: unset in $ENV_FILE -> ${(j:, :)missing}"
  print -u2 "refusing to write configs with empty credentials"
  exit 1
fi

# Not substituted into any template, but the dotfilesd pcgordo plugin reads them directly
# and refuses to load without them. Warn instead of failing: the configs are still valid.
if [ -z "$HA_MCP_TOKEN" ] || [ -z "$HA_MCP_URL" ]; then
  print -u2 "warning: HA_MCP_URL/HA_MCP_TOKEN unset in $ENV_FILE — the dotfilesd pcgordo plugin will fail to load"
fi

for pair in \
  "$SCRIPT_DIR/opencode.jsonc.template:$SCRIPT_DIR/opencode.jsonc" \
  "$HOME/.vscode/mcp.json.template:$HOME/.vscode/mcp.json"; do
  tpl="${pair%%:*}"
  out="${pair##*:}"
  if [ -f "$tpl" ]; then
    envsubst "$VARS" < "$tpl" > "$out"
  fi
done
