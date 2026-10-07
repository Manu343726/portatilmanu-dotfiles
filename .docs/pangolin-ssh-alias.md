# Pangolin SSH → sesión real: alias `p-manu343726` → `manu343726`

> **TL;DR:** Automated Provisioning de Pangolin crea la cuenta con un prefijo
> `p-` hardcodeado y sin opción de cambiarlo. Un bloque de 3 líneas en
> `.bash_profile` de esa cuenta redirige cada sesión interactiva a la cuenta real
> `manu343726` vía `sudo -u manu343726 -i`, de modo que la terminal de la web
> termina en la sesión de siempre (uid 1000, `$HOME`, dotfiles zsh, tmux).
> Script: `.docs/pangolin-ssh-alias.sh` (este repo) →
> `/usr/local/sbin/pangolin-ssh-alias.sh` (VM).

| | |
|---|---|
| Máquina | VM 108 `homelab` — ver [`homelab-vm.md`](homelab-vm.md) |
| Fecha | 2026-10-07 |
| Ficheros tocados | `/home/p-manu343726/.bash_profile` y `/home/p-manu343726/.bashrc` (backups `*.bak` al lado), `/usr/local/sbin/pangolin-ssh-alias.sh` |
| Rollback | borrar el bloque / restaurar `*.bak` / volver a *Manual Authentication* en la web |

## 1. Contexto: qué hay montado

- **Recurso SSH en Pangolin Cloud** (`app.pangolin.net`, organización autenticada
  vía SSO contra Authentik — "Organization Identity Provider").
- Configuration del recurso: Mode = **Pangolin SSH**, Authentication Method =
  **Automated Provisioning** (`pamMode: push`), Auth Daemon Location = **On Site**.
- El sitio corre como **servicio binario** `pangolin-site.service`
  (`/usr/bin/pangolin up site`, `User=root`, enabled+active) — no Docker; el modo
  SSH nativo lo exige.
- Flujo de acceso: FQDN del recurso → SSO Authentik → terminal en el navegador.
  Ni password ni clave en ningún momento: el connector (newt, embebido en el
  site) recibe un certificado SSH firmado por el control plane y monta la sesión
  en la máquina.

## 2. El problema: el prefijo `p-` es hardcodeado (y la doc no lo cuenta)

Tras la primera prueba, `whoami` devolvió **`p-manu343726`** (uid 1001, home
`/home/p-manu343726`, shell `/bin/bash`), no `manu343726`. Causa, en
`server/routers/ssh/signSshKey.ts` del repo `fosrl/pangolin`:

```js
// prefix with p-
usernameToUse = `p-${usernameToUse}`;   // línea 372 — sin flag, sin env, sin config
```

Evidencia recogida el 2026-10-07:

| Qué | Hallazgo |
|---|---|
| Comportamiento observable | `whoami` → `p-manu343726` en la VM (la realidad manda sobre la doc) |
| Origen del prefijo | commit **«Prefix usernames» (2026-02-25)** en la historia de `signSshKey.ts` |
| Doc oficial (`/manage/ssh`) | dice "derives the remote username from your Pangolin identity (the part before @)" + sufijo numérico si hay colisión en la org — **nunca menciona el `p-`**. El prefijo entró 2 días antes del release 1.16 (febrero 2026) cuya doc es la que se lee; nunca se actualizó |
| ¿Se puede desactivar? | No: cero flags, variables de entorno, issues o endpoints. `pamUsername` vive solo en la tabla `userOrgs` de la BD, y en Pangolin Cloud no hay UI ni API para editarlo |
| Los dos modos de `pamMode` | `push` (Automated) **siempre** aplica el prefijo; `passthrough` (Manual) usa el usuario tecleado — pero entonces hay que teclear usuario y password (o pegar la clave privada) en cada conexión |

Traducción: con Automated Provisioning, `p-manu343726` es **inevitable**; con
Manual puedes escribir `manu343726`, pero pierdes el login de un clic vía SSO.

## 3. Por qué este hack

Se valoraron tres opciones:

- **A.** Manual Authentication + clave privada — mapeo exacto, cero cambios de
  máquina (la clave pública ya está en `authorized_keys`), pero hay que elegir
  clave/p password y no es "abrir y estar".
- **B.** Manual Authentication + password — requeriría tocar `sshd`
  (`PasswordAuthentication no` en `/etc/ssh/sshd_config.d/10-homelab.conf`).
- **C.** Mantener Automated Provisioning — login SSO puro, sin nada que teclear,
  pero con la cuenta `p-`.

Se eligió **C** y, para no renunciar a la identidad real, se le añadió este hack:
la cuenta `p-` sigue siendo la **puerta** (la crea y autoriza Pangolin tal cual),
pero cada sesión interactiva **salta acto seguido** a `manu343726`.

## 4. En qué consiste el hack

### El bloque

```sh
# /home/p-manu343726/.bash_profile (y .bashrc) — primeras líneas
if [ -t 0 ] && [ "$(id -un)" = "p-manu343726" ]; then
    exec sudo -u manu343726 -i
fi
```

### Por qué `.bash_profile` y no `.profile`

- newt arranca la sesión con `exec.Command(shell, "--login")` dentro de una PTY
  (`nativessh/pty_unix.go`), como `p-manu343726`, con env
  `HOME`, `SHELL`, `USER` y **`TERM=xterm-256color`** (línea 57 del mismo fichero).
- Un bash de login lee, en orden, `~/.bash_profile` → `~/.bash_login` →
  `~/.profile` y **para en el primero que exista**.
- `/etc/skel` de Manjaro trae `.bash_profile` (no `.profile`), así que
  `useradd -m` copia `.bash_profile` al crear la cuenta: **es el que se lee**.
  Un bloque en `.profile` jamás se ejecutaría.
