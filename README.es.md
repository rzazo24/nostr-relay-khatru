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

**Instancia pública:** `wss://relay.hivescope.xyz`: abierta para leer y escribir, con los
límites de velocidad descritos más abajo. Trátala con cariño; si se abusa de ella, se endurecerá.

## Qué hace

- Habla el protocolo y guarda los eventos en SQLite. NIPs soportados (y el documento NIP-11
  del relé anuncia exactamente estos, ni uno más): **1** (eventos, filtros, suscripciones),
  **9** (borrados), **11** (documento de información), **13** (prueba de trabajo, solo si se
  activa), **40** (caducidad), **42** (autenticación), **45** (`COUNT`), **70** (eventos
  protegidos) y **77** (sincronización Negentropy), más el **86** (API de gestión) cuando hay
  un dueño configurado.
- Acepta cualquier kind por defecto; se puede restringir con `RELAY_ALLOWED_KINDS`.
- **Límites por evento**: longitud del contenido (en caracteres, no bytes), número de
  tags, tamaño de cada valor de tag y cuánto puede adelantarse `created_at`. Los
  eventos antiguos se aceptan a propósito, para poder copiar historial.
- **Límite por consulta**: el `limit` de cada filtro se acota a `RELAY_MAX_LIMIT`
  (500 por defecto). Ver [las advertencias](#advertencias-que-conviene-conocer).
- **Límites de velocidad por IP** para eventos, suscripciones (REQ) y conexiones, usando
  la IP real del cliente de `X-Forwarded-For` (que pone Caddy).
- Docker Compose con Caddy para TLS automático, healthcheck de Docker autocontenido,
  script de backup seguro de SQLite y un script opcional de autorrecuperación.

### NIP-86: API de gestión (moderar sin redesplegar)

Pon en `RELAY_PUBKEY` tu clave pública (hex) y usa cualquier cliente NIP-86 (firmando con
esa clave, con autenticación HTTP NIP-98) para cambiar estas listas al vuelo: se guardan en
el mismo SQLite y sobreviven a los reinicios.

- **Pubkeys**: `banpubkey` / `allowpubkey`. Con al menos un pubkey permitido, el relé pasa a
  ser de **escritura restringida**: solo publican los permitidos (leer sigue abierto) y NIP-11
  dice `restricted_writes: true`. Banear saca de los permitidos, y al revés.
- **Eventos**: `banevent` borra el evento si está guardado y lo rechaza a partir de entonces.
- **Kinds**: `allowkind` / `disallowkind` (con lista de permitidos no vacía, solo pasan esos).
- **IPs**: `blockip` / `unblockip`.
- **Información**: `changerelayname`, `changerelaydescription`, `changerelayicon`.

Solo la puede usar el dueño; sin `RELAY_PUBKEY` la API está desactivada. Para que NIP-98
verifique la URL detrás de un proxy se usan las cabeceras `X-Forwarded-*` de Caddy, o
define `RELAY_PUBLIC_URL`.

### NIP-13: prueba de trabajo (antispam)

`RELAY_MIN_POW=N` exige que el id de cada evento empiece por al menos `N` bits a cero **y**
que se comprometa a ese objetivo en su tag `nonce` (así no pasan los ids con suerte sin
minado real). Los borrados (kind 5) están exentos. Desactivado por defecto; con ~20 bits a un
cliente le cuesta un segundo de CPU por evento, nada para una persona y mucho para un bot de spam.

### NIP-42: autenticación y mensajes privados

- El relé ofrece un desafío `AUTH` nada más conectar.
- **Kinds privados** (`RELAY_PRIVATE_KINDS`, por defecto `4` y `1059`: DMs clásicos y "gift
  wraps" de NIP-17/59): solo los ven su autor y su destinatario (tag `p`), autenticados. Una
  consulta dirigida solo a esos kinds recibe `auth-required:` para que el cliente se autentique y
  repita; una consulta genérica (sin kinds) se sirve **sin** ellos, y las suscripciones en vivo nunca
  los reciben. Publicarlos no exige nada especial (los gift wraps salen de claves desechables).
- `RELAY_AUTH_REQUIRED=true` hace obligatoria la autenticación para leer y escribir.
- Los eventos protegidos de NIP-70 (tag `["-"]`) solo los puede publicar su autor, autenticado.

### NIP-77: sincronización Negentropy

Clientes y otros relés pueden reconciliar sus eventos con este sin descargarlo todo. Una sesión
de sincronización no se limita por `RELAY_MAX_LIMIT` (quedaría incompleta en silencio) sino por
`RELAY_MAX_NEGENTROPY_EVENTS` (100 000 por defecto).

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
| `RELAY_NAME`, `RELAY_DESCRIPTION`, `RELAY_CONTACT` | | Documento NIP-11 |
| `RELAY_PUBKEY` | | Clave pública del dueño (hex): `pubkey` de NIP-11 y la única que puede usar NIP-86 |
| `RELAY_PUBLIC_URL` | *(se deduce)* | URL pública https, para NIP-42/NIP-98 |
| `RELAY_ICON` | | Icono del relé para NIP-11: una URL absoluta, o una ruta como `/icon.png` (que Caddy sirve desde `./static`) |
| `RELAY_MAX_CONTENT_LENGTH` | `65536` | Caracteres del `content` |
| `RELAY_MAX_EVENT_TAGS` | `2000` | Tags por evento |
| `RELAY_MAX_TAG_VALUE_BYTES` | `1024` | Bytes de cada elemento de un tag |
| `RELAY_MAX_FUTURE_SKEW_SECONDS` | `900` | Cuánto puede adelantarse `created_at` (`0` = sin límite) |
| `RELAY_ALLOWED_KINDS` | *(todos)* | Kinds separados por comas; el kind 5 (borrados) siempre se acepta |
| `RELAY_MAX_LIMIT` | `500` | Máximo de eventos por filtro |
| `RELAY_MAX_NEGENTROPY_EVENTS` | `100000` | Máximo de eventos que ofrece una sesión NIP-77 |
| `RELAY_MIN_POW` | `0` | Dificultad mínima NIP-13 en bits (`0` = desactivado) |
| `RELAY_AUTH_REQUIRED` | `false` | Exigir autenticación NIP-42 para leer y escribir |
| `RELAY_PRIVATE_KINDS` | `4,1059` | Kinds que solo ven su autor y su destinatario (`none` = desactivado) |
| `RELAY_EVENTS_PER_MINUTE` / `_BURST` | `30` / `60` | Eventos por IP |
| `RELAY_REQS_PER_MINUTE` / `_BURST` | `60` / `180` | Suscripciones por IP |
| `RELAY_CONNS_PER_MINUTE` / `_BURST` | `20` / `60` | Conexiones por IP |

Un valor inválido detiene el relé al arrancar con un error que nombra la variable.

## Página de presentación

Al abrir la URL del relé en un navegador se ve una pequeña página de presentación (`static/index.html`, `landing.css`, `landing.js`; sin dependencias externas): nombre, icono y descripción del relé, su dirección `wss://` con un botón de copiar, los NIPs soportados, los límites y las reglas, en español e inglés. Se rellena sola con el documento NIP-11 del propio relé, así que siempre está al día. Caddy solo la sirve a los `GET` normales de `/`: los clientes de Nostr (`Accept: application/nostr+json`), los WebSockets y los `POST` de NIP-86 siguen llegando al relé sin tocar.

Para que la descripción sea bilingüe, escríbela como `English text | Texto en español` en `RELAY_DESCRIPTION`: todos los clientes muestran las dos, y la página de presentación enseña la del idioma elegido.

## Icono del relé

Los clientes muestran el `icon` de NIP-11. Pon una imagen cuadrada (PNG/JPG/WebP, ~512×512, ligera) en `static/`, define `RELAY_ICON=/icon.png` y Caddy la sirve en `https://<RELAY_DOMAIN>/icon.png` (`static/icon.png` y `static/icon.svg` son los de la instancia pública). Mejor PNG: muchas apps nativas no renderizan SVG. `changerelayicon` de NIP-86 lo cambia en caliente.

## Operación

- **Backups**: `./scripts/backup-db.sh` hace una copia consistente con `sqlite3 .backup`
  (segura mientras el relé escribe) en `~/backups/nostr-relay-khatru`, conservando 14
  días. Prográmalo tú en cron, por ejemplo `17 3 * * * /ruta/a/scripts/backup-db.sh`.
- **Autorrecuperación (opcional)**: `./scripts/healthcheck.sh` reinicia los contenedores
  tras fallos repetidos. No se instala solo; lee antes su cabecera.
- **Avisos externos (opcional)**: el script también puede avisar a un servicio de monitorización: define `PING_URL` (se llama cada vez que el relé responde; un servicio como healthchecks.io te escribe cuando los avisos *dejan de llegar*, lo que cubre también que caiga el servidor entero) y, si quieres, `PING_FAIL_URL` (se llama al detectar un fallo, para avisar antes). Un monitor inalcanzable nunca rompe el chequeo. O apunta un monitor HTTP externo (UptimeRobot…) a `https://<RELAY_DOMAIN>/`, que responde 200.
- **Logs**: `docker compose logs -f relay`. El relé registra lo que *rechaza* (`reject event kind=1 pubkey=ab12cd34 reason="rate-limited: …"`, como mucho 5 líneas por motivo y minuto) y, si hubo actividad, un resumen de una línea por minuto (`stats … saved=12 rejected=3 [rate-limited=3]`). Nunca registra contenido ni IPs, solo el kind, los 8 primeros caracteres del pubkey y el motivo: útil para ver por qué se rechaza a un cliente (por ejemplo Damus).

## Advertencias que conviene conocer

- **El límite oculto de consultas de sqlite.** El backend `eventstore/sqlite3` acota en silencio
  cada consulta a 100 eventos (y a 10 valores por tag) si no se configura: un cliente que pide
  `limit: 500` recibe 100 sin ningún error. El servidor sube el tope del backend y aplica él los
  límites reales (`RELAY_MAX_LIMIT`, o `RELAY_MAX_NEGENTROPY_EVENTS` en las sesiones de
  sincronización); la prueba de humo comprueba que `limit: 500` devuelve de verdad 130 eventos.
- **go-nostr debe ser ≥ 0.52.** khatru v0.19.1 depende de go-nostr v0.51.8, cuyo analizador de
  mensajes NIP-77 está roto (nunca reconoce `NEG-OPEN`, así que la sincronización se cuelga). `go.mod`
  fija la v0.52.1, donde funciona; no la bajes. Una prueba de integración sincroniza 350 eventos para
  detectarlo.

## Desarrollo

```bash
CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go vet ./... && CGO_ENABLED=1 go test ./...
# internal/server tiene pruebas de integración que arrancan un relé real (NIP-13/42/70/77/86)

# prueba de humo extremo a extremo contra un relé en marcha (Node + nostr-tools). La de
# volumen publica 130 eventos, así que arranca el relé con límites de velocidad altos:
RELAY_EVENTS_PER_MINUTE=1000 RELAY_EVENTS_BURST=1000 RELAY_REQS_PER_MINUTE=1000 RELAY_REQS_BURST=1000 go run . &
cd test && npm install && RELAY_URL=ws://localhost:3334 npm test
```

Ver [`CLAUDE.md`](CLAUDE.md) para cómo encaja el código.

## Licencia

[MIT](LICENSE)
