# Pangolin SSH → sesión real: alias `p-manu343726*` → `manu343726`

> **TL;DR:** Automated Provisioning de Pangolin crea la cuenta con un prefijo
> `p-` hardcodeado y, si el nombre choca en la org, un sufijo numérico aleatorio
> (`p-manu343726`, `p-manu34372631`, ...). Un drop-in en `/etc/profile.d/` con
> pattern `p-manu343726*` redirige cada sesión interactiva a la cuenta real
> `manu343726` vía `sudo -u manu343726 -i`, así que la terminal de la web
> termina siempre en la sesión de siempre (uid 1000, `$HOME`, dotfiles zsh,
> tmux). Script: `.docs/pangolin-ssh-alias.sh` (este repo) →
> `/usr/local/sbin/pangolin-ssh-alias.sh` (VM).

| | |
|---|---|
| Máquina | VM 108 `homelab` — ver [`homelab-vm.md`](homelab-vm.md) |
| Fecha | 2026-10-07 |
| Ficheros tocados | `/etc/profile.d/99-pangolin-alias.sh`, `/usr/local/sbin/pangolin-ssh-alias.sh` |
| Rollback | `rm /etc/profile.d/99-pangolin-alias.sh` (y el script) |

## 1. Contexto: qué hay montado

- **Recurso SSH en Pangolin Cloud** (`app.pangolin.net`, org `manu343726`), con
  SSO delegado a Authentik vía "Organization Identity Provider".
- Configuration del recurso: Mode = **Pangolin SSH**, Authentication Method =
  **Automated Provisioning** (`pamMode: push`), Auth Daemon Location = **On Site**.
- El sitio corre como **servicio binario** `pangolin-site.service`
  (`/usr/bin/pangolin up site`, `User=root`, enabled+active) — no Docker; el modo
  SSH nativo lo exige (Pangolin CLI v0.18.1).
- Flujo de acceso: FQDN del recurso → SSO Authentik → terminal en el navegador.
  Ni password ni clave en ningún momento: el control plane firma un certificado
  SSH y el connector monta la sesión en la máquina.

## 2. El problema (I): el prefijo `p-` es hardcodeado

`server/routers/ssh/signSshKey.ts` (fosrl/pangolin):

```js
// prefix with p-
usernameToUse = `p-${usernameToUse}`;   // línea 372 — sin flag, sin env, sin config
```

No existe ningún flag, variable o endpoint para desactivarlo; `pamUsername` vive
solo en la tabla `userOrgs` de la BD (inaccesible en Pangolin Cloud).

## 3. El problema (II): el sufijo numérico de colisión

El nombre **no** es siempre el mismo. En el mismo fichero:

```js
// líneas 332-333: solo se genera si la ficha NO tiene pamUsername
if (!userOrg.pamUsername) {
    usernameToUse = `p-${email.split("@")[0].replace(...)}`;   // → p-manu343726

    // líneas 375-386: ¿otra ficha userOrgs de la org ya usa ese nombre?
    const [existingUserWithSameName] = await db.select().from(userOrgs)
        .where(and(eq(userOrgs.orgId, orgId), eq(userOrgs.pamUsername, usernameToUse)));

    if (existingUserWithSameName) {
        // líneas 388-408: sufijo aleatorio 0-100 (hasta 20 intentos)
        const randomNum = randomInt(0, 101);
        usernameToUse = `${usernameToUse}${randomNum}`;        // → p-manu34372631
    }
    await db.update(userOrgs).set({ pamUsername: usernameToUse })
        .where(and(eq(userOrgs.orgId, orgId), eq(userOrgs.userId, userId)));  // ← SE GUARDA
} else {
    usernameToUse = userOrg.pamUsername;   // ← se REUTILIZA siempre
}
```

Dos consecuencias:

1. **Se genera una sola vez por ficha de usuario y se guarda** — no cambia en
   cada conexión.
2. El sufijo aparece cuando **ya existe otra ficha** en la org con el mismo
   `pamUsername`.

**Causa raíz confirmada en el dashboard**: la org tiene **dos fichas de usuario
con el mismo email**:

