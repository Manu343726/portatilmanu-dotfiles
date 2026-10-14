# Bluetooth audio (SMSL AD18) — cortes y microcortes

Amplificador: **SMSL AD18** (`08:13:5B:00:FF:DC`), receptor BT 4.2 con A2DP sink,
AVRCP y HFP/HSP. Además el AD18 es **DAC por USB** (ver "Plan B" al final).

## El adaptador del portátil

El X13 no tiene BT dedicado: es un **MediaTek MT7922 combo**, con las dos mitades
en el mismo chip y compartiendo front-end/antena:

| Parte | Bus | Identificación |
|---|---|---|
| WiFi | PCIe, driver `mt7921e` | `MEDIATEK MT7922` (`14c3:7922`) |
| Bluetooth | USB, driver `btusb` | `13d3:3568 IMC Networks Wireless_Device` |

Consecuencia: en esta máquina el BT **compite con el propio WiFi** por la radio.
Mantener el WiFi en **5 GHz** (y no en 2.4 GHz) es la primera regla de higiene.

## Causa raíz (14-10-2026): un AP WiFi pegado al amplificador

El síntoma era stuttering continuo y **desconexiones cada ~30 s**, en bucle: el
transporte A2DP se establecía, aguantaba ~30 s y moría. La prueba de que el
amplificador estaba bien era que **desde un móvil sonaba sin problema**.

El culpable era un **AP/extender WiFi colocado a centímetros del SMSL**: a esa
distancia el receptor BT del AD18 quedaba desensibilizado, la entrega de audio se
corrompía y el firmware del ampli acababa colgando el enlace él mismo. Se ve
clarísimo en `btmon`:

```
> HCI Event: Disconnect Complete   Handle: 50
        Reason: Remote User Terminated Connection (0x13)   ← lo corta EL AMPLI
@ MGMT Event: Device Disconnected
        BR/EDR Address: 08:13:5B:00:FF:DC
        Reason: Connection terminated by remote host (0x03)
```

**Lo que daña el enlace es la radio de 2.4 GHz de ese AP, no el AP en sí.**
Reproducido en los dos sentidos:

| Estado del AP | BT hacia el AD18 |
|---|---|
| Enchufado, 2.4 GHz emitiendo (o con emisión residual) | cortes cada ~30 s |
| 2.4 GHz apagado "a medias" (la radio seguía emitiendo algo) | seguía cortándose |
| Ciclo de encendido con el 2.4 GHz de verdad apagado | **0 fallos** |
| Enchufado de nuevo, **solo 5 GHz** (canal 48 a señal 100) | **0 fallos** |

Es decir: el AP puede convivir perfectamente con el amplificador, siempre que su
radio de 2.4 GHz esté apagada de verdad. Ojo con el matiz: "apagar la banda" en la
interfaz del AP no siempre la silencia — un ajuste a medias dejaba emisión residual
y los cortes continuaban. Si hace falta comprobarlo, un escaneo es concluyente:

```bash
nmcli -f BSSID,SSID,CHAN,SIGNAL dev wifi list | grep <BSSID-del-AP>
# el BSS de 2.4 GHz (canal 1..13) NO debe aparecer;
# el de 5 GHz puede estar ahí a plena señal sin problema.
```

Verificación con el journal durante la sesión estable: **0 `terminated
unexpectedly`** y **0 reconexiones de enlace**.


Síntomas acompañantes que también desaparecieron (todos consecuencia, no causa):

- `spa.bluez5.sink.media: connection (...) terminated unexpectedly`
- `a2dp.c:connect_cb() HUP or ERR on socket: Connection reset by peer (104)`
- `avdtp.c:handle_unanswered_req() No reply to Suspend request`
- kernel: `Bluetooth: hci0: ACL packet for unknown connection handle 3837`
  (tramas ACL corruptas llegando al host — la firma de la desensibilización)

## Cambios de software aplicados (además de retirar el AP)

Fichero: **`~/.config/wireplumber/wireplumber.conf.d/51-bluetooth.conf`**

1. **Códec forzado a SBC.** Por defecto se negociaba **aptX HD (576 kbps)** contra
   un receptor BT 4.2; con el enlace degradado eso no hay quien lo sostenga.
   Ojo: PipeWire **ordena los códecs por calidad internamente**, así que hay que
   *quitar* los de bitrate alto de la lista, no reordenarlos:
   `aptX HD > LDAC > aptX > AAC > SBC-XQ > SBC`.

   ```conf
   monitor.bluez.properties = {
     bluez5.codecs = [ sbc ]
     bluez5.enable-sbc-xq = false
   }
   ```

   *Nota:* esta lista es **global** (el códec no se puede fijar por dispositivo),
   así que afecta a todos los receptores, incluidos los Bose QC45. Para devolver
   calidad a otros dispositivos con el AD18 estable, la alternativa es
   `bluez5.codecs = [ sbc aac ]` (el AD18 no negocia AAC, así que se queda en SBC).

