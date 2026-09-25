#!/usr/bin/env zsh
# Generate MCP config files from templates, substituting env vars.
# Usage: source this script (so .env vars are available in the calling shell)
#   or run it directly to just write the files.

SCRIPT_DIR="${0:A:h}"
ENV_FILE="$SCRIPT_DIR/.env"
VARS='${HA_MCP_URL}${HA_MCP_TOKEN}${GITHUB_PERSONAL_ACCESS_TOKEN}'

[ -f "$ENV_FILE" ] && source "$ENV_FILE"

# Fail loudly instead of silently substituting an empty value into a header.
missing=()
for name in HA_MCP_URL HA_MCP_TOKEN GITHUB_PERSONAL_ACCESS_TOKEN; do
  [ -z "${(P)name}" ] && missing+=("\$$name")
done
if (( ${#missing} )); then
  print -u2 "error: unset in $ENV_FILE -> ${(j:, :)missing}"
  print -u2 "refusing to write configs with empty credentials"
  exit 1
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
