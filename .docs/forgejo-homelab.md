# Forgejo — el forge de git del homelab (Homelab / VM 108)

El forge **activo** es **Forgejo**, que corre en **Homelab (VM 108)**, el host Docker
del homelab nuevo (ver [`homelab-vm.md`](homelab-vm.md)). Sustituye al **Gitea** de
`DockerHost` (VM 102), que sigue en pie pero ya no es el del día a día
(ver [`gitea-dockerhost.md`](gitea-dockerhost.md)). El remote LAN del repo de dotfiles
apunta aquí: `ssh://git@192.168.0.30:2222/manu343726/portatilmanu-dotfiles.git`.

| | |
|---|---|
| Imagen | `codeberg.org/forgejo/forgejo:16.0.5-rootless` (Forgejo 16.0.5, API compatible Gitea 1.22) |
| Host | `Homelab` (VM 108, en `servernotangordo`) |
| Compose | `~/docker-compose-services/dev/forgejo/` en Homelab |
| Volumen | named `homelab-forgejo-data` → `/data` |
| Base de datos | Postgres central (`homelab-postgres`), DB `homelab_forgejo` |
| Config | variables `FORGEJO__*` del `compose.yaml` → `app.ini` en `/var/lib/gitea/custom/conf/app.ini` |
| Web / API | `http://192.168.0.30:3004` (contenedor 3000) · público `https://git.manu343726.com` (Pangolin) |
| SSH | `git@192.168.0.30:2222` · canónico `git@git.manu343726.com:2222` |

Como todo servicio del homelab nuevo, sigue `SERVICE-RULES.md`: detrás de Pangolin, OIDC
con Authentik, `/metrics` nativo, logs a Loki.

## Direcciones (ojo con la LAN)

- **LAN real `192.168.0.30`** — **fija desde 2026-10-15** (era DHCP; ver
  [`homelab-vm.md`](homelab-vm.md)): el router solo acepta port-forward para equipos de su
  tabla DHCP, pero las reglas apuntan a DockerHost, no a este VM.
  Desde `portatilmanu` puede no responder según el momento: el 2026-10-10 los puertos
  `2222` y `3004` daban *timeout* mientras ZeroTier funcionaba.
- **ZeroTier `172.25.73.178`** — la vía **durable**. `ssh manu343726@172.25.73.178`
  entra al host; `ssh://git@172.25.73.178:2222/...` es el remote de git. Preferir ZT para
  remotes que deban durar.
- La web pública va por Pangolin con SSO de Authentik. **Git por HTTPS no es usable**
  (el challenge SSO no lo puede responder git), así que push/pull van **siempre por SSH**
  (de ahí `START_SSH_SERVER` en el puerto 2222).

## Operación

```sh
ssh manu343726@172.25.73.178          # o: ssh Homelab  (LAN 192.168.0.30)
cd ~/docker-compose-services/dev/forgejo
docker compose up -d forgejo          # arranca / recrea con la config actual
docker compose logs -f forgejo
docker exec forgejo sh -c 'grep -i push_create /var/lib/gitea/custom/conf/app.ini'
```

- **No editar `app.ini` a mano**: se regenera desde las variables `FORGEJO__<sección>__<clave>`
  del `compose.yaml` (mapeo `__` → `.`). Para cambiar algo: editar el `compose.yaml` y
  `docker compose up -d forgejo`. El cambio va al repo **Homelab** (commiteado).
- La imagen es rootless (uid 1000) y el volumen es named (no bind mount) por eso.

## Push-to-create (habilitado 2026-10-10)

```yaml
FORGEJO__repository__ENABLE_PUSH_CREATE_USER: "true"
FORGEJO__repository__ENABLE_PUSH_CREATE_ORG: "true"
```

Con esto, `git push` a un repo inexistente lo crea. **Importante:** crear por API
(`POST /api/v1/user/repos`) exige un token con scope `write:user`, que el token del
mirror **no** tiene — push-to-create evita esa dependencia.

## Tokens y mirror

`~/docker-compose-services/dev/forgejo/.env` (mode 600, gitignored): `FORGEJO_PORT`,
`FORGEJO_DOMAIN`, `DB_PASSWORD`, `SECRET_KEY`, `INTERNAL_TOKEN`, `MIRROR_GITEA_TOKEN`,
`MIRROR_GITHUB_TOKEN`.

- **`MIRROR_GITEA_TOKEN`** — token de `manu343726` con `read:user` + `write:repository`.
  Vale para leer la API (`GET /api/v1/user`, `GET /api/v1/repos/search`), pero **no**
  para crear repos.
- **`SECRET_KEY` / `INTERNAL_TOKEN`** — se generan una vez; cambiarlos cierra todas las
  sesiones y rompe los tokens.
- **`forgejo-mirror`** (`jaedle/mirror-to-gitea`) espeja cada repo de la cuenta de GitHub
  (`manu343726`) a la organización **`github-mirrors`**, privados incluidos, cada hora. La
  sincronización continua posterior la hace el pull-mirror propio de Forgejo.

## Cuentas y acceso

- **Sin registro local** (`DISABLE_REGISTRATION`) y la instancia requiere sesión
  (`REQUIRE_SIGNIN_VIEW`). El único admin local es `manu343726` (bootstrap); el resto de
  cuentas se crean en el primer login **OAuth2 de Authentik** (`ENABLE_AUTO_REGISTRATION`).
- Claves SSH registradas: `portatil` (portatilmanu) y `homelab-vm`.
- Organizaciones: `homelab` (privada) y `github-mirrors` (espejos públicos).

## Crear un repo

Preferido: **push-to-create** (ya habilitado).

```sh
git init -b main && git add -A && git commit -m "initial commit"
git remote add origin ssh://git@172.25.73.178:2222/manu343726/<repo>.git
git push -u origin main        # lo crea si no existe
```

Alternativa por API: necesita un token con `write:user` (o el endpoint admin
`POST /api/v1/admin/users/{user}/repos` con `write:admin`). Token temporal desde el host:

```sh
docker exec -u git forgejo forgejo admin user generate-access-token \
  --username manu343726 --token-name tmp --scopes all --raw
# ...usar el token...
# revocar (no hay CLI para borrarlo; el DELETE de la API pide basic auth):
docker exec -e PGPASSWORD="$DB_PASSWORD" homelab-postgres \
  psql -U homelab_forgejo -d homelab_forgejo -c "DELETE FROM access_token WHERE name='tmp';"
```

## Repos conocidos

- `manu343726/portatilmanu-dotfiles` — dotfiles de `portatilmanu`.
- `homelab/Homelab` — repo del homelab (vive en el `$HOME` de la VM 108).
- `manu343726/archlinux-rpi3` — tooling de la imagen Arch ARM para Raspberry Pi 3B+.
- `github-mirrors/*` — espejos automáticos de GitHub.
