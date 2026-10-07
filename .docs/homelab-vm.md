# VM 108 `homelab` — Manjaro headless para el homelab AI-operable

Creada 2026-10-02 en `servernotangordo`. Es el **host Docker del homelab nuevo**, que
sustituye a los servicios existentes de `DockerHost` (102) y `DockerHost2` (106); de
esos todavía no se ha migrado nada.

| | |
|---|---|
| VMID | 108 |
| Nodo | `servernotangordo` |
| IP LAN real | `192.168.0.30` (DHCP) |
| MAC | `BC:24:11:C8:84:8A` |
| SSH | `ssh homelab` (alias en `~/.ssh/config`, user `root`) |
| SO | Manjaro Linux, kernel `6.1.187-2-MANJARO`, perfil mínimo sin escritorio |
| Disco | `local-lvm:vm-108-disk-0`, 100 GB, GPT |
| Particiones | `sda1` 1 MiB `ef02` (BIOS boot para GRUB) + `sda2` 100 GB ext4 en `/` |
| CPU / RAM | 6 vCPU (`cpu: host`) / 10 GB (`balloon: 2048`) |
| Red | `virtio` sobre `vmbr0`, `firewall=1` |
| Puerto serie | `serial0: socket` (añadido 2026-10-03; ver *Consola*) |
| Arranque | `order=scsi0`, **sin ISO** (`ide2: none`) |
| Otros | `onboot: 1`, `startup: order=3,up=30`, `agent: 1`, `serial-getty@ttyS0` |

## SO y servicios

Instalado con `pacstrap` (base + `linux`, `linux-firmware`, `grub`, `openssh`, `sudo`,
`git`, `curl`, `wget`, `rsync`, `ca-certificates`, `pacman-contrib`, `base-devel`,
`qemu-guest-agent`, `nfs-utils`, `iptables-nft`), más `docker`, `docker-compose` e
`inetutils` instalados después. Docker 29.7.2 / Compose 5.5.1.

Activos: `systemd-resolved`, `systemd-networkd`, `sshd`, `qemu-guest-agent`, `docker`,
`serial-getty@ttyS0`, `pangolin-site` (site de Pangolin Cloud añadido 2026-10-07:
SSH público sobre SSO — documentado en el repo **Homelab**,
`docs/pangolin-ssh-alias.md`). No hay escritorio ni NetworkManager (red con
`systemd-networkd`).

Acceso: **solo por clave pública**, sin contraseña por SSH (`PermitRootLogin
prohibit-password`). Hay password de root (`homelab`) para la consola/serie, donde antes
no había ninguna. Las claves de `harmony-owner` y `manu343726@portatilmanu` están en
`/root/.ssh/authorized_keys`.

Red por DHCP a propósito: el router Vodafone solo acepta reglas de port-forward para
equipos de su tabla DHCP (ver [`dhcp-migration.md`](dhcp-migration.md)), así que una IP
fija impediría exponer servicios. `systemd-resolved` se añadió después porque el perfil
mínimo no lo trae y sin él el guest no resolvía nombres.

## Shell: zsh + oh-my-zsh

`zsh` es el shell principal de root (`/usr/bin/zsh`), con Oh My Zsh instalado en
`/root/.oh-my-zsh`.

Adaptado de [`zsh.md`](zsh.md), **no copiado**. Lo que se quitó y por qué:

| Cambio | Motivo |
|---|---|
| `ZSH_TMUX_AUTOSTART=true` **eliminado** | En un servidor cada sesión SSH debe ser usable tal cual; autoenvolver en tmux deja al admin encerrado si tmux muere. El plugin `tmux` sí está, para usarlo a propósito: `tmux new -s nombre`. |
| Rutas `/home/manu343726/...` **eliminadas** | NVM, LM Studio, opencode, nchat y la integración de VSCode son del portatil. |
| Tema `agnoster` **mantenido** con los glifos powerline | Al confundir el síntoma: sin fuente parcheada se ve basura, y degradar a ASCII lo "arregla" pero deja de parecerse al portatil. La causa era que faltaba **Hack Nerd Font**; con ella el prompt va completo, igual que en el portatil. |
| Alias `eza`/`bat`/`rg`/`fd`, `fzf`, `zoxide` **mantenidos** | Se instalaron las herramientas (`eza bat ripgrep fd fzf zoxide tmux`) para que ningún alias quede apuntando a un binario inexistente. |
| Paleta Monokai **mantenida** | `AGNOSTER_DIR_BG='#A6E22E'`, `AGNOSTER_DIR_FG='#272822'`, `ZSH_AUTOSUGGEST_HIGHLIGHT_STYLE='fg=#75715E'`. |