2. **Nunca suspender el nodo A2DP.** El AD18 no contesta al `Suspend` de AVDTP
   (`No reply to Suspend request`), y tras el timeout el transporte se desmonta.

   ```conf
   monitor.bluez.rules = [
     {
       matches = [ { node.name = "~bluez_output.*" } ]
       actions = { update-props = {
         session.suspend-timeout-seconds = 0
         node.pause-on-idle = false
       } }
     }
   ]
   ```

El fichero está en el repo; para aplicarlo: `systemctl --user restart wireplumber`.
(`wm-restart` no basta: hay que recrear el nodo.) El reinicio tira la conexión BT,
hay que volver a emparejar/conectar desde la GUI.

## Playbook de diagnóstico (por si vuelve)

```bash
# 1. ¿Se está cortando? Fallos de transporte en ventana de tiempo:
journalctl --user -u wireplumber --since "20 min ago" | grep -c "terminated unexpectedly"
journalctl -u bluetooth --since "20 min ago" | grep -E "fd\([0-9]+\) ready|Host is down|Suspend"

# 2. Códec y estado del nodo BT:
pw-dump | python3 -c '
import json,sys
for o in json.load(sys.stdin):
    p=o.get("info",{}).get("props",{})
    if p.get("node.name","").startswith("bluez_output"):
        print(p["node.name"], p.get("api.bluez5.codec"), o["info"]["state"])'

# 3. Captura HCI (necesita root; el exec_run del daemon con sudo=true):
timeout 80 btmon -w /tmp/bt.snoop
# ...y para releerla:
btmon -r /tmp/bt.snoop | grep -A5 "Disconnect Complete"     # ← la razón del corte

# 4. Calidad del enlace (RSSI/TX power que consulta blueman cada segundo):
timeout 12 btmon -K | grep -A6 "Read RSSI"
```

Lectura de la razón de desconexión:

| Reason | Significado |
|---|---|
| `Remote User Terminated Connection (0x13)` | **Corta el dispositivo remoto** (aquí: desensibilización/interferencia). |
| `Connection Timeout (0x08)` | Se perdió el enlace (RF, fuera de alcance, *supervision timeout*). |
| `Connection Terminated By Local Host (0x16)` | Corta el host (p. ej. perfil/BlueZ). |

## Gotchas descubiertos

- **`bluetoothctl scan on` + `connect` en la misma pasada → `br-connection-page-timeout`.**
  Mientras el controlador escanea no puede hacer *paging*. Para conectar, no
  escanear (usar la GUI de blueman, o `bluetoothctl connect <MAC>` a secas).
  Esto me hizo perder varias rondas pensando que el ampli rechazaba la conexión.
- **`btmon -a` NO es "analyze the current capture"**: `-a/--analyze` toma un
  fichero como argumento. Para releer una captura es `btmon -r <file>`.
- **`btmon` necesita root** (CAP_NET_ADMIN): `btmon: Failed to bind channel:
  Operation not permitted`. Usar `exec_run(sudo=true)`.
- **`btmon` no vuelca los payloads A2DP** salvo que se pida: existe `-A/--a2dp`
  ("Dump A2DP stream traffic"). Aun así, con `-A` en la captura no apareció
  disección AVDTP: para eso, mejor depurar `bluetoothd` (`-d`).
- **`pkill -f 'patrón'` se mata a sí mismo** si el patrón aparece en su propia
  línea de comando (el shell la incluye). Usar `pkill -f '[p]atrón'`.
- En una tubería, `VAR=x cmd1 | cmd2` **solo** define `VAR` para `cmd1`. Para
  pasar datos a python en un pipeline, usar `sys.argv`, no `os.environ`.
- **blueman-manager abre/cierra un socket HCI RAW cada segundo** (polling de RSSI)
  y lanza ciclos de *discovery* BLE periódicos mientras la ventana está abierta.
  No fue el culpable aquí, pero es actividad de radio extra sobre el mismo chip
  compartido: si aparecen microcortes, cerrarlo es un buen descarte.
- El RSSI que reporta el MT7922 en enlaces BR/EDR sale **absurdamente alto**
  (`-2 / 0 dBm`, imposible a cualquier distancia razonable). Es un *quirk* del
  driver: **no usar el RSSI de MediaTek para juzgar la calidad del enlace**.

## Plan B: USB

El AD18 es un DAC USB real y ya está registrado como tarjeta de audio
(`alsa_card.usb-SMSL_AUDIO_SMSL_AD18_AMP-00`, de hecho es el
`default-profile`). Por cable: cero cortes, cero interferencias y mejor calidad
que cualquier códec A2DP. Si el BT vuelve a dar guerra y no apetece pelearse, es
la salida rápida y definitiva.
