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
  **Verified with a real client (Damus on iOS, 2026-09-29):** a kind-4 DM published through this relay was
  read by the recipient account after Damus answered the `auth-required:` challenge with NIP-42 (the log showed one
  `reject filter kind=4 … auth-required`, then successful authentications and no repeat).
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

## Control panel (`/admin`)

`internal/admin` = read-only API under `/admin/api/*` mounted on khatru's `Router()` mux (khatru wraps it in CORS `*`, so
protection is the session cookie: `HttpOnly`, `SameSite=Strict`, `Secure` when HTTPS — never rely on CORS). Login =
NIP-98 (kind 27235) signed by `RELAY_PUBKEY`, `u` = exact URL (from `RELAY_PUBLIC_URL` or `X-Forwarded-*`), `method` =
POST, ±60 s, each signature id single-use (3 min memory), 10 attempts/min/IP; exchanged for a 1 h in-memory session (max 5,
lost on restart). No owner → 403. Stats come from a second sqlite connection opened `_query_only=true` (results cached
10 s) plus `activityLog` (per-minute buckets for 2 h, last 100 rejections, totals by reason: `Minutes/Rejections/
ReasonTotals`, all in memory). **Privacy rules enforced by tests**: no IPs, rejections carry only 8 chars of the pubkey, and
`privateKinds` (4, 13, 14, 1059) content is never sent. The UI is `static/admin/{index.html,admin.js,admin.css}` with a strict
CSP (`script-src 'self'; style-src 'self'`): **no inline `style=` attributes or `innerHTML`** — set widths via CSSOM
(`el.style.width`) and text via `textContent` (a page test injects `<img onerror>`/`<script>` in a note to prove it).
Caddy: `/admin/api/*` → relay, `/admin`, `/admin/admin.js|css` → static with `Cache-Control: no-cache`. Phase 2 (moderation) is below. The browser extension used is nos2x (NIP-07): the page asks it to sign one event per login.
`/admin/api/session` answers 401 when logged out, so a 401 in the console at page load is expected.

**Retention** (`internal/retention`): `RELAY_RETENTION_DAYS` (0 = off) → hourly pass (first one 1 min after start) that deletes
*regular* kinds (`nostr.IsRegularKind`: <10000 except 0 and 3) older than N days, never the owner's events, never
replaceable/addressable ones (current account state), max 20 000 per pass, paging by `Until` from the oldest seen. It uses the raw
store (`db.QueryEvents/DeleteEvent`) so the private-kinds filter doesn't hide anything. Logs `retention deleted=…` only when it
deleted something. `Server.RunRetentionOnce` exists for tests. Production: 180 days. The panel shows `perDay` (events by
creation date, last 14 days — *creation* date, not arrival) and the setting.

**Restore / deploy scripts**: `scripts/restore-db.sh` (integrity check → refuses if a container uses the volume → saves
`pre-restore-*.sqlite.gz` → swaps the file, removes `-wal/-shm`; rehearse with `VOLUME_NAME=<scratch>`; never run it with
`--yes` against the production volume to "test" it) and `scripts/deploy.sh` (exports `VERSION=$(git describe --tags --always
--dirty)` which compose passes as a build arg → `main.version` → NIP-11/panel/landing). Use deploy.sh instead of a bare
`docker compose up --build`, or the version shows as `dev`. Caddy sends HSTS (`max-age=31536000`, no subdomains/preload).

