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
#   PING_FAIL_URL     (opcional) URL a la que se avisa de un fallo, para que te avise antes de que venza el
#                     plazo (en healthchecks.io es la URL de ping + /fail). Solo se usa a partir de
#                     ALERT_THRESHOLD fallos seguidos: un tropiezo aislado no manda ningún aviso.
#   ALERT_THRESHOLD   fallos seguidos antes de avisar a PING_FAIL_URL (default: FAIL_THRESHOLD, o sea 2)
#   BUNKER_STATUS_CMD (opcional) orden que dice el estado de un firmador hivescope-bunker en esta máquina (por ejemplo
#                     /ruta/a/hivescope-bunker status). Si falla o dice LOCKED en ALERT_THRESHOLD chequeos seguidos, se avisa a
#                     PING_FAIL_URL con el motivo en vez de mandar el "todo bien": tras un reinicio el bunker arranca bloqueado y
#                     nada firma hasta desbloquearlo a mano. Al bunker no se le reinicia nunca (lo dejaría bloqueado otra vez).
#   STATE_DIR, LOG_FILE, COMPOSE_DIR, RESTART_CMD (default: "docker compose restart")

set -uo pipefail

RELAY_URL="${RELAY_URL:?define RELAY_URL (por ejemplo https://relay.example.com)}"
FAIL_THRESHOLD="${FAIL_THRESHOLD:-2}"
COOLDOWN_SECONDS="${COOLDOWN_SECONDS:-600}"
ALERT_THRESHOLD="${ALERT_THRESHOLD:-$FAIL_THRESHOLD}"
STATE_DIR="${STATE_DIR:-$HOME/.local/state/nostr-relay-khatru-health}"
LOG_FILE="${LOG_FILE:-$HOME/backups/nostr-relay-khatru/healthcheck.log}"
COMPOSE_DIR="${COMPOSE_DIR:-$(cd "$(dirname "$0")/.." && pwd)}"
RESTART_CMD="${RESTART_CMD:-docker compose restart}"
PING_URL="${PING_URL:-}"
PING_FAIL_URL="${PING_FAIL_URL:-}"
BUNKER_STATUS_CMD="${BUNKER_STATUS_CMD:-}"

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
# Un segundo argumento opcional va como texto del aviso (healthchecks.io lo incluye en la notificación).
ping() {
  [ -n "$1" ] || return 0
  if [ -n "${2:-}" ]; then curl -fsS -m 10 --retry 2 -o /dev/null --data-raw "$2" "$1" 2>/dev/null || log "no se pudo avisar al monitor externo"
  else curl -fsS -m 10 --retry 2 -o /dev/null "$1" 2>/dev/null || log "no se pudo avisar al monitor externo"; fi
}

# El firmador (si se vigila): ¿está en marcha y DESBLOQUEADO? Devuelve 0 si sí; si no, deja el motivo en $bunker_why.
bunker_ok() {
  [ -n "$BUNKER_STATUS_CMD" ] || return 0
  local out
  if ! out="$($BUNKER_STATUS_CMD 2>&1)"; then bunker_why="hivescope-bunker no responde: ${out:0:200}"; return 1; fi
  if echo "$out" | grep -q LOCKED; then bunker_why="hivescope-bunker: BLOQUEADO (tras un reinicio hay que desbloquearlo por SSH: hivescope-bunker unlock)"; return 1; fi
  return 0
}

fails="$(cat "$STATE_DIR/fails" 2>/dev/null || echo 0)"

if curl -fsS -o /dev/null --max-time 10 -H 'Accept: application/nostr+json' "$RELAY_URL" 2>/dev/null; then
  [ "$fails" -gt 0 ] && log "recuperado tras $fails fallo(s)"
  echo 0 > "$STATE_DIR/fails"
  bfails="$(cat "$STATE_DIR/bunker-fails" 2>/dev/null || echo 0)"
  bunker_why=""
  if bunker_ok; then
    [ "$bfails" -gt 0 ] && log "el bunker vuelve a estar bien tras $bfails chequeo(s)"
    echo 0 > "$STATE_DIR/bunker-fails"
    ping "$PING_URL"
  else
    bfails=$((bfails + 1)); echo "$bfails" > "$STATE_DIR/bunker-fails"
    log "$bunker_why ($bfails/$ALERT_THRESHOLD)"
    # el relé está bien, pero el "todo bien" no se manda: lo que se vigila es que todo funcione, firmar incluido
    if [ "$bfails" -ge "$ALERT_THRESHOLD" ]; then ping "$PING_FAIL_URL" "$bunker_why"; else ping "$PING_URL"; fi
  fi
  exit 0
fi

fails=$((fails + 1))
echo "$fails" > "$STATE_DIR/fails"
log "el relé NO responde ($fails/$FAIL_THRESHOLD)"
[ "$fails" -ge "$ALERT_THRESHOLD" ] && ping "$PING_FAIL_URL" "el relé no responde en $RELAY_URL ($fails chequeos seguidos)"
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
