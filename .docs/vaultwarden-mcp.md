# Vaultwarden MCP — agents maintain the homelab secrets

The homelab's secrets live in Vaultwarden's organization **`homelab secrets`**, collection
**`passwords, tokens, etc`**. AI agents read and write it through the local MCP
**`vaultwarden`**, so an operator can say "save this API key as X" and it lands in the vault
instead of a chat log or a stray file.

## What runs

- **Server:** [`@icoretech/warden-mcp`](https://github.com/icoretech/warden-mcp) — a
  Vaultwarden-first MCP that wraps the Bitwarden CLI (`bw`, bundled). Installed at
  `~/.opencode/mcp/warden/`, run over stdio (`warden-mcp --stdio`).
- **Wiring:** `~/.config/opencode/opencode.jsonc.template` (the `vaultwarden` entry) plus
  `generate-configs.sh` (it substitutes the `VW_*` variables). The rendered `opencode.jsonc`
  is gitignored; the credentials live in `~/.config/opencode/.env` as `VW_HOST`, `VW_USER`,
  `VW_PASSWORD` (0600, gitignored).
- **State:** `KEYCHAIN_BW_HOME_ROOT=~/.local/state/warden-mcp` (the `bw` profile cache).

## The account is dedicated, and least-privilege

`homelab-agent@manu343726.com` — a Vaultwarden **user** (not the owner) and a member of the
organization, with the collection granted as **Edit items, hidden passwords**. The owner's
master password is never handed to an agent; the account's own master password is the one in
`.env`.

**Why not a machine client here:** the Bitwarden CLI/MCP path authenticates with
`bw login <email> <password>` (or a personal API key), not with machine-client credentials. A
machine client is for the Pangolin *tunnel*, not for the Vaultwarden *API*.

## It only works while the Pangolin client is up

Vaultwarden is a **private** Pangolin resource (see `pangolin-client.md`, AGENTS rule 16), so
`BW_HOST=https://passwords.manu343726.com` resolves only while `pangolin-client.service` is
connected. If the MCP suddenly cannot reach the server, check the tunnel first.

## Redaction

warden-mcp redacts secret fields (login passwords, TOTP seeds/codes, card numbers, hidden
custom fields, notes, private keys) by default; a caller must pass `reveal: true` to see them.
`NOREVEAL=true` or `READONLY=true` in the MCP environment tightens it further.

## Recreating the account (after a reinstall)

This Vaultwarden has **no SMTP**, and the Pangolin organization is managed only from the web
vault. Vaultwarden makes this easy: **without SMTP, an organization invite is auto-accepted**
(no email is needed). The steps:

1. On the homelab VM, set `SIGNUPS_ALLOWED=true` in
   `~/docker-compose-services/security/vaultwarden/.env` and run
   `docker compose up -d --force-recreate`.
2. Register `homelab-agent@manu343726.com` in the web vault (it sets its own master password;
   that one goes into `.env` as `VW_PASSWORD`).
3. Set `SIGNUPS_ALLOWED=false` and recreate again.
4. As the owner: **Members → Invite member** (role `User`; on the **Collections** tab grant
   `passwords, tokens, etc` as **Edit items, hidden passwords**), then **Confirm** the member
   in the fingerprint dialog.
5. Regenerate the opencode config: `zsh ~/.config/opencode/generate-configs.sh`.

## Verify

Call the MCP tools:

- `vaultwarden_keychain_sync()` → `success: true` and a `lastSync` timestamp.
- `vaultwarden_keychain_list_organizations()` → `homelab secrets`.

A write smoke test is `vaultwarden_keychain_create_note(...)` scoped to the org + collection,
then `vaultwarden_keychain_delete_item(id, permanent: true)`.
