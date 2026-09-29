# nostr-relay-khatru

[![CI](https://github.com/rzazo24/nostr-relay-khatru/actions/workflows/ci.yml/badge.svg)](https://github.com/rzazo24/nostr-relay-khatru/actions/workflows/ci.yml)

*[Read in English](README.md)*

Un relé [Nostr](https://nostr.com/) pequeño y de propósito general, escrito en Go
con [khatru](https://github.com/fiatjaf/khatru) y almacenamiento SQLite. Es abierto
para leer y escribir —sin cuentas ni lista blanca—, con límites para que un solo
cliente no pueda llenar el disco ni tumbarlo.

Nació de [hivescope-relay](https://github.com/rzazo24/hivescope-relay), un relé de
un solo propósito para un chat vinculado a cuentas de Hive (ya discontinuado),
conservando lo que servía (límites de velocidad, SQLite, Docker, backup) y quitando
todo lo específico de aquel chat.

## Qué hace

- Habla el protocolo básico y guarda los eventos en SQLite: NIP-01 (eventos, filtros,
  suscripciones), NIP-09 (borrados), NIP-11 (documento de información del relé, con los
  límites configurados), NIP-40 (caducidad: khatru elimina los eventos caducados y este
  relé rechaza los que ya llegan caducados) y NIP-45 (`COUNT`).
- Acepta cualquier kind por defecto; se puede restringir con `RELAY_ALLOWED_KINDS`.
- **Límites por evento**: longitud del contenido (en caracteres, no bytes), número de
  tags, tamaño de cada valor de tag y cuánto puede adelantarse `created_at`. Los
  eventos antiguos se aceptan a propósito, para poder copiar historial.
- **Límite por consulta**: el `limit` de cada filtro se acota a `RELAY_MAX_LIMIT`
  (500 por defecto). Ver [el límite oculto de sqlite](#el-límite-oculto-de-consultas-de-sqlite).
- **Límites de velocidad por IP** para eventos, suscripciones (REQ) y conexiones, usando
  la IP real del cliente de `X-Forwarded-For` (que pone Caddy).
- Docker Compose con Caddy para TLS automático, healthcheck de Docker autocontenido,
  script de backup seguro de SQLite y un script opcional de autorrecuperación.

No incluye, a propósito: autenticación (NIP-42), acceso de pago, listas blancas,
búsqueda (NIP-50) ni panel web. Es un relé pequeño; si necesitas eso, mira los
[ejemplos de khatru](https://github.com/fiatjaf/khatru): este repo es un buen punto de partida.

## Cómo ejecutarlo

### Con Docker (relé + Caddy con TLS)

```bash
cp .env.example .env      # pon RELAY_DOMAIN y lo que quieras cambiar
docker compose up -d --build
```

Apunta antes el DNS de tu dominio al servidor; Caddy obtiene el certificado solo. El
relé no publica ningún puerto en el host: solo Caddy (80/443) queda expuesto. Tu relé
queda en `wss://<RELAY_DOMAIN>`.

### En local, sin Docker

```bash
CGO_ENABLED=1 go run .    # escucha en :3334, datos en ./data/relay.sqlite
```

Hacen falta `CGO_ENABLED=1` y un compilador de C: el driver de SQLite
([mattn/go-sqlite3](https://github.com/mattn/go-sqlite3)) usa cgo.

## Configuración

Todo son variables de entorno (ver [`.env.example`](.env.example)); todas son
opcionales salvo `RELAY_DOMAIN` con Docker.

| Variable | Por defecto | Significado |
|---|---|---|
| `RELAY_LISTEN_ADDR` | `:3334` | Dirección en la que escucha |
| `RELAY_DB_PATH` | `./data/relay.sqlite` | Archivo SQLite |
| `RELAY_NAME`, `RELAY_DESCRIPTION`, `RELAY_PUBKEY`, `RELAY_CONTACT` | | Documento NIP-11 |
| `RELAY_MAX_CONTENT_LENGTH` | `65536` | Caracteres del `content` |
| `RELAY_MAX_EVENT_TAGS` | `2000` | Tags por evento |
| `RELAY_MAX_TAG_VALUE_BYTES` | `1024` | Bytes de cada elemento de un tag |
| `RELAY_MAX_FUTURE_SKEW_SECONDS` | `900` | Cuánto puede adelantarse `created_at` (`0` = sin límite) |
| `RELAY_ALLOWED_KINDS` | *(todos)* | Kinds separados por comas; el kind 5 (borrados) siempre se acepta |
| `RELAY_MAX_LIMIT` | `500` | Máximo de eventos por filtro |
| `RELAY_EVENTS_PER_MINUTE` / `_BURST` | `30` / `60` | Eventos por IP |
| `RELAY_REQS_PER_MINUTE` / `_BURST` | `60` / `180` | Suscripciones por IP |
| `RELAY_CONNS_PER_MINUTE` / `_BURST` | `20` / `60` | Conexiones por IP |

Un valor inválido detiene el relé al arrancar con un error que nombra la variable.

## Operación

- **Backups**: `./scripts/backup-db.sh` hace una copia consistente con `sqlite3 .backup`
  (segura mientras el relé escribe) en `~/backups/nostr-relay-khatru`, conservando 14
  días. Prográmalo tú en cron, por ejemplo `17 3 * * * /ruta/a/scripts/backup-db.sh`.
- **Autorrecuperación (opcional)**: `./scripts/healthcheck.sh` reinicia los contenedores
  tras fallos repetidos. No se instala solo; lee antes su cabecera.
- **Logs**: `docker compose logs -f relay`.

## El límite oculto de consultas de sqlite

El backend `eventstore/sqlite3` acota en silencio cada consulta a 100 eventos (y a 10
valores por tag) si no se configura: un cliente que pide `limit: 500` recibe 100 sin
ningún error. `main.go` alinea el `QueryLimit` del backend con `RELAY_MAX_LIMIT` y sube
el tope de valores de tag, y la prueba de humo comprueba que una consulta con
`limit: 500` devuelve de verdad 130 eventos. Tenlo presente si cambias el backend de
almacenamiento.

## Desarrollo

```bash
CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go vet ./... && CGO_ENABLED=1 go test ./...

# prueba de humo extremo a extremo contra un relé en marcha (Node + nostr-tools). La de
# volumen publica 130 eventos, así que arranca el relé con límites de velocidad altos:
RELAY_EVENTS_PER_MINUTE=1000 RELAY_EVENTS_BURST=1000 RELAY_REQS_PER_MINUTE=1000 RELAY_REQS_BURST=1000 go run . &
cd test && npm install && RELAY_URL=ws://localhost:3334 npm test
```

Ver [`CLAUDE.md`](CLAUDE.md) para cómo encaja el código.

## Licencia

[MIT](LICENSE)
