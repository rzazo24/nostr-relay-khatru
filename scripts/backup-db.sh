#!/usr/bin/env bash
# Copia de seguridad de la base SQLite del relé.
#
# Usa el propio comando ".backup" de sqlite3 (no un "cp" del archivo), que es
# seguro incluso con el relé escribiendo en ese mismo momento: sqlite3 hace
# una copia consistente a nivel de página, no una foto congelada del sistema
# de archivos. Corre en un contenedor temporal de Alpine que monta el mismo
# volumen Docker que usa el relé (relay-data), en modo lectura.
#
# Uso:
#   ./scripts/backup-db.sh
#
# Variables de entorno opcionales:
#   BACKUP_DIR      dónde guardar los .sqlite.gz (default: ~/backups/nostr-relay-khatru)
#   RETENTION_DAYS  cuántos días de backups conservar (default: 14)
#   VOLUME_NAME     nombre del volumen Docker con la base (default: nostr-relay-khatru_relay-data)
#   DB_FILENAME     nombre del archivo .sqlite dentro del volumen

set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-$HOME/backups/nostr-relay-khatru}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"
VOLUME_NAME="${VOLUME_NAME:-nostr-relay-khatru_relay-data}"
DB_FILENAME="${DB_FILENAME:-relay.sqlite}"

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_FILE="nostr-relay-khatru-${TIMESTAMP}.sqlite"

mkdir -p "$BACKUP_DIR"

echo "[$(date -u +%FT%TZ)] iniciando backup de ${VOLUME_NAME}/${DB_FILENAME} -> ${BACKUP_DIR}/${OUT_FILE}.gz"

docker run --rm \
  -v "${VOLUME_NAME}:/data:ro" \
  -v "${BACKUP_DIR}:/backup" \
  alpine:3.20 \
  sh -c "apk add --no-cache sqlite >/dev/null && sqlite3 /data/${DB_FILENAME} \".backup '/backup/${OUT_FILE}'\""

gzip "${BACKUP_DIR}/${OUT_FILE}"

echo "[$(date -u +%FT%TZ)] backup ok: ${BACKUP_DIR}/${OUT_FILE}.gz ($(du -h "${BACKUP_DIR}/${OUT_FILE}.gz" | cut -f1))"

# limpieza: borra backups más viejos que RETENTION_DAYS
find "$BACKUP_DIR" -name 'nostr-relay-khatru-*.sqlite.gz' -mtime "+${RETENTION_DAYS}" -print -delete

echo "[$(date -u +%FT%TZ)] backups actuales:"
ls -lh "$BACKUP_DIR"