**Built-in help** (`<dialog id="help">` in `static/admin/index.html`, static Spanish HTML; opened by the *Ayuda* button and the `?`
/`data-help="h-…"` buttons next to each section title, scrolling inside the dialog — the TOC links are intercepted so the URL
doesn't change). **When you add a card, config field or moderation box, add its explanation to the help**: a Playwright check
once verified that every `.card .l`, `#config dt` and `#mod h3` label appears in the help text. The NIP list in the help is static
text — update it if the supported NIPs change.

**Phase 2 — moderation from the panel** (`internal/admin/mod.go`): `GET /admin/api/moderation` (lists, effective info, defaults,
overrides) and `POST /admin/api/mod/{ban-pubkey,unban-pubkey,allow-pubkey,unallow-pubkey,ban-event,unban-event,kind,ip,info}`.
Mutations go through `Panel.mutation`: valid session + `Sec-Fetch-Site` same-origin/none + `Origin` equal to ours +
`Content-Type: application/json` (defence in depth on top of the SameSite=Strict cookie — khatru's CORS is `*`). Inputs accept
hex or npub / note1 / nevent1 (`nip19`), reasons ≤200 chars, no control characters; name ≤80, description ≤600, icon must be
https:// or a `/path` (an empty icon = no icon; name/description can't be empty — use `reset`). **The owner is exempt** from
ban/allow-list/kind rules in `policies.NewModeration(store, owner)` (so a bad list can't lock the owner out) and the API refuses
to ban the owner. Removing from a list has its own store methods (`UnbanPubKey`, `RemoveAllowedPubKey`, `ClearKindRule`,
`DeleteSetting`) because NIP-86's `allowpubkey` on a banned key would *enable the allow-list*. Deleting events goes through
`Server.DeleteEventByID/DeleteEventsByAuthor` (`admin.Effects`), reading the store directly (the private-kinds filter must not
hide them from the moderator). Each action logs `admin action=… target=<8 chars>` via `activityLog.Admin` — never full keys or
reasons (tested). **Frontend gotcha**: the moderation *forms are static HTML* and the 15 s auto-refresh only re-renders the
*lists*; the relay-info form is filled only on first load and after save/restore (`forceInfo`), otherwise the refresh would wipe
what you're typing. Go serialises nil slices as `null`: every list sent to the panel goes through `nonNil` (a `null` broke
`.map` in the page once).

## Deployment

**Reboot verified (2026-09-30)**: after a real server reboot both containers came back on their own (`restart: unless-stopped`,
docker enabled at boot), Caddy reused its stored certificate, cron entries survived, `/tmp` was wiped (don't keep anything you
need there) and Damus reconnected by itself. The public relay also receives real third-party traffic: ephemeral events
(`kind 20001`, unknown app) that the per-IP limiter rejects in bursts — expected on an open relay; `disallowkind` via NIP-86 or
`RELAY_ALLOWED_KINDS` would block them if ever wanted.

**Bilingual description convention**: `RELAY_DESCRIPTION="English | Español"` (split on " | " in `landing.js`'s `pickLanguage`; any
other clients simply show both). The production `.env` uses it. Editing `.env` needs `docker compose up -d --force-recreate relay`.

**Landing page** (`static/index.html` + `landing.css` + `landing.js`): the Caddyfile serves it for `GET`/`HEAD` of `/` that
are NOT NIP-11 (`Accept: application/nostr+json`), NOT WebSocket (`Upgrade`/`Connection`) — NIP-86 `POST`s also skip it
(`method GET HEAD`). Because that matcher doesn't require `Accept: text/html`, plain `curl`, browsers and uptime monitors all
get a 200 page (an early version required text/html, so a monitor sending `*/*` would have reached the relay's 404).
`+Vary Accept` is set since one URL returns different things. The page has a strict CSP (`script-src 'self'`, no inline) and
inserts everything from NIP-11 with `textContent` — keep that: the name/description can be changed live via NIP-86. The npub
shown in the footer is bech32-encoded in `landing.js` (verified against nostr-tools). All static assets (`/landing.css`, `/landing.js`, icons, admin) are sent with `Cache-Control: no-cache` — without it browsers kept an
older CSS for hours (heuristic caching from `Last-Modified`) and a styling fix looked like it hadn't worked. Static files are a *directory* bind mount
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
healthcheck script (pings healthchecks.io via `PING_URL`/`PING_FAIL_URL` set in the cron line — the ping URL is a secret, it lives only in the crontab, never in git) runs every 2 minutes from the same crontab (`RELAY_URL=https://relay.hivescope.xyz`; log in
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
