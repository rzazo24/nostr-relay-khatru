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

- `main.go` wires a `khatru.Relay` to the sqlite backend, the NIP-11 info document and
  three policy hooks. **khatru stops at the first `RejectEvent` that rejects, so order
  matters**: the IP rate limiter goes first (cheap), then `policies.NewEventLimits`.
- `internal/config` reads env vars (`RELAY_*`) through an injected `get` function so it
  is testable; invalid values fail startup with an error that names the variable.
- `internal/policies/limits.go` — content length (runes, not bytes), tag count, tag
  value size, `created_at` future skew, already-expired NIP-40 events, optional kind
  allow-list (kind 5 deletions are always allowed). A limit of 0 disables it. Old
  events are accepted on purpose (backfilling history).
- `--healthcheck` subcommand: the binary requests its own NIP-11 document; used as the
  Docker healthcheck because the runtime image has no curl/wget.

**The sqlite backend silently caps queries** (learned the hard way in hivescope-relay):
by default every query returns at most 100 events and allows 10 tag values per filter,
with no error — a client asking `limit: 500` gets 100. `main.go` sets the backend's
`QueryLimit` to `RELAY_MAX_LIMIT` and `QueryTagsLimit` to 500, and wraps `QueryEvents`
so filters without a `limit` (or above the max) get `RELAY_MAX_LIMIT`. The smoke test
asserts that `limit: 500` returns 130 events; keep that test if you touch storage.

**IP rate limits depend on the proxy**: khatru takes the client IP from
`X-Forwarded-For` (Caddy sets it). Without a proxy in front, everyone behind the same
address shares one bucket. Limits are per IP: `RELAY_EVENTS_*`, `RELAY_REQS_*`,
`RELAY_CONNS_*`.

## Deployment

`docker-compose.yml`: `relay` (no published port) behind `caddy` (TLS for
`RELAY_DOMAIN`). `scripts/backup-db.sh` and `scripts/healthcheck.sh` are provided but
**never installed automatically** (no cron is created) — the operator decides. The
healthcheck script restarts `relay` + `caddy` after repeated failures with a cooldown;
it can't alert anyone.

## Conventions

Comments and READMEs follow the sibling project: Spanish comments in code, README in
English with a Spanish translation (`README.es.md`) — keep both in sync when behavior
changes.
