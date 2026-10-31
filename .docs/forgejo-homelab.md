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
con Authentik, `/metrics` nativo, logs a Loki. El SSO de Pangolin sigue puesto: la página web
va por él. Lo que no puede ir por ahí es git (ver *Direcciones*).

## Direcciones (ojo con la LAN)

- **LAN real `192.168.0.30`** — **fija desde 2026-10-15** (era DHCP; ver
  [`homelab-vm.md`](homelab-vm.md)): el router solo acepta port-forward para equipos de su
  tabla DHCP, pero las reglas apuntan a DockerHost, no a este VM.
  Desde `portatilmanu` puede no responder según el momento: el 2026-10-10 los puertos
  `2222` y `3004` daban *timeout* mientras ZeroTier funcionaba.
- **ZeroTier `172.25.73.178`** — la vía **durable**. `ssh manu343726@172.25.73.178`
  entra al host; `ssh://git@172.25.73.178:2222/...` es el remote de git. Preferir ZT para
  remotes que deban durar.
- La web pública va por Pangolin **con su SSO**, y eso tiene una consecuencia para git: un
  cliente git no puede responder un challenge HTML, así que sobre `git.manu343726.com` recibe
  un `302` a `https://pangolin.manu343726.com/auth/resource/...` y falla con *unable to update
  url base from redirection*. Por eso:
  - **push/pull a mano** van por **SSH** (`START_SSH_SERVER` en el puerto 2222), como siempre;
  - **el CI (y cualquier cliente de la LAN) usa el nombre de la LAN**:
    **`http://git.manu343726.net:3004`**. Ese nombre lo resuelve Pi-hole
    (`network/pihole/config/etc-dnsmasq.d/99-lan-domain.conf`) a `192.168.0.30` y habla con
    Forgejo **directamente en el 3004**, sin ingress ni SSO por medio; el namespace
    `*.manu343726.net` es **solo de la LAN** (no está registrado en internet), lo cual basta
    porque los dos runners están en casa.
  - El intento de mantener ambos (reglas *Bypass Auth* por path en Pangolin para dejar pasar
    solo `/api/*` y las rutas smart-HTTP) **se probó y se revirtió**: en `ee-1.24.0` no surtió
    efecto. Ver `homelab/Homelab#327` y su comentario de reversión.

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

## Actions (CI): dos runners de instancia

Desde 2026-10-31 la instancia tiene **dos runners de instancia** (registrados con
`owner_id 0`, filas de `action_runner` en la DB central), los dos con `capacity: 1` y los dos
anunciando, además de `docker`, una etiqueta de host para poder fijar un job a una máquina:

| runner | host | etiquetas | dónde vive el registro |
|---|---|---|---|
| `forgejo-runner-108` | VM 108 (`Homelab`) | `docker`, `homelab` | volumen `homelab-forgejo-runner-data` |
| `forgejo-runner-pcgordo` | PCGORDO (WSL) | `docker`, `wsl` | volumen `forgejo-runner_forgejo-runner-data` |

`runs-on: docker` lo coge cualquiera de los dos; `runs-on: [docker, homelab]` y
`runs-on: [docker, wsl]` fijan el host. Consecuencia directa: **un workflow no puede depender
de estado local del host** — ni rutas absolutas, ni nada fuera de su propio checkout, ni un
servicio que exista solo en una de las dos máquinas.

El `capacity: 1` no es prudencia genérica: cada runner trae **su propio par de daemons
Docker-in-Docker**, declarado en su compose (VM 108: `dev/forgejo/compose.yaml`, servicios
`ci-dind` y `ship-dind`; PCGORDO: `dev/forgejo/runner-wsl/compose.yaml`, mismos nombres), con
tmpfs de **3 GiB (`ci-dind`)** y **6 GiB (`ship-dind`)** dimensionados para **una** compilación a
la vez. Dos compilaciones concurrentes contra el mismo daemon gastan el mismo presupuesto dos veces, y lo
que pierde la segunda no es un turno de cola sino un `no space left on device` convertido en capa
truncada u OOM dentro de `dockerd`; además la documentación de docker-access de Forgejo avisa de
que con `capacity > 1` sobre un dind compartido los contenedores de un job son visibles desde
otro. El paralelismo es **horizontal**: un job en cada host, nunca dos en el mismo. El precio es
cola: el CI de `z80-emulator` tiene ~9 jobs, así que como mucho dos vuelan y el resto espera.

Los jobs obtienen Docker **por TCP**, contra `tcp://ci-dind:2375` y `tcp://ship-dind:2375` (en
la red `homelab` del compose, donde resuelven por nombre en los dos hosts). El socket del host
**no se monta nunca** dentro del contenedor del job (`docker_host: "-"` en `runner-config.yml`),
así que ningún job tiene privilegio de kernel sobre el daemon de fuera. La imagen de los jobs es
stock, `catthehacker/ubuntu:act-24.04` (git, node y python3), y no una que compile este repo: una
imagen propia solo existe en el host que la construyó, y con un runner hermano en otra máquina eso
ya no se puede asumir.

Se retiraron en el mismo cambio la pila `dev/ci-dind/` (los dind pasan a ser del compose del
runner) y el par `dev/forgejo/runner-z80/`, que nunca llegó a desplegarse.