| Usuario | Identity Provider | Rol |
|---|---|---|
| `manu343726@gmail.com` · *you* | OAuth2/OIDC **authentik** (SSO) | Admin |
| `manu343726@gmail.com` | **Internal** (registro email/password de Pangolin) | Owner Admin |
| `pablo@mac-veigh.com` | OAuth2/OIDC authentik | homelab-admins |

Ambas fichas derivan `p-manu343726`; la primera que se usó se quedó con el nombre
limpio y la segunda — al colisionar — recibió el sufijo aleatorio. El historial
de la VM lo clava:

```
19:17:50  connection: username="p-manu343726"      (creada la cuenta, uid 1001)
19:33:39  connection: username="p-manu343726"
19:34:37  connection: username="p-manu34372631"    (¡otra ficha! uid 1002)
19:44:03  connection: username="p-manu34372631"
...        (estable desde entonces)
```

Según con qué ficha se autentique la conexión, sale un nombre u otro — de ahí la
sensación de "usuario nuevo cada vez" (en realidad cambió una vez y se fijó).

## 4. Por qué este hack

Se valoraron tres opciones:

- **A.** Manual Authentication + clave privada — mapeo exacto, pero hay que
  elegir clave y no es "abrir y estar".
- **B.** Manual Authentication + password — requeriría tocar `sshd`.
- **C.** Mantener Automated Provisioning — login SSO puro, pero con la cuenta `p-`.

Se eligió **C** y se le añadió un redirect: la cuenta `p-*` sigue siendo la
**puerta** (la crea y autoriza Pangolin tal cual), pero cada sesión interactiva
**salta acto seguido** a `manu343726`.

## 5. En qué consiste el hack

### El drop-in

`/etc/profile.d/99-pangolin-alias.sh`:

```sh
if [ -t 0 ]; then
    case "$(id -un)" in
        p-manu343726*) exec sudo -u manu343726 -i ;;
    esac
fi
```

### Por qué `/etc/profile.d` y no `.bash_profile`

La primera versión ponía el bloque en `.bash_profile` de la cuenta concreta. Se
descartó al descubrir que **el nombre de la cuenta no es estable** (sufijo
aleatorio): un bloque atado a `p-manu343726` no aplica a `p-manu34372631`, y
habría que replicarlo en cada home nuevo. El drop-in:

- Es **uno solo** para todas las cuentas `p-manu343726*` (pattern, cubre
  cualquier sufijo futuro).
- Lo sourcea `/etc/profile` en **todo login shell** (líneas 23-25 de
  `/etc/profile` en Manjaro), y la sesión de newt es `bash --login` en PTY —
  verificado en `nativessh/pty_unix.go` (`exec.Command(shell, "--login")`).
- No depende de que el home exista ni de que `copySkelInto` copie nada.
- Es **inerte para todo lo demás**: el guard exige `[ -t 0 ]` (tty) y que el
  nombre case con `p-manu343726*`.

### Por qué `[ -t 0 ]`

Solo redirige con **tty** (la PTY real de Pangolin). Una ejecución no
interactiva (`bash --login -c 'comando'`) **no** redirige: como `exec` sustituye
el proceso, el `-c` original se perdería y quedaría un shell colgado esperando
input.

### Por qué no pide password

El auth-daemon crea la cuenta `p-*` con `SudoMode: full` → `/etc/sudoers.d/` con
`(ALL) NOPASSWD: ALL` (verificado con `sudo -l`). `sudo -i` además **preserva
`TERM`** (verificado), así que colores y keybindings quedan intactos.

### Flujo resultante

```
navegador → SSO Authentik → Pangolin firma cert (principal p-manu343726[NN])
  → newt: PTY + exec bash --login   (uid 1001/1002, HOME=/home/p-manu343726[NN])
    → /etc/profile.d/99-pangolin-alias.sh: guard OK → exec sudo -u manu343726 -i
      → zsh login de manu343726      (uid 1000, HOME, dotfiles, tmux, omz)
```

## 6. Por qué sobrevive a re-provisiones y renombrados

El auth-daemon (`authdaemon/host_linux.go` de newt), en cada conexión:

| Operación | ¿Afecta? |
|---|---|
| `ensureUser`: `useradd` solo si la cuenta no existe | No |
| `reconcileUser`: `usermod -G` + fichero sudoers | No |
| `copySkelInto`: solo crea ficheros que faltan | No toca `/etc/profile.d` (fuera del home) |
| uid / shell / home / username | No se reescriben |