- `.bashrc` lleva el mismo bloque por si en el futuro newt lanzara un shell
  interactivo no-login (bash interactiva no-login lee `.bashrc`).

### Por qué `[ -t 0 ]`

Solo redirige cuando hay **tty** — es decir, la PTY real de Pangolin. Una
ejecución no interactiva (`bash --login -c 'comando'`, sin tty) **no** redirige:
como `exec` sustituye el proceso, el `-c` original se perdería y quedaría un
shell interactivo colgado esperando input. El guard evita romper eso.

### Por qué no pide password

El auth-daemon crea `p-manu343726` con `SudoMode: full`, lo que le escribe un
fichero en `/etc/sudoers.d/` con `(ALL) NOPASSWD: ALL` (verificado con `sudo -l`).
El `exec sudo -u manu343726 -i` pasa sin prompt, y `sudo -i` **preserva `TERM`**
(verificado: `TERM=foo-256color` sobrevive el salto), así que colores y
keybindings quedan intactos.

### Flujo resultante

```
navegador → SSO Authentik → Pangolin firma cert (principalf: p-manu343726)
  → newt: PTY + exec bash --login   (uid 1001, HOME=/home/p-manu343726)
    → .bash_profile: guard OK → exec sudo -u manu343726 -i
      → zsh login de manu343726     (uid 1000, HOME, dotfiles, tmux, omz)
```

## 5. Por qué sobrevive a re-provisiones

`authdaemon/host_linux.go` (newt) ejecuta en **cada** conexión:

| Operación | ¿Toca el bloque? |
|---|---|
| `ensureUser`: `useradd` solo si la cuenta **no existe** | No (la cuenta ya está) |
| `reconcileUser`: `usermod -G` (grupos exactos) + fichero sudoers | No (ni home ni ficheros de usuario) |
| `copySkelInto`: «Only creates files that don't already exist» — `os.Stat(dst) == nil → continue` | **No sobreescribe** `.bash_profile`/`.bashrc` |
| uid / shell / home / nombre de usuario | Nunca se reescriben tras la creación |

**Único caso que pierde el bloque:** si la cuenta se **borra y recrea**
(`useradd -m` copia skel limpio). Hoy Pangolin solo crea si falta, no borra. Si
algún día pasara: `sudo /usr/local/sbin/pangolin-ssh-alias.sh`.

## 6. Comportamiento esperado

| Escenario | Resultado |
|---|---|
| Terminal web, `whoami` | `manu343726` ✅ |
| `$HOME` / `$USER` / uid | `/home/manu343726` / `manu343726` / `1000` |
| Shell, prompt, tmux, oh-my-zsh | Idénticos a un SSH normal (`ssh Homelab`) |
| Ficheros creados en la sesión | Propiedad `manu343726` (uid 1000) |
| `sudo` dentro de la sesión | El wheel NOPASSWD preexistente de `manu343726` |
| Log de conexiones de Pangolin | **`p-manu343726`** — es la identidad de acceso; la sesión OS es la tuya. El audit trail queda "desacoplado" de la sesión real, a saber |
| Comandos no interactivos (sin PTY)* | Corren como `p-manu343726` (uid 1001), por diseño — ver §4 |
| SSH directo / otras cuentas | Sin cambios: el guard solo actúa con `id -un = p-manu343726` |

\* en modo nativo casi todo pasa por PTY, pero si se invocara algo sin tty (p. ej.
`bash --login -c`), no se redirige a propósito.

## 7. Instalar / verificar / rollback

### Instalar (desde portatilmanu)

```bash
ssh Homelab 'sudo -n tee /usr/local/sbin/pangolin-ssh-alias.sh >/dev/null && \
  sudo -n chmod 755 /usr/local/sbin/pangolin-ssh-alias.sh' < .docs/pangolin-ssh-alias.sh
ssh Homelab 'sudo -n /usr/local/sbin/pangolin-ssh-alias.sh'
```

Idempotente: re-ejecutar imprime `SKIP (ya aplicado)` por fichero; siempre deja
`*.bak` junto a cada dotfile la primera vez.

### Verificar

```bash
# Replica la sesión PTY de newt (pty_unix.go) sin salir del portatil:
printf 'echo WHO=$(whoami); exit\n' | ssh Homelab 'TERM=xterm-256color timeout 30 \
  script -qec "sudo -n -u p-manu343726 /bin/bash --login" /dev/null' | tr -d '\r' | \
  grep -aoE 'WHO=(p-)?manu343726' | tail -1
# → WHO=manu343726
```

O simplemente conectarse desde el recurso en la web y ejecutar `whoami`.

### Rollback

```bash
ssh Homelab 'sudo -n mv /home/p-manu343726/.bash_profile.bak /home/p-manu343726/.bash_profile &&
  sudo -n mv /home/p-manu343726/.bashrc.bak /home/p-manu343726/.bashrc &&
  sudo -n rm /usr/local/sbin/pangolin-ssh-alias.sh'
```

Y en la web: *Authentication Method* → `Manual Authentication` (entonces el
usuario se teclea — la cuenta `p-` queda huérfana: `sudo userdel -r p-manu343726`).

## 8. Referencias

- `fosrl/pangolin` → `server/routers/ssh/signSshKey.ts` (líneas 371-372: el
  prefijo); commit «Prefix usernames» 2026-02-25; docs `https://docs.pangolin.net/manage/ssh`
  (describe la derivación sin el prefijo — desactualizada).
- `fosrl/newt` → `nativessh/pty_unix.go` (PTY + `--login` + `TERM=xterm-256color`)
  y `authdaemon/host_linux.go` (`ensureUser`, `reconcileUser`, `copySkelInto`).
- [`homelab-vm.md`](homelab-vm.md) — la máquina.
