#!/usr/bin/env bash
# pangolin-ssh-alias.sh — redirige las sesiones de las cuentas provisionadas por
# Pangolin (p-manu343726*: el sufijo numerico es aleatorio) a la cuenta real
# manu343726, en la VM homelab.
#
# Fuente: .docs/pangolin-ssh-alias.sh   ·   Doc: .docs/pangolin-ssh-alias.md
# Instalado en la VM como: /usr/local/sbin/pangolin-ssh-alias.sh
#
# Idempotente: instala/actualiza el drop-in de /etc/profile.d y limpia bloques
# viejos (v1) si los hubiera en los homes de las cuentas p-*.
set -euo pipefail

DROPIN=/etc/profile.d/99-pangolin-alias.sh

cat > "$DROPIN" <<'EOF'
# Pangolin SSH alias (ver .docs/pangolin-ssh-alias.md en los dotfiles).
# El provisioning JIT de Pangolin crea la cuenta con prefijo "p-" hardcodeado y
# sufijo numerico aleatorio si el nombre choca (p-manu343726, p-manu34372631...).
# Este drop-in redirige las sesiones interactivas de esas cuentas a la cuenta
# real manu343726 (uid 1000, su $HOME y dotfiles) via sudo NOPASSWD.
# Solo actua con tty y con nombres p-manu343726*; inerte para todo lo demas.
if [ -t 0 ]; then
    case "$(id -un)" in
        p-manu343726*) exec sudo -u manu343726 -i ;;
    esac
fi
EOF
chmod 644 "$DROPIN"
echo "OK: $DROPIN"

# Limpieza de bloques v1 (prefijados a mano en .bash_profile/.bashrc).
shopt -s nullglob
for H in /home/p-manu343726*; do
    [ -d "$H" ] || continue
    for f in "$H/.bash_profile" "$H/.bashrc"; do
        [ -f "$f" ] || continue
        if grep -Fq '# --- Pangolin alias:' "$f" || grep -Fq '# --- Pangolin SSH alias' "$f"; then
            tmp=$(mktemp)
            sed '/^# --- Pangolin.*---$/,/^# --- fin bloque alias ---$/d' "$f" > "$tmp"
            cat "$tmp" > "$f"   # redireccion sobre el mismo inodo: conserva owner/modo
            rm -f "$tmp"
            echo "limpio bloque viejo en $f"
        fi
    done
done

echo "--- drop-in instalado ---"
cat "$DROPIN"
