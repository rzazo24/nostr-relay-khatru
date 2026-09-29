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

## What it does

- Speaks the core protocol and stores events in SQLite: NIP-01 (events, filters,
  subscriptions), NIP-09 (deletions), NIP-11 (relay information document, with the
  configured limits), NIP-40 (expiration — khatru drops expired events and this
  relay refuses events that are already expired) and NIP-45 (`COUNT`).
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

Not included, by design: authentication (NIP-42), paid access, allow-lists, search
(NIP-50), a web dashboard. It's a small relay; if you need those, look at
[khatru's examples](https://github.com/fiatjaf/khatru) — this repo is a good starting point.

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
| `RELAY_NAME`, `RELAY_DESCRIPTION`, `RELAY_PUBKEY`, `RELAY_CONTACT` | | NIP-11 document |
| `RELAY_MAX_CONTENT_LENGTH` | `65536` | Characters in `content` |
| `RELAY_MAX_EVENT_TAGS` | `2000` | Tags per event |
| `RELAY_MAX_TAG_VALUE_BYTES` | `1024` | Bytes in each tag element |
| `RELAY_MAX_FUTURE_SKEW_SECONDS` | `900` | How far ahead `created_at` may be (`0` = no limit) |
| `RELAY_ALLOWED_KINDS` | *(all)* | Comma-separated kinds; kind 5 (deletions) is always allowed |
| `RELAY_MAX_LIMIT` | `500` | Max events per filter |
| `RELAY_EVENTS_PER_MINUTE` / `_BURST` | `30` / `60` | Events per IP |
| `RELAY_REQS_PER_MINUTE` / `_BURST` | `60` / `180` | Subscriptions per IP |
| `RELAY_CONNS_PER_MINUTE` / `_BURST` | `20` / `60` | Connections per IP |

An invalid value stops the relay at startup with an error naming the variable.

## Operations

- **Backups**: `./scripts/backup-db.sh` makes a consistent copy with `sqlite3 .backup`
  (safe while the relay is writing) into `~/backups/nostr-relay-khatru`, keeping 14
  days. Schedule it with cron yourself, e.g. `17 3 * * * /path/to/scripts/backup-db.sh`.
- **Self-heal (optional)**: `./scripts/healthcheck.sh` restarts the containers after
  repeated failures. It is never installed automatically; read its header first.
- **Logs**: `docker compose logs -f relay`.

## The sqlite query limit caveat

The `eventstore/sqlite3` backend silently caps every query at 100 events (and 10
tag values per filter) unless you configure it — a client asking for `limit: 500`
gets 100 with no error. `main.go` aligns the backend's `QueryLimit` with
`RELAY_MAX_LIMIT` and raises the tag-value cap, and the smoke test checks that a
`limit: 500` query really returns 130 events. Keep that in mind if you swap the
storage backend.

## Development

```bash
CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go vet ./... && CGO_ENABLED=1 go test ./...

# end-to-end smoke test against a running relay (Node + nostr-tools). The volume test
# publishes 130 events, so start the relay with high rate limits:
RELAY_EVENTS_PER_MINUTE=1000 RELAY_EVENTS_BURST=1000 RELAY_REQS_PER_MINUTE=1000 RELAY_REQS_BURST=1000 go run . &
cd test && npm install && RELAY_URL=ws://localhost:3334 npm test
```

See [`CLAUDE.md`](CLAUDE.md) for how the code fits together.

## License

[MIT](LICENSE)
