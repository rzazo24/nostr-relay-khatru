#!/usr/bin/env bash
# Restaura la base de datos del relé desde un backup hecho con backup-db.sh.
#
# Uso:
#   docker compose stop relay                              # el relé tiene que estar parado
#   ./scripts/restore-db.sh ~/backups/nostr-relay-khatru/nostr-relay-khatru-AAAAMMDDTHHMMSSZ.sqlite.gz
#   docker compose up -d relay
#
# Qué hace, por orden (y se detiene en cuanto algo no cuadra):
#   1. Comprueba que el backup se descomprime y que SQLite lo da por íntegro (integrity_check).
#   2. Se niega si hay algún contenedor usando el volumen (restaurar con el relé escribiendo
#      corrompería la base).
#   3. Guarda una copia de seguridad de la base ACTUAL (pre-restore-...) por si te equivocas de backup.
#   4. Copia el backup al volumen y borra los archivos -wal/-shm viejos.
# Pide que escribas «restaurar» para confirmar, salvo con --yes.
#
# Variables de entorno opcionales:
#   BACKUP_DIR   dónde está/se guardan los backups (default: ~/backups/nostr-relay-khatru)
#   VOLUME_NAME  volumen Docker de destino (default: nostr-relay-khatru_relay-data)
#   DB_FILENAME  nombre del archivo dentro del volumen (default: relay.sqlite)
#
# Para ENSAYAR una restauración sin tocar producción usa un volumen de pruebas:
#   VOLUME_NAME=restore-test ./scripts/restore-db.sh <backup.gz> --yes

set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-$HOME/backups/nostr-relay-khatru}"
VOLUME_NAME="${VOLUME_NAME:-nostr-relay-khatru_relay-data}"
DB_FILENAME="${DB_FILENAME:-relay.sqlite}"

usage() { sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'; }

FILE=""; YES=0
for arg in "$@"; do
  case "$arg" in
    -h|--help) usage; exit 0 ;;
    --yes) YES=1 ;;
    -*) echo "opción desconocida: $arg" >&2; usage >&2; exit 2 ;;
    *) FILE="$arg" ;;
  esac
done
[ -n "$FILE" ] || { usage >&2; exit 2; }
[ -f "$FILE" ] || { echo "no existe el archivo: $FILE" >&2; exit 1; }
FILE="$(cd "$(dirname "$FILE")" && pwd)/$(basename "$FILE")"
mkdir -p "$BACKUP_DIR"

sqlite_in_alpine() { # $@ = montajes extra; el comando va por stdin
  docker run --rm -i "$@" alpine:3.20 sh -c 'apk add --no-cache sqlite >/dev/null 2>&1 && sh'
}

echo "[1/4] comprobando el backup: $FILE"
gzip -t "$FILE" || { echo "el archivo no es un gzip válido" >&2; exit 1; }
CHECK="$(sqlite_in_alpine -v "$FILE:/in.gz:ro" <<'SH'
gunzip -c /in.gz > /tmp/check.sqlite
echo "integridad: $(sqlite3 /tmp/check.sqlite 'pragma integrity_check')"
echo "eventos: $(sqlite3 /tmp/check.sqlite 'select count(*) from event')"
SH
)"
echo "$CHECK" | sed 's/^/      /'
echo "$CHECK" | grep -q '^integridad: ok$' || { echo "el backup NO pasa la comprobación de integridad: no se restaura" >&2; exit 1; }

echo "[2/4] comprobando que el relé no está usando el volumen $VOLUME_NAME"
if [ -n "$(docker ps -q --filter "volume=$VOLUME_NAME")" ]; then
  echo "hay contenedores usando el volumen. Páralos antes: docker compose stop relay" >&2
  exit 1
fi

CURRENT="$(sqlite_in_alpine -v "$VOLUME_NAME:/data:ro" <<SH || true
[ -f /data/$DB_FILENAME ] && echo "eventos en la base actual: \$(sqlite3 /data/$DB_FILENAME 'select count(*) from event')" || echo "el volumen no tiene base de datos todavía"
SH
)"
echo "      $CURRENT"

if [ "$YES" != 1 ]; then
  echo
  echo "Se va a SUSTITUIR la base de datos del volumen $VOLUME_NAME por este backup."
  read -r -p "Escribe «restaurar» para continuar: " answer
  [ "$answer" = "restaurar" ] || { echo "cancelado"; exit 1; }
fi

echo "[3/4] guardando una copia de la base actual (por si te equivocas de backup)"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
sqlite_in_alpine -v "$VOLUME_NAME:/data:ro" -v "$BACKUP_DIR:/backup" <<SH
if [ -f /data/$DB_FILENAME ]; then
  sqlite3 /data/$DB_FILENAME ".backup '/backup/pre-restore-$TS.sqlite'" && gzip /backup/pre-restore-$TS.sqlite && echo "      guardada: pre-restore-$TS.sqlite.gz"
else
  echo "      (no había base actual)"
fi
SH

echo "[4/4] restaurando"
docker run --rm -v "$VOLUME_NAME:/data" -v "$FILE:/in.gz:ro" alpine:3.20 sh -c "
  set -e
  gunzip -c /in.gz > /data/$DB_FILENAME.restoring
  rm -f /data/$DB_FILENAME-wal /data/$DB_FILENAME-shm
  mv /data/$DB_FILENAME.restoring /data/$DB_FILENAME
"
echo "hecho. Arranca el relé: docker compose up -d relay"
