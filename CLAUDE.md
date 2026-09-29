# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A small **general-purpose** Nostr relay: Go + [khatru](https://github.com/fiatjaf/khatru)
+ SQLite (`eventstore/sqlite3`). Open for reading and writing, no auth, no allow-list;
it only enforces size/rate limits. It was derived from `hivescope-relay` (a
single-purpose Hive-linked chat relay, discontinued — don't port its Hive/room/presence
logic here). Docs are in `README.md` / `README.es.md`; this file is about how the code fits.

## Commands

```bash
# CGO_ENABLED=1 is required: the sqlite driver (mattn/go-sqlite3) uses cgo
CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go vet ./... && CGO_ENABLED=1 go test ./...
CGO_ENABLED=1 go run .                       # :3334, ./data/relay.sqlite
go test ./internal/policies/ -run TestLimits_FutureSkew -v

# integration tests start a real relay (internal/server): CGO_ENABLED=1 go test ./internal/server/

# end-to-end smoke test (Node + nostr-tools). Start the relay with HIGH rate limits
# first — the volume test publishes 130 events:
RELAY_EVENTS_PER_MINUTE=1000 RELAY_EVENTS_BURST=1000 RELAY_REQS_PER_MINUTE=1000 RELAY_REQS_BURST=1000 CGO_ENABLED=1 go run . &
cd test && npm install && RELAY_URL=ws://localhost:3334 npm test

docker compose up -d --build                 # relay + Caddy (needs RELAY_DOMAIN in .env)
./scripts/backup-db.sh                       # consistent sqlite .backup (not cp)
```

CI (`.github/workflows/ci.yml`) runs build/vet/unit tests plus the Node smoke test
against a real running binary — mirror that when changing behavior.

## Architecture

- `main.go` only loads config and starts `internal/server`, which builds the whole relay
  (so integration tests can start it): sqlite backend, NIP-11 document, policy hooks, NIP-86.
  **khatru stops at the first `RejectEvent` that rejects, so order matters**: IP rate
  limiter, auth-required, moderation lists, `NewEventLimits` (sizes, skew, kinds,
  expiration), then `NewPoW` last.
- `internal/config` reads env vars (`RELAY_*`) through an injected `get` function so it
  is testable; invalid values fail startup with an error that names the variable.
- `internal/policies`: `limits.go` (content length in runes, tag count/size, `created_at`
  skew, already-expired NIP-40, static kind allow-list; 0 disables a limit; old events
  accepted on purpose), `pow.go` (NIP-13: actual difficulty AND the target committed in the
  `nonce` tag; kind 5 exempt), `moderation.go` (banned/allowed pubkeys, banned events,
  kinds — via the small `Moderator` interface), `auth.go` (NIP-42 helpers and
  `PrivateKinds`).
- `internal/moderation` is the NIP-86 state: lists in tables of the SAME sqlite file
  (a second connection, WAL) mirrored in memory for the hot path. Banning a pubkey removes
  it from the allow list and vice versa; ≥1 allowed pubkey = write-restricted relay.
- **NIP-86 auth**: only `RELAY_PUBKEY` (the owner) passes `RejectAPICall`; with it unset the
  API is disabled. NIP-98 verifies the `u` tag against khatru's base URL (from
  `RELAY_PUBLIC_URL`, else `X-Forwarded-*`).
