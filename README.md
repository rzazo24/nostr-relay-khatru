# nostr-relay-khatru

[![CI](https://github.com/rzazo24/nostr-relay-khatru/actions/workflows/ci.yml/badge.svg)](https://github.com/rzazo24/nostr-relay-khatru/actions/workflows/ci.yml)

*[Leer en español](README.es.md)*

A small, general-purpose [Nostr](https://nostr.com/) relay written in Go with
[khatru](https://github.com/fiatjaf/khatru) and SQLite storage. It is open for
reading and writing — no accounts, no allow-list — with limits so that a single
client can't fill the disk or take it down.

It grew out of [hivescope-relay](https://github.com/rzazo24/hivescope-relay), a
single-purpose relay for a chat linked to Hive accounts (now discontinued),
keeping what was useful (rate limits, SQLite setup, Docker, backup) and dropping
everything specific to that chat.

**Public instance:** `wss://relay.hivescope.xyz` — open for reading and writing, with the
rate limits described below. Please be kind to it; if it gets abused, it will be tightened.

## What it does

- Speaks the core protocol and stores events in SQLite. Supported NIPs (and the
  relay's NIP-11 document advertises exactly these, no more): **1** (events, filters,
  subscriptions), **9** (deletions), **11** (information document), **13** (proof of work,
  only when enabled), **40** (expiration), **42** (authentication), **45** (`COUNT`),
  **70** (protected events) and **77** (Negentropy sync), plus **86** (management API)
  when an owner is configured.
- Accepts any kind by default; optionally restrict it with `RELAY_ALLOWED_KINDS`.
- **Per-event limits**: content length (characters, not bytes), number of tags, size
  of each tag value, and how far in the future `created_at` may be. Old events are
  accepted on purpose so people can copy their history over.
- **Per-query limit**: every filter's `limit` is capped at `RELAY_MAX_LIMIT`
  (500 by default). See [the sqlite caveat](#the-sqlite-query-limit-caveat).
- **Per-IP rate limits** for events, subscriptions (REQ) and connections, using the
  real client IP from `X-Forwarded-For` (set by Caddy).
- Docker Compose with Caddy for automatic TLS, a self-contained Docker
  healthcheck, a safe SQLite backup script and an optional self-heal script.

### NIP-86: management API (moderation without redeploying)

Set `RELAY_PUBKEY` to your public key (hex) and use any NIP-86 client (signing with that
key, via NIP-98 HTTP auth) to change these lists on the fly — they are stored in the same
SQLite file and survive restarts:

- **Pubkeys**: `banpubkey` / `allowpubkey`. With at least one allowed pubkey the relay
  becomes **write-restricted**: only allowed pubkeys can publish (reading stays open) and
  NIP-11 says `restricted_writes: true`. Banning removes from the allowed list and
  vice versa.
- **Events**: `banevent` deletes the event if it's stored and refuses it from then on.
- **Kinds**: `allowkind` / `disallowkind` (a non-empty allow list means only those kinds).
- **IPs**: `blockip` / `unblockip`.
- **Info**: `changerelayname`, `changerelaydescription`, `changerelayicon`.

Only the owner can call it; without `RELAY_PUBKEY` the API is disabled. For NIP-98 to
verify the URL behind a proxy, Caddy's `X-Forwarded-*` headers are used, or set
`RELAY_PUBLIC_URL`.

### NIP-13: proof of work (anti-spam)

`RELAY_MIN_POW=N` requires every event's id to start with at least `N` zero bits **and**
to commit to that target in its `nonce` tag (so lucky ids without real mining don't
pass). Deletions (kind 5) are exempt. Off by default; ~20 bits costs a client a second or
so of CPU per event, which is nothing for a person and a lot for a spam bot.

### NIP-42: authentication and private messages

- The relay offers an `AUTH` challenge as soon as a client connects.
- **Private kinds** (`RELAY_PRIVATE_KINDS`, default `4` and `1059` — legacy DMs and NIP-17/59
  gift wraps): only the author and the recipient (`p` tag), authenticated, can read them.
  A query aimed only at those kinds gets `auth-required:` so the client authenticates and
  retries; a generic query (no kinds) is served **without** them, and live subscriptions
  never receive them. Publishing them needs nothing special (gift wraps come from throwaway
  keys).
- `RELAY_AUTH_REQUIRED=true` makes authentication mandatory for reading and writing.
- NIP-70 protected events (tag `["-"]`) can only be published by their authenticated author.

### NIP-77: Negentropy sync

Clients and other relays can reconcile their events with this relay without downloading
everything. A sync session is not limited by `RELAY_MAX_LIMIT` (that would make it
silently incomplete) but by `RELAY_MAX_NEGENTROPY_EVENTS` (100 000 by default).

## Running it

### With Docker (relay + Caddy with TLS)

```bash
cp .env.example .env      # set RELAY_DOMAIN and anything else you want
docker compose up -d --build
```

Point your domain's DNS at the server first; Caddy gets the certificate on its own.
The relay itself publishes no port on the host — only Caddy (80/443) is exposed.
Your relay is then at `wss://<RELAY_DOMAIN>`.

### Locally, without Docker

```bash
CGO_ENABLED=1 go run .    # listens on :3334, data in ./data/relay.sqlite
```

`CGO_ENABLED=1` and a C compiler are required: the SQLite driver
([mattn/go-sqlite3](https://github.com/mattn/go-sqlite3)) uses cgo.

## Configuration

Everything is an environment variable (see [`.env.example`](.env.example)); all are
optional except `RELAY_DOMAIN` when using Docker.

| Variable | Default | Meaning |
|---|---|---|
| `RELAY_LISTEN_ADDR` | `:3334` | Address the relay listens on |
| `RELAY_DB_PATH` | `./data/relay.sqlite` | SQLite file |
| `RELAY_NAME`, `RELAY_DESCRIPTION`, `RELAY_CONTACT` | | NIP-11 document |
| `RELAY_PUBKEY` | | Owner's public key (hex): NIP-11 `pubkey` and the only one allowed to use NIP-86 |
| `RELAY_PUBLIC_URL` | *(deduced)* | Public https URL, for NIP-42/NIP-98 |
| `RELAY_ICON` | | Relay icon for NIP-11: an absolute URL, or a path like `/icon.png` (served by Caddy from `./static`) |
| `RELAY_MAX_CONTENT_LENGTH` | `65536` | Characters in `content` |
| `RELAY_MAX_EVENT_TAGS` | `2000` | Tags per event |
| `RELAY_MAX_TAG_VALUE_BYTES` | `1024` | Bytes in each tag element |
| `RELAY_MAX_FUTURE_SKEW_SECONDS` | `900` | How far ahead `created_at` may be (`0` = no limit) |
| `RELAY_ALLOWED_KINDS` | *(all)* | Comma-separated kinds; kind 5 (deletions) is always allowed |
| `RELAY_MAX_LIMIT` | `500` | Max events per filter |
| `RELAY_MAX_NEGENTROPY_EVENTS` | `100000` | Max events offered to a NIP-77 sync session |
| `RELAY_MIN_POW` | `0` | NIP-13 minimum difficulty in bits (`0` = off) |
| `RELAY_AUTH_REQUIRED` | `false` | Require NIP-42 authentication to read and write |
| `RELAY_PRIVATE_KINDS` | `4,1059` | Kinds only their author/recipient can read (`none` = off) |
| `RELAY_EVENTS_PER_MINUTE` / `_BURST` | `30` / `60` | Events per IP |
| `RELAY_REQS_PER_MINUTE` / `_BURST` | `60` / `180` | Subscriptions per IP |
| `RELAY_CONNS_PER_MINUTE` / `_BURST` | `20` / `60` | Connections per IP |

An invalid value stops the relay at startup with an error naming the variable.

## Landing page

Opening the relay's URL in a browser shows a small presentation page (`static/index.html`, `landing.css`, `landing.js`; no external dependencies): the relay's name, icon and description, its `wss://` address with a copy button, the supported NIPs, the limits and the rules, in English and Spanish. It fills itself in from the relay's own NIP-11 document, so it is always up to date. Caddy serves it only to plain `GET`s of `/` — Nostr clients (`Accept: application/nostr+json`), WebSockets and NIP-86 `POST`s still reach the relay untouched.

To make the description bilingual, write it as `English text | Texto en español` in `RELAY_DESCRIPTION`: every client shows both, and the landing page shows the one matching the chosen language.

## Relay icon

Clients show the NIP-11 `icon`. Put a square image (PNG/JPG/WebP, ~512×512, small) in `static/`, set `RELAY_ICON=/icon.png` and Caddy serves it at `https://<RELAY_DOMAIN>/icon.png` (the `static/icon.png` and `static/icon.svg` here are the ones used by the public instance). Prefer PNG: many native apps can't render SVG. NIP-86's `changerelayicon` overrides it live.

## Operations

- **Backups**: `./scripts/backup-db.sh` makes a consistent copy with `sqlite3 .backup`
  (safe while the relay is writing) into `~/backups/nostr-relay-khatru`, keeping 14
  days. Schedule it with cron yourself, e.g. `17 3 * * * /path/to/scripts/backup-db.sh`.
- **Self-heal (optional)**: `./scripts/healthcheck.sh` restarts the containers after
  repeated failures. It is never installed automatically; read its header first.
- **External alerts (optional)**: the script can also ping a monitoring service — set `PING_URL` (called each time the relay answers; a service like healthchecks.io emails you when the pings *stop*, which also covers the whole server going down) and, optionally, `PING_FAIL_URL` (called on a failure, to alert sooner). An unreachable monitor never breaks the check. Or point an outside HTTP monitor (UptimeRobot…) at `https://<RELAY_DOMAIN>/`, which answers 200.
- **Logs**: `docker compose logs -f relay`. The relay logs what it *rejects* (`reject event kind=1 pubkey=ab12cd34 reason="rate-limited: …"`, at most 5 lines per reason per minute) and, when there was activity, a one-line summary per minute (`stats … saved=12 rejected=3 [rate-limited=3]`). It never logs content or IPs, only the kind, the first 8 characters of the pubkey and the reason — handy to see why a client (say Damus) is being refused.

## Caveats worth knowing

- **The sqlite query limit.** The `eventstore/sqlite3` backend silently caps every query at
  100 events (and 10 tag values per filter) unless you configure it — a client asking for
  `limit: 500` gets 100 with no error. The server raises the backend's cap and applies the real
  limits itself (`RELAY_MAX_LIMIT`, or `RELAY_MAX_NEGENTROPY_EVENTS` in sync sessions); the smoke
  test checks that `limit: 500` really returns 130 events.
- **go-nostr must be ≥ 0.52.** khatru v0.19.1 depends on go-nostr v0.51.8, whose NIP-77 message parser
  is broken (it never recognizes `NEG-OPEN`, so sync hangs). `go.mod` pins v0.52.1, where it works;
  don't downgrade it. An integration test syncs 350 events to catch this.

## Development

```bash
CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go vet ./... && CGO_ENABLED=1 go test ./...
# internal/server has integration tests that start a real relay (NIP-13/42/70/77/86)

# end-to-end smoke test against a running relay (Node + nostr-tools). The volume test
# publishes 130 events, so start the relay with high rate limits:
RELAY_EVENTS_PER_MINUTE=1000 RELAY_EVENTS_BURST=1000 RELAY_REQS_PER_MINUTE=1000 RELAY_REQS_BURST=1000 go run . &
cd test && npm install && RELAY_URL=ws://localhost:3334 npm test
```

See [`CLAUDE.md`](CLAUDE.md) for how the code fits together.

## License

[MIT](LICENSE)
