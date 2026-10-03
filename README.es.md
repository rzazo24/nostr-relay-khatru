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
| `RELAY_TAGS`, `RELAY_LANGUAGES`, `RELAY_POSTING_POLICY` | | NIP-11 `tags`, `language_tags` (separados por comas) y `posting_policy` (URL): lo que usan los directorios de relés para clasificarlo |
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

## Panel de control (solo para el dueño)

`https://<RELAY_DOMAIN>/admin` es un panel de control para el dueño del relé: eventos guardados (por tipo), claves distintas, tamaño de la base de datos, conexiones abiertas, una gráfica de actividad por minuto de la última hora, los rechazos (por motivo y los últimos) y los eventos más recientes con su contenido público recortado, las listas de moderación de un vistazo y la configuración actual. 
**Estado del servidor** (tarjetas): disco usado (en rojo desde el 85 %) y antigüedad y tamaño de la última copia de seguridad (en rojo si tiene más de 36 h o no hay). La tarjeta de la copia necesita `BACKUP_DIR` en `.env`, apuntando a la carpeta donde escribe `scripts/backup-db.sh`; compose la monta en solo lectura y define `RELAY_BACKUP_DIR`.

**Entrar con un firmador remoto (NIP-46)**: además de una extensión NIP-07, el inicio de sesión del panel acepta un firmador remoto como Clave en iOS —no hace falta extensión en el navegador, así que sirve en un móvil—. El panel muestra un enlace `nostrconnect://`, el firmador lo aprueba y firma el mismo evento de inicio de sesión NIP-98, y la clave no sale de él. El intercambio pasa por **este relé y por el propio de Clave (`wss://relay.powr.build`, indicado como `data-extra-relays` en `static/admin/index.html` y permitido en el `connect-src` del Caddyfile)** —Clave solo recibe peticiones en segundo plano por su relé— como eventos kind 24133 cifrados con NIP-44, así que el relé de terceros no ve nada legible; el panel abre Clave con `clave://connect?uri=…` (o copia el enlace `nostrconnect://` en Clave › Connect); como el móvil suspende la página mientras aprobas en la otra app, el relé guarda los eventos 24133 **10 minutos en memoria** (máximo 500, de 16 KB) y solo se los entrega a una consulta que nombre al destinatario con `#p`. Ese buzón hace además que el relé conteste `OK true` a los efímeros que nadie escuchaba, en vez de `mute: no one was listening`. La parte del navegador es un paquete pequeño de `nostr-tools` en `static/admin/vendor/` (se reconstruye con `scripts/build-admin-vendor.sh`).

**Copia de seguridad ahora** (enlace bajo las tarjetas): descarga a demanda una copia consistente `.sqlite.gz` de la base de datos (`VACUUM INTO` sobre una conexión de solo lectura aparte, así que es seguro mientras el relé escribe; mismo patrón de nombre que `scripts/backup-db.sh`, se restaura con `scripts/restore-db.sh`). Contiene todo lo que guarda el relé, incluidos los mensajes privados (cifrados): trátala como un secreto. Se arma en la memoria del navegador, así que está pensada para bases de datos de hasta unos cientos de MB; una a la vez; queda en el historial de acciones.

**Historial de acciones**: cada acción del dueño (baneos, vetos, reglas, cambios de información, inicios y cierres de sesión del panel y lo hecho por NIP-86) se guarda en la base de datos con su objetivo completo y tu nota; se conservan las últimas 1 000 y el panel enseña 100.

**Claves más ruidosas**: ranking de las claves con más *eventos* rechazados en las últimas 24 h (contadores en memoria, a cero al reiniciar, 1 000 claves como máximo), con la clave pública completa, el último tipo, el motivo más repetido y botones *Banear* / *Buscar* (tu propia clave no se puede banear). La **información del relé** de *Moderación* edita ahora también `contact`, `tags`, `language_tags` y `posting_policy` (lo que leen los directorios); los cambios mandan sobre el `.env` hasta que restauras.

