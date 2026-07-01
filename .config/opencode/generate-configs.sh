#!/usr/bin/env zsh
# Generate MCP config files from templates, substituting env vars.
# Usage: source this script (so .env vars are available in the calling shell)
#   or run it directly to just write the files.

SCRIPT_DIR="${0:A:h}"
ENV_FILE="$SCRIPT_DIR/.env"
VARS='${HA_MCP_URL}${HA_MCP_TOKEN}'

[ -f "$ENV_FILE" ] && source "$ENV_FILE"

for pair in \
  "$SCRIPT_DIR/opencode.jsonc.template:$SCRIPT_DIR/opencode.jsonc" \
  "$HOME/.vscode/mcp.json.template:$HOME/.vscode/mcp.json"; do
  tpl="${pair%%:*}"
  out="${pair##*:}"
  if [ -f "$tpl" ]; then
    envsubst "$VARS" < "$tpl" > "$out"
  fi
done