- **Private kinds** (`RELAY_PRIVATE_KINDS`, default 4 and 1059) are enforced in three places:
  `Server.query` filters results by `PrivateKinds.Visible(event, viewer)` (skipped for
  `khatru.IsInternalCall`, otherwise deleting your own DM wouldn't find it), `PreventBroadcast`
  stops live delivery, and `NewPrivateFilter` answers `auth-required:` only to filters aimed
  *exclusively* at private kinds (so clients authenticate and retry). Generic filters are served
  minus the private events instead of rejected. `OnConnect` sends an AUTH challenge.
- **NIP-70** (protected events) is implemented by khatru itself — don't add a policy for it.
- `internal/server/activity.go`: the activity log. Policies are wrapped by `logEvent`/`logFilter`
  (they never change the decision); rejections are logged capped per reason per minute plus a
  1-minute `stats` summary printed only when something happened (no timer: it rolls when the next
  thing is logged). **Privacy rule: never log content, IPs or full pubkeys.** A test enforces it.
  `LogOutput` is a package variable so tests can capture it.
- `--healthcheck` subcommand: the binary requests its own NIP-11 document; used as the
  Docker healthcheck because the runtime image has no curl/wget.

**The NIP-11 document must not lie**: `setupInfo` overrides khatru's default `supported_nips`
(which advertises 42/70/86 unconditionally) with exactly what is enabled; 13 only if
`RELAY_MIN_POW>0`, 86 only if `RELAY_PUBKEY` is set. `restricted_writes` follows the allow
list live (via `OverwriteRelayInformation`). A test asserts the list.

**The sqlite backend silently caps queries** (learned the hard way in hivescope-relay):
by default every query returns at most 100 events and allows 10 tag values per filter,
with no error. The backend's `QueryLimit` is set to `max(RELAY_MAX_LIMIT,
RELAY_MAX_NEGENTROPY_EVENTS)` and `Server.query` applies the real cap: `RELAY_MAX_LIMIT`
normally, `RELAY_MAX_NEGENTROPY_EVENTS` when `eventstore.IsNegentropySession(ctx)` (an NIP-77
session capped at 500 would sync incompletely and never say so).

**go-nostr must stay ≥ v0.52**: khatru v0.19.1 pins v0.51.8, whose `nip77.ParseNegMessage`
compares the label with its quotes still attached, so `NEG-OPEN` is never recognized and
negentropy hangs. `go.mod` overrides it to v0.52.1. `TestNIP77_*` in `internal/server`
fails (hangs) if this regresses.

**IP rate limits depend on the proxy**: khatru takes the client IP from
`X-Forwarded-For` (Caddy sets it). Without a proxy in front, everyone behind the same
address shares one bucket. Limits are per IP: `RELAY_EVENTS_*`, `RELAY_REQS_*`,
`RELAY_CONNS_*`.

**Testing**: `internal/server/server_test.go` starts a real relay on an `httptest` server
with sqlite in a temp dir and talks to it with the go-nostr client (AUTH, NIP-98-signed
management calls, NEG sync, NIP-11). Rate limits are raised in `start()`. When a NIP-42 test
authenticates right after connecting, the challenge arrives asynchronously — `keys.auth` retries.

## Deployment

**Landing page** (`static/index.html` + `landing.css` + `landing.js`): the Caddyfile serves it for `GET`/`HEAD` of `/` that
are NOT NIP-11 (`Accept: application/nostr+json`), NOT WebSocket (`Upgrade`/`Connection`) — NIP-86 `POST`s also skip it
(`method GET HEAD`). Because that matcher doesn't require `Accept: text/html`, plain `curl`, browsers and uptime monitors all
get a 200 page (an early version required text/html, so a monitor sending `*/*` would have reached the relay's 404).
`+Vary Accept` is set since one URL returns different things. The page has a strict CSP (`script-src 'self'`, no inline) and
inserts everything from NIP-11 with `textContent` — keep that: the name/description can be changed live via NIP-86. The npub
shown in the footer is bech32-encoded in `landing.js` (verified against nostr-tools). Static files are a *directory* bind mount
(edits go live at once); the Caddyfile is a *file* mount, so after editing it run `docker compose up -d --force-recreate caddy`.

`static/` holds the relay icon (`icon.svg` is the source, `icon.png` 512×512 is what is advertised —
rendered with headless Chromium since no SVG converter is installed). Caddy serves `/icon.png` and
`/icon.svg` from it (mounted read-only at `/srv/static`); `RELAY_ICON=/icon.png` puts it in NIP-11, and
khatru resolves relative icon paths against the public URL. The Caddyfile is a single-file bind mount:
after editing it, `docker compose up -d --force-recreate caddy`.

Running in production on the author's Oracle Cloud VPS (arm64) at `wss://relay.hivescope.xyz`
via this compose file, with a local `.env` (gitignored: `RELAY_DOMAIN`, name, description,
`RELAY_PUBLIC_URL`, `RELAY_PUBKEY` = the owner's key for NIP-86). `docker compose up -d --force-recreate relay`
after editing `.env` (env_file is read when the container is created). A daily cron runs
`scripts/backup-db.sh` (03:17, log in `~/backups/nostr-relay-khatru/backup.log`); the
healthcheck script runs every 2 minutes from the same crontab (`RELAY_URL=https://relay.hivescope.xyz`; log in
`~/backups/nostr-relay-khatru/healthcheck.log`, only written on failures/recoveries).


`docker-compose.yml`: `relay` (no published port) behind `caddy` (TLS for
`RELAY_DOMAIN`). `scripts/backup-db.sh` and `scripts/healthcheck.sh` are provided but
**never installed automatically** (no cron is created) — the operator decides. The
healthcheck script restarts `relay` + `caddy` after repeated failures with a cooldown;
it can't alert anyone.

## Conventions

Comments and READMEs follow the sibling project: Spanish comments in code, README in
English with a Spanish translation (`README.es.md`) — keep both in sync when behavior
changes.
