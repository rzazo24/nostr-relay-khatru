#!/usr/bin/env bash
# Chequeo de salud opcional en el propio servidor: pide por HTTPS el documento
# NIP-11 del relé y, si falla varias veces seguidas, reinicia el contenedor de
# Caddy o el del relé. Solo intenta recuperarse solo y deja constancia en el log:
# no avisa a nadie ni sirve si cae la máquina entera.
#
# NO se instala solo. Si lo quieres, añade a tu cron (por ejemplo cada 2 minutos):
#   */2 * * * * RELAY_URL=https://relay.example.com /ruta/a/scripts/healthcheck.sh
#
# Variables de entorno:
#   RELAY_URL         (obligatoria) URL pública https del relé
#   FAIL_THRESHOLD    fallos seguidos antes de reiniciar (default: 2)
#   COOLDOWN_SECONDS  espera mínima entre reinicios (default: 600)
#   PING_URL          (opcional) URL a la que se avisa cuando el relé responde bien: un servicio de
#                     monitorización tipo healthchecks.io la espera cada pocos minutos y te avisa si
#                     DEJA de llegar (servidor caído, relé caído, cron parado...).
#   PING_FAIL_URL     (opcional) URL a la que se avisa al detectar un fallo, para que te avise antes
#                     de que venza el plazo (en healthchecks.io es la URL de ping + /fail).
#   STATE_DIR, LOG_FILE, COMPOSE_DIR, RESTART_CMD (default: "docker compose restart")

set -uo pipefail

RELAY_URL="${RELAY_URL:?define RELAY_URL (por ejemplo https://relay.example.com)}"
FAIL_THRESHOLD="${FAIL_THRESHOLD:-2}"
COOLDOWN_SECONDS="${COOLDOWN_SECONDS:-600}"
STATE_DIR="${STATE_DIR:-$HOME/.local/state/nostr-relay-khatru-health}"
LOG_FILE="${LOG_FILE:-$HOME/backups/nostr-relay-khatru/healthcheck.log}"
COMPOSE_DIR="${COMPOSE_DIR:-$(cd "$(dirname "$0")/.." && pwd)}"
RESTART_CMD="${RESTART_CMD:-docker compose restart}"
PING_URL="${PING_URL:-}"
PING_FAIL_URL="${PING_FAIL_URL:-}"

mkdir -p "$STATE_DIR" "$(dirname "$LOG_FILE")"

# Un solo chequeo a la vez.
exec 9>"$STATE_DIR/lock"
flock -n 9 || exit 0

# El log no crece sin límite: pasado 1 MB se queda con la parte final.
if [ -f "$LOG_FILE" ] && [ "$(stat -c %s "$LOG_FILE")" -gt 1048576 ]; then
  tail -n 2000 "$LOG_FILE" > "$LOG_FILE.tmp" && mv "$LOG_FILE.tmp" "$LOG_FILE"
fi
log() { echo "$(date -u +%FT%TZ) $*" >> "$LOG_FILE"; }

# Avisos a un monitor externo. Un fallo al avisar no debe romper el chequeo: solo se apunta en el log.
ping() {
  [ -n "$1" ] || return 0
  curl -fsS -m 10 --retry 2 -o /dev/null "$1" 2>/dev/null || log "no se pudo avisar al monitor externo"
}

fails="$(cat "$STATE_DIR/fails" 2>/dev/null || echo 0)"

if curl -fsS -o /dev/null --max-time 10 -H 'Accept: application/nostr+json' "$RELAY_URL" 2>/dev/null; then
  [ "$fails" -gt 0 ] && log "recuperado tras $fails fallo(s)"
  echo 0 > "$STATE_DIR/fails"
  ping "$PING_URL"
  exit 0
fi

fails=$((fails + 1))
echo "$fails" > "$STATE_DIR/fails"
log "el relé NO responde ($fails/$FAIL_THRESHOLD)"
ping "$PING_FAIL_URL"
[ "$fails" -ge "$FAIL_THRESHOLD" ] || exit 0

now="$(date +%s)"
last="$(cat "$STATE_DIR/last-restart" 2>/dev/null || echo 0)"
if [ $((now - last)) -lt "$COOLDOWN_SECONDS" ]; then
  log "reinicio omitido (cooldown, el último fue hace $((now - last))s)"
  exit 0
fi

echo "$now" > "$STATE_DIR/last-restart"
echo 0 > "$STATE_DIR/fails"
# Si cae el relé, Caddy (que hace de proxy) también deja de contestar: se reinician los dos.
log "reiniciando relay y caddy"
(cd "$COMPOSE_DIR" && $RESTART_CMD relay caddy) >> "$LOG_FILE" 2>&1 || log "el reinicio falló"
exit 0