Y como el guard es por **pattern de nombre** (no por home ni por cuenta concreta),
funciona igual si Pangolin genera un sufijo nuevo (`p-manu34372657`, ...) o crea
otra cuenta. **No hay nada que se pueda desincronizar** salvo borrar el drop-in.

## 7. Comportamiento esperado

| Escenario | Resultado |
|---|---|
| Terminal web, `whoami` | `manu343726` ✅ (con cualquiera de las dos fichas de Pangolin) |
| `$HOME` / `$USER` / uid | `/home/manu343726` / `manu343726` / `1000` |
| Shell, prompt, tmux, oh-my-zsh | Idénticos a un SSH normal (`ssh Homelab`) |
| Ficheros creados en la sesión | Propiedad `manu343726` (uid 1000) |
| `sudo` dentro de la sesión | El wheel NOPASSWD preexistente de `manu343726` |
| Log de conexiones de Pangolin | **`p-manu343726` o `p-manu34372631`** — es la identidad de acceso; la sesión OS es la tuya |
| Comandos no interactivos (sin PTY) | Corren como la cuenta `p-*`, por diseño — ver §5 |
| SSH directo / otras cuentas | Sin cambios: el guard solo actúa con `p-manu343726*` + tty |

## 8. Instalar / verificar / rollback

### Instalar (desde portatilmanu)

```bash
ssh Homelab 'sudo -n tee /usr/local/sbin/pangolin-ssh-alias.sh >/dev/null && \
  sudo -n chmod 755 /usr/local/sbin/pangolin-ssh-alias.sh' < .docs/pangolin-ssh-alias.sh
ssh Homelab 'sudo -n /usr/local/sbin/pangolin-ssh-alias.sh'
```

Idempotente. El script escribe el drop-in y limpia bloques v1 si los hubiera.

### Verificar

```bash
# Replica la sesión PTY de newt para la cuenta real (p-manu34372631):
printf 'echo WHO=$(whoami); exit\n' | ssh Homelab 'TERM=xterm-256color timeout 30 \
  script -qec "sudo -n -u p-manu34372631 /bin/bash --login" /dev/null' | tr -d '\r' | \
  grep -aoE 'WHO=[A-Za-z0-9_-]+' | tail -1
# → WHO=manu343726
```

O simplemente conectarse desde el recurso en la web y ejecutar `whoami`.

### Rollback

```bash
ssh Homelab 'sudo -n rm /etc/profile.d/99-pangolin-alias.sh /usr/local/sbin/pangolin-ssh-alias.sh'
```

## 9. Limpieza opcional (recomendada)

La causa raíz es tener **dos fichas de Pangolin con el mismo email**. Opciones,
todas desde el dashboard (`Settings → Access → Users`):

- **Fusionar/eliminar una** de las dos fichas de `manu343726@gmail.com`. Ojo: la
  *Internal* es **Owner Admin** (dueña de la org) y la OIDC es Admin; si se borra
  la Internal hay que transferir la propiedad antes. Decidir cuál se mantiene (lo
  natural es quedarse con la OIDC, que es la que usas con Authentik).
- Mientras existan ambas, el hack las cubre igual — no es urgente.

Quedan además **dos cuentas de sistema huérfanas** en la VM si se elimina una
ficha: `p-manu343726` (uid 1001) y `p-manu34372631` (uid 1002). La que ya no use
Pangolin se puede borrar:

```bash
ssh Homelab 'sudo userdel -r p-manu343726'    # solo si su ficha se elimina
```

## 10. Referencias

- `fosrl/pangolin` → `server/routers/ssh/signSshKey.ts`: prefijo (371-372) y
  sufijo de colisión (332-428); commit «Prefix usernames» 2026-02-25. Docs
  `https://docs.pangolin.net/manage/ssh` (describe la derivación sin prefijo ni
  sufijo explícitos — desactualizada).
- `fosrl/newt` → `nativessh/pty_unix.go` (PTY + `--login` + `TERM=xterm-256color`)
  y `authdaemon/host_linux.go` (`ensureUser`, `reconcileUser`, `copySkelInto`).
- [`homelab-vm.md`](homelab-vm.md) — la máquina.
