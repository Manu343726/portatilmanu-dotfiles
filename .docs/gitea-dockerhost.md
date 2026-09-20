# Gitea git server + GitHub mirror — DockerHost

The internal git server is **Gitea**, running as a Docker Compose service on
`DockerHost`. Both the dotfiles repo and other internal repos are pushed here
(the `origin` remote's LAN push URL), plus GitHub.

## Service layout

**Directory:** `~/docker-compose-services/dev/gitea/`

**`docker-compose.yml`** defines two services:

| Service | Image | Purpose |
|---------|-------|---------|
| `gitea` | `gitea/gitea:1.23.1-rootless` | the git server itself |
| `mirror-to-gitea` | `jaedle/mirror-to-gitea` | one-way mirror of GitHub repos → Gitea |

State lives in `./data` (Gitea) and `./config`. Secrets live in
`.secrets.env` (mode 600), which is gitignored.

## Ports

- `3001` — web UI / HTTP API (`http://DockerHost:3001`)
- `2222` — SSH (used as the git remote URL for LAN pushes)

The Gitea API is reached by `mirror-to-gitea` over the compose network
(`http://gitea:3000`), so it does not depend on the published ports.

## Managing

```sh
cd ~/docker-compose-services/dev/gitea

docker compose up -d          # start gitea (+ mirror)
docker compose up -d gitea    # start only the git server
docker compose up -d mirror-to-gitea   # start only the mirror
docker compose logs -f mirror-to-gitea # mirror logs
docker ps --filter name=gitea
```

Both services use `restart: always`, so they come back on DockerHost reboot.

## When the git server is "down"

Symptoms: LAN `git push` fails with `Connection refused` on port 2222, and
`docker ps` shows no `gitea` container.

Fix (no sudo needed):

```sh
cd ~/docker-compose-services/dev/gitea && docker compose up -d
```

Wait ~10s, then verify:

```sh
docker ps --filter name=gitea                     # gitea Up
ssh -p 2222 git@DockerHost                        # or: git push to LAN remote
```

## `mirror-to-gitea` — required tokens

The mirror service reads the user's GitHub repos and creates mirrors on Gitea.
It needs two tokens in `.secrets.env`:

```
GITEA_TOKEN=<gitea token>
GITHUB_TOKEN=<github fine-grained PAT>
```

**Gitea token** — created in Gitea web UI: **Settings → Applications →
Generate New Token**. Needs at least `read:user` and `write:repository`.
(Without `read:user` it crashes: `required=[read:user], token scope=write:repository`.)

**GitHub token** — fine-grained PAT (Settings → Developer settings →
Personal access tokens → Fine-grained tokens). Requirements:
- **Account permissions → Profile → Read** (this is what grants the `read:user`
  scope the tool asks for; the classic `read:user` scope no longer exists for
  fine-grained PATs, and Profile only offers No access / Read / Read-and-write).
- **Repository access** must include the repos to mirror.
- **Repository permissions → Contents → Read** (source repos only need to be
  *read*; mirror repos are created on the Gitea side with the Gitea token).

`MIRROR_PRIVATE_REPOSITORIES=true` is set in the compose file, so the GitHub
token also needs access to the private repos you want mirrored.

After updating `.secrets.env`:

```sh
cd ~/docker-compose-services/dev/gitea && docker compose up -d mirror-to-gitea
docker logs -f mirror-to-gitea   # should print "Starting with the following configuration: ..." then sync
```

A healthy run prints repo-by-repo mirror status (`Repository is already
mirrored; doing nothing: <name>` or creates them) and finishes with
`Did it!` / `Waiting for 3600 seconds...` (it re-syncs hourly).

## Troubleshooting

- `token does not have at least one of required scope(s), required=[read:user]`
  on `GET /api/v1/user` (Gitea) → Gitea token missing `read:user`.
- Same message from GitHub (`GET /user`, `github.com`) → PAT needs
  Account → Profile → Read.
- `mirroring private repositories requires setting GITHUB_TOKEN` → GitHub token
  missing or empty while `MIRROR_PRIVATE_REPOSITORIES=true`.

## See also

- Dotfiles push setup / remotes: `AGENTS.md` (origin has GitHub + LAN push URLs)
- The other DockerHost mounts: `dockerhost-nfs.md`, `mac-stash.md`