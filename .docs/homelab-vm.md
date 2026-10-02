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
| Arranque | `order=scsi0`, **sin ISO** (`ide2: none`) |
| Otros | `onboot: 1`, `startup: order=3,up=30`, `agent: 1`, `serial-getty@ttyS0` |

## SO y servicios

Instalado con `pacstrap` (base + `linux`, `linux-firmware`, `grub`, `openssh`, `sudo`,
`git`, `curl`, `wget`, `rsync`, `ca-certificates`, `pacman-contrib`, `base-devel`,
`qemu-guest-agent`, `nfs-utils`, `iptables-nft`), más `docker`, `docker-compose` e
`inetutils` instalados después. Docker 29.7.2 / Compose 5.5.1.

Activos: `systemd-resolved`, `systemd-networkd`, `sshd`, `qemu-guest-agent`, `docker`,
`serial-getty@ttyS0`. No hay escritorio ni NetworkManager (red con `systemd-networkd`).

Acceso: **solo por clave pública**, sin contraseña (`PermitRootLogin
prohibit-password`). Las claves de `harmony-owner` y `manu343726@portatilmanu` están en
`/root/.ssh/authorized_keys`.

Red por DHCP a propósito: el router Vodafone solo acepta reglas de port-forward para
equipos de su tabla DHCP (ver [`dhcp-migration.md`](dhcp-migration.md)), así que una IP
fija impediría exponer servicios. `systemd-resolved` se añadió después porque el perfil
mínimo no lo trae y sin él el guest no resolvía nombres.

## Consola serie

`GRUB_CMDLINE_LINUX_DEFAULT` lleva `console=tty0 console=ttyS0,115200` y
`serial-getty@ttyS0` está habilitado. Es el único canal fiable para depurar un host sin
consola física, y el que se usó durante la instalación.

```sh
ssh ServerNoTanGordo 'timeout 30 socat -u /var/run/qemu-server/108.serial -,raw,echo=0'
# o, con la VM apagada, arrancarla y mirar la consola desde la web de PVE
```

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