**Búsqueda** (solo lectura): una caja que entiende lo que escribes —un `npub` o clave de 64 caracteres (los eventos de esa cuenta y un *resumen de la clave*: eventos guardados, primero y último, tipos, nombre del perfil, baneada o en lista blanca), un id de evento `note1…`/`nevent1…`/hex, los primeros 6 o más caracteres de una clave o id (las claves de 8 caracteres de *Rechazos* se pueden pulsar), un número pequeño (un tipo de evento) o cualquier texto (coincidencia sin distinguir mayúsculas en el contenido de los eventos públicos; los mensajes privados ni se buscan ni se enseñan)— con filtro opcional por tipo y un rango de fechas opcional (*Desde*/*Hasta*, días completos en tu hora local, con botones rápidos de hoy / 7 / 30 días; el resumen de la clave no se acota por fechas), páginas de 50 (*Cargar más*) y un total con tope de 10 000. Los resultados llevan los mismos botones *Vetar evento* / *Banear clave*, y el resumen de la clave permite banear o quitar el baneo.

**Ayuda integrada**: el botón *Ayuda* (y el `?` junto a cada sección) abre una explicación de cada número, gráfica, motivo de rechazo, lista de moderación y campo de configuración, con un glosario de términos de Nostr.

**Moderación desde el panel** (se aplica al momento y se guarda): banear o desbanear una clave (hex o `npub`, con opción de borrar sus eventos guardados), una lista blanca de escritura (con al menos una clave solo pueden publicar esas —y tú—; leer sigue siendo libre), vetar un evento por su id (`note1…`/`nevent1…`/hex: se borra y no vuelve a aceptarse), permitir o prohibir tipos de evento, bloquear IPs y cambiar el nombre, la descripción y el icono del relé (o restaurar los de la configuración). Junto a cada evento reciente hay botones rápidos de *Banear clave* y *Vetar evento*. El dueño nunca queda bloqueado por estas listas ni puede banearse a sí mismo. Cada acción se confirma en la página y deja una línea de auditoría en el log del relé (`admin action=… target=<8 primeros caracteres>`). Las mismas listas se pueden gestionar con NIP-86 desde cualquier cliente compatible.

- **Entrada**: firmas una petición NIP-98 con la clave de `RELAY_PUBKEY` usando una extensión NIP-07 del navegador (nos2x, Alby…). El servidor comprueba la firma (dueño, esta URL y método exactos, de menos de un minuto y sin haberse usado antes) y la cambia por una sesión de una hora en una cookie `HttpOnly`, `SameSite=Strict` y `Secure`. Los intentos de entrada tienen límite de velocidad. Sin `RELAY_PUBKEY` el panel está desactivado.
- **Privacidad**: nunca muestra direcciones IP ni el contenido de los mensajes privados (kinds 4, 13, 14, 1059). Los rechazos solo llevan los 8 primeros caracteres del pubkey. El histórico de actividad está solo en memoria (se pierde al reiniciar).
- **Seguridad**: la página tiene una política de contenido estricta (sin script ni estilo en línea) y pone todo lo que recibe como texto, nunca como HTML.

## Estadísticas de largo plazo

La gráfica de actividad tiene pestañas de **60 min / 24 h / 7 d / 30 d / 90 d**. La de 60 minutos es en directo y está en memoria; las largas se guardan en el propio archivo SQLite del relé como **contadores por hora** (`activity_hourly`: eventos guardados, efímeros y rechazados —en total y por motivo— y autenticaciones, más el máximo de conexiones abiertas, tamaño de la base de datos y eventos guardados vistos cada hora), se vuelcan cada minuto, **se suman** (un reinicio a mitad de hora no pisa nada) y se conservan un año. Bajo la gráfica hay un resumen del periodo con cuánto han crecido la base de datos y los eventos guardados, útil para prever el disco. Solo se guardan contadores: nunca contenido, claves ni IPs. El histórico empieza el día en que se activó la función.

## Página de presentación

Al abrir la URL del relé en un navegador se ve una pequeña página de presentación (`static/index.html`, `landing.css`, `landing.js`; sin dependencias externas): nombre, icono y descripción del relé, su dirección `wss://` con un botón de copiar, los NIPs soportados, los límites y las reglas, en español e inglés. Se rellena sola con el documento NIP-11 del propio relé, así que siempre está al día. Caddy solo la sirve a los `GET` normales de `/`: los clientes de Nostr (`Accept: application/nostr+json`), los WebSockets y los `POST` de NIP-86 siguen llegando al relé sin tocar.

Para que la descripción sea bilingüe, escríbela como `English text | Texto en español` en `RELAY_DESCRIPTION`: todos los clientes muestran las dos, y la página de presentación enseña la del idioma elegido.

## Icono del relé

Los clientes muestran el `icon` de NIP-11. Pon una imagen cuadrada (PNG/JPG/WebP, ~512×512, ligera) en `static/`, define `RELAY_ICON=/icon.png` y Caddy la sirve en `https://<RELAY_DOMAIN>/icon.png` (`static/icon.png` y `static/icon.svg` son los de la instancia pública). Mejor PNG: muchas apps nativas no renderizan SVG. `changerelayicon` de NIP-86 lo cambia en caliente.

## Retención de datos

Un relé abierto acumula eventos para siempre si no le dices otra cosa. `RELAY_RETENTION_DAYS=N` activa una pasada en segundo plano (un minuto tras arrancar y luego cada hora, como mucho 20 000 borrados por pasada) que elimina los eventos **normales** con más de `N` días: notas, reacciones, reposts, peticiones de borrado, mensajes directos… Nunca borra lo que describe el *estado actual* de una cuenta (perfiles, contactos, listas de relés y demás tipos reemplazables o direccionables: uno por cuenta y tipo) ni nada publicado por el dueño (`RELAY_PUBKEY`). `0` (el valor por defecto) lo guarda todo. Cada pasada que borra algo deja una línea `retention deleted=… scanned=…` en el log. El panel de control muestra el ajuste y una gráfica de *eventos por día* para valorar el ritmo de crecimiento. La instancia de producción usa 180 días.

## Restaurar un backup

Los backups (`scripts/backup-db.sh`, cada noche en el cron) son copias consistentes de SQLite. Para restaurar uno:

```bash
docker compose stop relay
./scripts/restore-db.sh ~/backups/nostr-relay-khatru/nostr-relay-khatru-<fecha>.sqlite.gz
docker compose up -d relay
```

El script comprueba que el backup se descomprime y pasa el `integrity_check` de SQLite, se niega a ejecutarse mientras un contenedor use el volumen, guarda la base de datos *actual* como `pre-restore-<fecha>.sqlite.gz` (por si te equivocas de backup), cambia el archivo y borra los `-wal`/`-shm` viejos. Te pide escribir `restaurar`, salvo con `--yes`. Para **ensayar** sin tocar producción, restaura en un volumen de pruebas: `VOLUME_NAME=restore-test ./scripts/restore-db.sh <backup.gz> --yes` y arranca un relé sobre él (`docker run -d -v restore-test:/app/data <imagen>`). Ese ensayo se hizo el 2026-09-30: un backup real se restauró, el relé arrancó con él y sirvió sus eventos.

## Desplegar

`./scripts/deploy.sh` recompila la imagen metiendo en el binario la versión de git (`git describe --tags --always --dirty`) —aparece en el `version` de NIP-11, en el panel y en la página de presentación— y recrea el relé; añade `--caddy` tras editar el Caddyfile o el `.env`. Caddy envía además `Strict-Transport-Security` (solo HTTPS, un año).

## Operación

- **Backups**: `./scripts/backup-db.sh` hace una copia consistente con `sqlite3 .backup`
  (segura mientras el relé escribe) en `~/backups/nostr-relay-khatru`, conservando 14
  días. Prográmalo tú en cron, por ejemplo `17 3 * * * /ruta/a/scripts/backup-db.sh`.
- **Autorrecuperación (opcional)**: `./scripts/healthcheck.sh` reinicia los contenedores
  tras fallos repetidos. No se instala solo; lee antes su cabecera.
- **Avisos externos (opcional)**: el script también puede avisar a un servicio de monitorización: define `PING_URL` (se llama cada vez que el relé responde; un servicio como healthchecks.io te escribe cuando los avisos *dejan de llegar*, lo que cubre también que caiga el servidor entero) y, si quieres, `PING_FAIL_URL` (se llama cuando hay `ALERT_THRESHOLD` fallos seguidos —2 por defecto, el mismo momento en que reiniciaría— para avisar antes; un tropiezo aislado no manda nada). Un monitor inalcanzable nunca rompe el chequeo. O apunta un monitor HTTP externo (UptimeRobot…) a `https://<RELAY_DOMAIN>/`, que responde 200.
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