`zsh -i -c '...'` emite `(anon):12: character not in range` y `can't change option: zle`;
**es un quirk de zsh 5.9.2 con `-i -c`, no de esta configuración** — se reproduce con
`zsh -i -c true` sin cargar nada. En una sesión interactiva real sobre pty no aparece.

Para reproducir el estado del portatil aquí: `cp ~/.zshrc` desde portatilmanu y quitar
las rutas específicas del portatil.

## Locale, teclado y consola

`LANG=en_US.UTF-8` (teclado americano) y `KEYMAP=us`. El locale se fija en **tres sitios**,
porque cada uno cubre una vía distinta y basta con uno para que parezca que no funciona:

| Fichero | Cubre |
|---|---|
| `/etc/locale.conf` | servicios de systemd |
| `/etc/environment` | sesiones PAM, o sea **SSH** (`UsePAM yes`) |
| `/etc/zsh/zshenv` | zsh, que **no lee `/etc/profile`** |

**Hay dos consolas distintas y no conviene mezclarlas:**

| | Dispositivo | Cómo se ve |
|---|---|---|
| Web de PVE (noVNC) | `tty0`, VGA de texto | la que sale "fea" sin ajustes |
| Serie | `ttyS0` (`TERM=vt220`) | texto plano, estable |

> **Corrección (2026-10-03):** antes de esta fecha **la VM no tenía puerto serie**.
> `qm create` lo dejó sin `serial0`, así que `/var/run/qemu-server/108.serial` era un
> fichero vacío y `serial-getty@ttyS0` no tenía nada al que conectarse — pese a que la
> documentación anterior afirmaba lo contrario. Añadido con `qm set 108 --serial0 socket`.

Para que la consola se vea como en el portatil está replicada su configuración exacta
de TTY, documentada en [`kmscon.md`](kmscon.md):

- `kmscon` en **`tty1`**, con `/etc/kmscon/kmscon.conf` → symlink a
  `/root/.config/kmscon/kmscon.conf`, **idéntico byte a byte** al del portatil: fuente
  `Hack Nerd Font Mono` 12px con `font-engine=freetype`, paleta Monokai completa,
  `xkb-layout=us`, `switchvt`, `multi-monitor=largest`, `session-control`.
- Paquetes `kmscon fontconfig libx11 ttf-hack-nerd`.
- El `.zshrc` lleva la misma línea que el portatil:
  `[[ "$TERM" == kmscon ]] && export TERM=xterm-256color`.
- `serial-getty@ttyS0` sigue activo como segunda vía.

Funciona porque la VGA de QEMU expone KMS (`bochs-drm`, `card0-Virtual-1`), así que
kmscon tiene contra qué trabajar; no hizo falta cambiar a `virtio-gpu`.

> **tty1, no tty2.** En el portatil kmscon va a tty2 porque tty1 lo ocupa el display
> manager. Aquí no hay display manager, así que va a **tty1**: es lo que muestra la web de
> PVE al entrar, sin tener que cambiar de VT. El unit lleva `Conflicts=getty@%i`, así que
> `getty@tty1` queda desplazado por él, y `autovt@tty1` apunta a `kmsconvt`, de modo que al
> arrancar la consola web va directa a kmscon. `kmsconvt@tty2` está deshabilitado.

**Hack Nerd Font es la clave del aspecto**: trae los glifos powerline y de iconos, que es
justo lo que agnoster y `eza --icons` necesitan. Sin ella sí que salen como basura. Por eso
aquí **no** se degrada el prompt a ASCII: en el portatil tampoco se degrada.

### Acceder por serie

```sh
ssh ServerNoTanGordo \
  'socat - UNIX-CONNECT:/var/run/qemu-server/108.serial0'
# login: root / homelab
```

Sin esto, si la VM no arranca, la única vía es la web de PVE (o `qm terminal 108`).

## Reinstalar

La instalación está automatizada en el host: `/root/install-homelab.sh`. Borra y
recrea la partición root, así que **no es idempotente sobre datos que importen**.

```sh
ssh ServerNoTanGordo 'bash /root/install-homelab.sh'   # reinstalación completa
```

Antes de arrancar la VM hay que **desmontar** el target y soltar el `losetup` (el script
hace las dos cosas en su `cleanup`). Y nunca lanzar la VM mientras corre el script: el
guest escribiría sobre el mismo disco que se está instalando.

## Por qué la instalación es un script y no Calamares