```sh
# VM 108
cd ~/docker-compose-services/dev/forgejo
docker compose up -d forgejo-runner ci-dind ship-dind

# PCGORDO, dentro de WSL: unit de usuario, con linger
systemctl --user status forgejo-runner.service
```

- **Editar `runner-config.yml` es un reinicio, no un `up -d`**: es un bind mount, y el daemon lo
  relee al arrancar. En la VM 108 es `docker compose restart forgejo-runner`; en PCGORDO,
  `docker compose restart runner` (allí el servicio se llama `runner`). Las etiquetas del fichero
  mandan sobre las que quedaron horneadas en `.runner` al registrarse, así que cambiar etiquetas
  también es un reinicio, **no** un re-registro.
- **El registro de PCGORDO vive en el volumen `forgejo-runner_forgejo-runner-data`** — con el
  prefijo del proyecto compose, porque allí el proyecto se llama `forgejo-runner` y no `forgejo`.
  Si ese volumen se pierde hay que **volver a registrar** contra
  **`http://git.manu343726.net:3004`**: la URL registrada no es solo la de la API, es también la
  que el daemon le pasa al job como *clone URL*, así que tiene que ser un nombre que se resuelva
  en los dos hosts y que no pase por el SSO de Pangolin. `https://git.manu343726.com` no vale
  (302 al login), y `http://forgejo:3000` tampoco (solo existe en la red compose de la VM 108).
  Los dos runners están registrados con el nombre de la LAN, y es lo que hace que
  `actions/checkout` diga `http://git.manu343726.net:3004/...` en el log.
- La unit `forgejo-runner.service` es de **usuario y necesita linger** (`loginctl enable-linger`),
  o no sobrevive a cerrar la sesión de WSL. PCGORDO no tiene el pase de arranque de
  `homelab-control`, así que esa unit más `restart: unless-stopped` es lo único que levanta los
  tres contenedores; en la VM 108 el ciclo de vida lo lleva el pase de arranque y por eso el
  `restart:` se omite a propósito.
- **El lint del repo de docs ya no manda nada a Loki** (commit `5d50e02`): su paso `report`
  escribía en `http://loki:3100`, un nombre que solo existe en la red Docker de la VM 108 y que un
  runner en otro host no resuelve. Por eso se retiró también la alerta de Grafana
  *CI: the docs repository's lint failed*, que leía ese stream y ya solo podía quedarse muda.

### Presupuesto y concurrencia (2026-10-31)

| | VM 108 (8 CPUs, 22 GiB) | PCGORDO WSL (24 CPUs, 30 GiB) |
|---|---|---|
| Presupuesto de CI | **4 CPU / 8 GB** | **20 CPU / 26 GB** |
| `ci-dind` (único daemon) | `cpus: 2`, `mem_limit: 6g`, tmpfs 4 GiB | `cpus: 10`, `mem_limit: 14g`, tmpfs 8 GiB |
| Contenedores de job | `--cpus=2 --memory=2g` | `--cpus=10 --memory=12g` |
| Daemon del runner | `cpus: 2`, `mem_limit: 512m` | sin tope propio (las dos capas ya suman) |

- **Ya no hay dos dind.** `ship-dind` desapareció; el único `ci-dind` lleva el alias de red
  `ship-dind`, así que el `DOCKER_HOST: tcp://ship-dind:2375` de z80 sigue resolviendo al mismo
  dockerd (mismo id de daemon por los dos nombres) y ningún workflow hubo que tocar.
- **Dos capas, no una:** el contenedor de job **no** vive dentro del dind (lo crea el runner en el
  daemon del host), así que el dind limita lo que se construye *dentro* y `container.options`
  limita el job. El peor caso por host suma el presupuesto de arriba.
- **El `mem_limit` tiene que ser mayor que el tmpfs.** Las páginas de tmpfs se cobran al cgroup
  del contenedor que las monta (medido en 108: `memory.current` del dind pasó de 679 MB a
  4,44 GB llenando el store), así que un tope por debajo del tmpfs convierte el fallo limpio
  (`no space left on device`) en OOM.
- **Consecuencia que hay que saber:** el job `ship` de `z80-emulator` necesita ~4,8 GB de store,
  así que **no cabe en la VM 108** (4 GiB) y allí falla limpiamente; necesita PCGORDO. Fijarlo con
  `runs-on: [docker, wsl]` es una línea en el workflow de *ese* repo, no de este.
- **Una acción a la vez por runner:** `capacity: 1` en los dos. El daemon declara su capacidad y
  Forgejo no le entrega un segundo task hasta terminar el primero; el resto queda en cola en la
  instancia. Probado con dos jobs del mismo workflow y el mismo label (run **587**): nunca hubo
  más de un contenedor `FORGEJO-ACTIONS-*` en el host y el segundo empezó al acabar el primero.
  Lo que sí puede correr en paralelo es **una acción por host** — el motivo de tener dos runners.

## Repos conocidos

- `manu343726/portatilmanu-dotfiles` — dotfiles de `portatilmanu`.
- `homelab/Homelab` — repo del homelab (vive en el `$HOME` de la VM 108).
- `manu343726/archlinux-rpi3` — tooling de la imagen Arch ARM para Raspberry Pi 3B+.
- `github-mirrors/*` — espejos automáticos de GitHub.
