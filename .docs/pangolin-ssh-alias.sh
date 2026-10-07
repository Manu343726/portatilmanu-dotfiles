#!/usr/bin/env bash
# pangolin-ssh-alias.sh — alias p-manu343726 -> manu343726 en la VM homelab.
#
# Fuente en los dotfiles: .docs/pangolin-ssh-alias.sh
# Doc completa (por qué, comportamiento, rollback): .docs/pangolin-ssh-alias.md
# Instalado en la VM como: /usr/local/sbin/pangolin-ssh-alias.sh
#
# Aplica el bloque de redireccion en los dotfiles de la cuenta creada por el
# Automated Provisioning de Pangolin (prefijo "p-" hardcodeado). Idempotente:
# si el bloque ya esta, no hace nada. Backups: <fichero>.bak
set -euo pipefail

H=/home/p-manu343726
U=p-manu343726
REAL=manu343726

read -r -d '' GUARD <<EOF || true
# --- Pangolin alias: redirige sesiones interactivas de p-manu343726 a manu343726 ---
# El provisioning JIT de Pangolin crea esta cuenta con prefijo "p-" hardcodeado
# (fosrl/pangolin signSshKey.ts), asi que las sesiones caen aqui y no en la
# cuenta real. Este bloque hace que el login interactivo salte a la cuenta real
# (uid 1000, \$HOME y dotfiles de manu343726) via sudo NOPASSWD. El auth-daemon
# (copySkelInto) solo anade ficheros que faltan y nunca sobreescribe este,
# asi que sobrevive a re-provisiones. Rollback: borrar este bloque.
if [ -t 0 ] && [ "\$(id -un)" = "$U" ]; then
    exec sudo -u $REAL -i
fi
# --- fin bloque alias ---
EOF

apply() {
  local f=$1
  if [ -f "$f" ] && grep -Fq 'exec sudo -u manu343726 -i' "$f"; then
    echo "SKIP (ya aplicado): $f"
    return 0
  fi
  cp -a "$f" "${f}.bak"
  local tmp
  tmp=$(mktemp)
  printf '%s\n\n' "$GUARD" > "$tmp"
  cat "${f}.bak" >> "$tmp"
  install -o "$U" -g "$U" -m 644 "$tmp" "$f"
  rm -f "$tmp"
  echo "OK: $f (backup en ${f}.bak)"
}

apply "$H/.bash_profile"
apply "$H/.bashrc"

echo "--- resultado .bash_profile ---"
cat "$H/.bash_profile"
echo "--- ownership ---"
ls -la "$H/.bash_profile" "$H/.bashrc"