El ISO oficial no trae `console=ttyS0` en el kernel cmdline de `kernels.cfg`, así que
arrancar el instalador de forma desatendida por consola serie no era posible sin
reempaquetar la ISO. Se optó por **construir el sistema directamente sobre el disco desde
el host**, con el userland de Manjaro ya extraído (`unsquashfs` de
`manjaro/x86_64/rootfs.sfs` del ISO a `/root/mjroot`) y `pacstrap` desde un chroot.
Ventaja adicional: el resultado es un sistema mínimo y controlado, no un perfil de
escritorio.

Pasos del script, por si hay que rehacerlo:

1. `losetup` sobre el **disco entero** + `partx --add` → particiones reales visibles.
2. `mkfs.ext4` del tamaño **exacto** de `sda2`.
3. Bind mounts de `/dev /proc /sys /run` en `/root/mjroot`, y `/mnt/t108` visible en la
   misma ruta **dentro** del chroot (`/root/mjroot/mnt/t108`) — sin esto `pacstrap` ve un
   directorio vacío.
4. `pacstrap` (sin `-K`, con `SigLevel = Never` solo para esa transacción; el
   `pacman.conf` original se restaura después).
5. Configuración base, `locale-gen`, `fstab`, claves SSH.
6. `mkinitcpio -P`.
7. `grub-install` **contra el `losetup`, nunca contra el thin volume de LVM**.
8. `grub-mkconfig`, servicios, verificación del MBR.

## Notas de operación

- **RAM:** el host tiene 29 GB y ya estaban asignados 28 (102=20, 104=4, 106=4). Los
  10 GB de la 108 salen de la RAM libre más la de `DockerHost2`, que está parada. Hay
  overcommit (~34 GB asignados sobre 29 físicos); `balloon: 2048` deja que la VM ceda
  memoria bajo presión. Si aparecen OOM, lo primero es bajar `memory` de la 102.
- **Disco:** 100 GB en `local-lvm`. El pool thin se autoextendió de 338 a 758 GB el
  2026-10-02 tras el reinicio de `servernotangordo`.
- **Firewall:** `net0` lleva `firewall=1`. Antes de abrir puertos hacia la 108 hay que
  añadir la regla en el firewall de la VM, no solo en el host.
- **Migración desde `DockerHost`:** nada migrado todavía. `casa_nas_backups` ya respalda
  la 102; conviene añadir la 108 a la lista de backup.

## Incident 2026-10-02 — quorum perdido y `sshd` wedged

Durante la instalación de la 108, `servernotangordo` acumuló **procesos en estado `D`**
(no interrumpibles — `kill -9` no los saca) por reintentos de particionado con
`sgdisk`/`sfdisk` que colgaban. Eso terminó tumbando `sshd`: las sesiones autenticaban
pero el comando nunca devolvía nada.

Diagnóstico clave: **`dd` al mismo device iba a 400–500 MB/s** mientras `sgdisk` se
colgaba. No era la E/S del disco, era `sgdisk` esperando en `BLKRRPART` a que los
procesos en D liberaran el device en exclusiva.

Se perdió quorum (`Quorate: No`, `servergordo` reseteaba el handshake SSH y quedaba
fuera del anillo). La API de `servernotangordo` es un `pve-api-daemon/3.0` recortado que
**no implementa `PUT /nodes/{node}/status`** (`501`), y `pvesh` igual, así que no había
forma remota de reiniciar. El smart plug `enchufe servernotangordo Socket 1` tampoco
cortaba corriente (su entidad estaba `unavailable` en HA).

Se perdió también el MTU: SSH por ZeroTier a ese host quedó inutilizable. Ver
[`ssh-zerotier-mtu.md`](ssh-zerotier-mtu.md) — el lado de `portatilmanu` ya estaba
corregido a 1350, pero el host tenía el overlay a 2800.

**Resolución:** reinicio del host (se ordenó `shutdown`, tardó y terminó aplicándose;
uptime 0 min tras ~4 min). El reinicio limpió los procesos en D, `sgdisk` volvió a
funcionar con normalidad y el pool thin se autoextendió. El smart plug de
`servernotangordo` **sigue sin poder cortarse desde HA** — pendiente de arreglar.

**Lección:** cualquier operación de escritura directa sobre los thin volumes de las VMs
(`dd`, `mkfs`, `losetup`) puede dejar el device en un estado que tumbe el nodo entero.
Antes de tocar la tabla de particiones de un disco de VM en este cluster, comprobar
`dmsetup info -c` (`Open` > 0 = device tomado) y no encadenar `sgdisk`/`sfdisk` con
`set -e` sobre un script que corra en segundo plano.