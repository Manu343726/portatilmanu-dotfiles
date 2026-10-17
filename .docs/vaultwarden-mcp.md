# Vaultwarden MCP — agents maintain the homelab secrets

The homelab's secrets live in Vaultwarden's organization **`homelab secrets`**. AI agents read
and write it through the MCP **`vaultwarden`**, so an operator can say "save this API key
as X" and it lands in the vault instead of a chat log or a stray file.

## What runs

- **Server:** [`@icoretech/warden-mcp`](https://github.com/icoretech/warden-mcp) — a
  Vaultwarden-first MCP that wraps the Bitwarden CLI (`bw`). Since **2026-10-17** it runs **on
  VM 108**, not here: the stack is
  [`docker-compose-services/security/vaultwarden-mcp/`](https://git.manu343726.com/homelab/Homelab/src/branch/main/docker-compose-services/security/vaultwarden-mcp),
  and it reaches Vaultwarden through a small nginx TLS terminator beside it (`warden-tls`),
  because the Bitwarden CLI refuses any URL that is not https while Vaultwarden speaks plain
  HTTP inside the network.
- **Wiring (portátil):** `~/.config/opencode/opencode.jsonc.template` has a `vaultwarden` entry
  pointing at the estate's gateway, `${LITELLM_MCP_URL}/vaultwarden/mcp`, like every other
  estate MCP. There is **no local warden process** any more: the install at
  `~/.opencode/mcp/warden/` and its `~/.local/state/warden-mcp` state were **deleted** on
  2026-10-17 (`~/.opencode/mcp/` is gone entirely; the same pass removed this machine's local
  Playwright MCP install, its `playwright-mcp-config.json`, and the dead
  `pangolin-mcp.env`/`proxmox-mcp.env` credential files, whose servers the gateway serves too).
- **Credentials:** `~/.config/opencode/.env` still carries `VW_HOST`/`VW_USER`/`VW_PASSWORD`
  (0600, gitignored). Nothing substitutes them any more, and that is deliberate: they are the
  canonical copy of the account's **master password**, which Vaultwarden cannot hold for itself,
  and the source the VM stack's `.env` is filled from. `HA_MCP_*`, which went dead the same way
  when Home Assistant moved behind the gateway, is gone from both the `.env` and
  `generate-configs.sh` — which now lists only the three variables the templates actually use.

## The vault structure

The "hierarchy" is encoded in the **collection name as a path**: `Area / Service`. It is not
real nesting — see the limitation below.

```
Infrastructure      / Proxmox · Network & SNMP · Hosts & TLS
Databases           / PostgreSQL · MongoDB · DbGate
Domains             / Dynadot
Identity & Access   / Authentik · OIDC clients · Pangolin · Traefik
AI                  / LiteLLM · OpenWebUI · OpenCode · Proxmox MCP
Observability       / Grafana · Portabase · Dockhand · Homelable · Glance · Exporters
Services            / Forgejo · Coder · Wiki.js · CopyParty · Navidrome
Security            / Vaultwarden
```

**Items:** one per service, type *note*, with each secret as a **hidden custom field** named
after the environment variable it came from (e.g. item `grafana` in
`Observability / Grafana` has hidden fields `GF_SECURITY_ADMIN_PASSWORD`, …). The
`Identity & Access / OIDC clients` collection instead has one item per Authentik OIDC app
(`backup`, `coder`, `grafana`, `vaultwarden`, …) with `CLIENT_ID` / `CLIENT_SECRET`.
`Domains / Dynadot` holds item `dynadot` for the registrar/DDNS credentials (hidden fields
`DYNADOT_API_KEY`, `DYNADOT_API_SECRET`, `DYNADOT_DDNS_PASSWORD`).

Sources: the homelab VM's `.env` files, `access/pangolin/terraform/terraform.tfvars` (the OIDC
app clients), and the two `~/.vaultwarden-admin-password` / `~/.homelable-admin-pw` files. The
shared `monitoring/.env` is split by key prefix (`GF_*`, `EXPORTER_*`, `SNMP_*`, `PVE_*`).

### Why it is not really nested

**Vaultwarden 1.37.4 cannot nest collections.** Its `Collection` model is
`{uuid, org_uuid, name, external_id}` — there is **no parent field** — and it silently ignores
`parentCollectionId` on create (the 2026.7.0 web vault sends it; the server drops it). Bitwarden
nested collections are display-only anyway (no permission inheritance). Hence the flat
`Area / Service` naming.

### Gotcha — collection names must be encrypted

The Bitwarden clients encrypt the collection name client-side. The raw REST API stores whatever
string it is handed, so a collection created with
`POST /api/organizations/<org>/collections` and a plaintext `name` shows as
**`[error: cannot decrypt]`** in the web vault and in the CLI/MCP. **Create or rename
collections through the MCP (`keychain_create_org_collection`) or `bw` — never the raw API.**

## The account

`homelab-agent@manu343726.com` is an **`Admin` of the organization** — not the owner, and not
the Vaultwarden *server* admin (that is the `/admin` panel behind `ADMIN_TOKEN`). Admin gives
implicit access to **every** collection, which is what a custodian of all the estate's secrets
needs, and it is also what lets the account create the collection tree. The owner's master
password is never handed to an agent.

**Why not a machine client:** the Bitwarden CLI/MCP path authenticates with
`bw login <email> <password>` (or a personal API key), not with machine-client credentials. A
machine client is for the Pangolin *tunnel*, not for the Vaultwarden *API*.

## The tunnel is for the UI now, not for the agents

Vaultwarden is a **private** Pangolin resource (see `pangolin-client.md`, AGENTS rule 16), so
`https://passwords.manu343726.com` — the *UI* — resolves only while `pangolin-client.service` is
connected. If the web vault stops answering, check the tunnel first.

The **MCP** path no longer depends on it: `warden-mcp` runs on the VM and reaches Vaultwarden
inside the homelab's Docker network, so the portátil's agents read and write secrets through
`${LITELLM_MCP_URL}/vaultwarden/mcp` over ZeroTier, with the laptop's tunnel up or down. What it
*does* depend on is the gateway being up and the portátil holding a valid `LITELLM_MCP_KEY` —
a stale one answers `401` on every server, which looks like the vault being broken.

## Redaction

warden-mcp redacts secret fields (login passwords, TOTP seeds/codes, card numbers, hidden
custom fields, notes, private keys) by default; a caller must pass `reveal: true` to see them.
`NOREVEAL=true` or `READONLY=true` in the MCP environment tightens it further.

## Recreating the account (after a reinstall)

This Vaultwarden has **no SMTP**, and the Pangolin organization is managed only from the web
vault. Without SMTP, an organization invite is **auto-accepted** (no email needed). The steps:

1. On the homelab VM, set `SIGNUPS_ALLOWED=true` in
   `~/docker-compose-services/security/vaultwarden/.env` and run
   `docker compose up -d --force-recreate`.
2. Register `homelab-agent@manu343726.com` in the web vault (it sets its own master password;
   that one goes into the portátil's `.env` as `VW_PASSWORD`, and from there into the VM stack's
   `security/vaultwarden-mcp/.env` as `BW_PASSWORD`).
3. Set `SIGNUPS_ALLOWED=false` and recreate again.
4. As the owner: **Members → Invite member**, **Confirm** it (fingerprint dialog), then change
   its role to **Admin**.
5. Recreate the collection tree **through the MCP** (the `Area / Service` list above), then
   repopulate the items from the VM `.env` files.
6. Copy the credentials onto the VM (`security/vaultwarden-mcp/.env`), `docker compose up -d`
   that stack, then regenerate the portátil's opencode config:
   `zsh ~/.config/opencode/generate-configs.sh` (it no longer needs `VW_*`, but the same `.env`
   is where they are kept).

## Verify

- `vaultwarden_keychain_sync()` → `success: true`.
- `vaultwarden_keychain_list_org_collections({organizationId})` → the 29 `Area / Service`
  collections.
- `vaultwarden_keychain_search_items({organizationId})` → the items; `keychain_get_item` returns
  fields redacted unless `reveal: true`.
