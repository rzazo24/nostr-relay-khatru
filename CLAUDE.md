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

# control-panel end-to-end (Playwright + real relay binary; build it first with `go build -o nostr-relay-khatru .`)
cd test/panel && npm install && npx playwright install chromium && RELAY_BIN=../../nostr-relay-khatru npm test

docker compose up -d --build                 # relay + Caddy (needs RELAY_DOMAIN in .env)
./scripts/backup-db.sh                       # consistent sqlite .backup (not cp)
# copy one account's events between relays, as-is (read-only on the source; try --dry-run first)
node scripts/copy-events.mjs <src-relay> <dst-relay> <npub|hex> [--dry-run]
```

Maintenance: `.github/dependabot.yml` opens weekly PRs (Go modules, Docker base images, Actions; npm monthly) and the CI job `vulncheck` runs `govulncheck` (fails only on vulnerabilities your code actually calls; the VPS itself patches via unattended-upgrades but needs a manual reboot for new kernels).

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

**Panel tests** (`test/panel/`, CI job `panel-test`): `harness.mjs` starts the real binary plus a tiny server standing in for Caddy
(serves `static/` and proxies `/admin/api/*`; it **reads the `/admin` CSP from the Caddyfile**, so the strict policy is really
enforced), `panel.test.mjs` drives Chromium with a simulated nos2x. Gotchas: stats are cached 10 s in the relay (use
`refreshUntil`); a login signature is single-use, so the fake signer adds a random `nonce` tag (two logins in the same
second would otherwise collide); the help-coverage test fails if a new card/config/moderation label is not explained in the help
dialog, and the last test fails on any console error (incl. CSP violations). Rate limits are set sky-high via env.

**Panel i18n** (`static/admin/i18n.js` engine + `i18n.en.js` dictionary; Spanish is the source language): fixed HTML text is translated
generically — a "unit" is an element whose children are only inline tags (`innerHTML`), or a loose text node, or a `title`/`aria-label`/`placeholder` — and looked
up by an FNV-1a hash of the Spanish original in `PANEL_EN.html` (no markup needed in `index.html`; `data-i18n-skip` opts out). Text written by JS goes through
`t('texto en español', {vars})` (`PANEL_EN.ui`, keyed by the Spanish phrase, `{x}` placeholders). Spanish strings that the *server* sends (search `what`, history
`detail`) are translated by regex rules in `I18N.tx()`. **Adding or changing any user-facing text**: edit the Spanish, then run the panel tests — the English test
fails listing what is untranslated; `I18N.missing` entries look like `html:5c679508 Idioma: los botones…` (key + start of the text), add that key to `i18n.en.js`.
Never name a local variable `t` in `admin.js`/`nip46.js` (it shadows the translator). Section-fold state is keyed by the Spanish h2 text kept in `data-fold-key`
(set before the first translation). The stored reasons for bans made from the panel are written in the UI language at the time.

**Hover is mouse-only**: every `:hover` rule that changes colours/borders (panel `admin.css`, `landing.css`) lives inside `@media (hover:hover)`; on touch
screens hover sticks after a tap, so a tapped button ("Actualizar") stayed green. The mobile panel test taps `#refresh` and asserts its border is unchanged.
Keep new hover styles inside that media query.
**New keys** (`internal/moderation/firstseen.go`, `internal/policies/newkeys.go`): the relay records *when it first saw each pubkey* (receive time, table
`key_first_seen`, only for events that get stored — ephemerals are skipped on purpose; backfilled at startup from the oldest stored event). The policy is **last** in the
`RejectEvent` chain so rejected junk creates no rows. `RELAY_NEW_KEY_HOURS` (0 = off) additionally refuses `RELAY_NEW_KEY_KINDS` (default 1,6,16,30023) from keys younger
than that; the owner and the author allowlist are exempt and profile/lists/reactions/deletions always pass. The panel's «nueva» badge comes from `Panel.markNewKeys`
(computed *after* the 10 s `eventStats` cache, on a copy — the cache is shared) and from the search handler. Go tests: `TestNewKeys_*`, policy and store unit tests;
panel test «cuentas nuevas…». (`go test -race` flags a race inside go-nostr's client `Relay.Close` used by the test helpers — it predates this and is not in server code.)

**Public stats**: `GET /stats.json` (`internal/admin/publicstats.go`, mounted by `Panel.Mount`, cached 30 s) feeds the landing page's *Activity* section (`landing.js`
`renderStats`, `#activity`, hidden if the fetch fails). It is deliberately unauthenticated, so it may only carry aggregates: no pubkeys, IPs, content, rejection reasons
or private kinds (the Go test `TestPublicStats` greps the raw body for all of those). The panel opens its read-only DB connection even without `RELAY_PUBKEY` because this
endpoint needs it. Caddy needs no change (the catch-all reverse proxy serves it); the landing CSP already allows `connect-src 'self'`. Panel test: «página de inicio: cifras públicas…».

**Event types are colour-coded**: `kindFamily(k)` in `admin.js` maps a kind to a family (note, react, profile, private, ephemeral, app, auth, delete, zap, other);
`kindBadge(k)` draws the pill (number + name, so colour is never the only cue) and event cards (`#recent`, search results) get `data-fam` for the coloured left
border. Colours are the `[data-fam=…]{--kc}` rules in `admin.css`; the same badge is used in the Rejections and Noisy keys tables. Help text for it lives in `h-recent`
(with its English entry in `i18n.en.js`). Panel test: «los eventos se distinguen de un vistazo…».

**Pressed-button flash**: `admin.js` puts class `flash` on any `<button>` for 1 s after a click (delegated listener), and `admin.css` lights it (border + glow, red for
`.danger`) with a 0.35 s fade; nothing stays lit. The help `<dialog>` has `tabindex="-1" autofocus` and `openHelp()` focuses it, so the browser does not auto-focus
«Cerrar» (it kept the green focus ring). Panel test: «botones: el de cerrar la ayuda…».

**Phone layout** (end of `static/admin/admin.css`, `@media (max-width:700px), (pointer:coarse)` + `(max-width:700px)`): grids need `min-width:0`
(a table inside a grid item made the page 535 px wide on a 390 px phone); inputs must be ≥16 px or iOS zooms on focus; tap targets ≥40–44 px; tables get class `stack`
(set by `labelTable()` in `admin.js`, which also writes each cell's `data-label` from its `<th>`; the CSS prints it with `td::before`, so `innerText` doesn't
see the labels — test `getComputedStyle(td,'::before').content`); `<dialog>` is full-screen; `viewport-fit=cover` + `env(safe-area-inset-*)`. The Playwright
`iPhone 13` profile's usable viewport is 390×664 (no Safari chrome).
Folding: on narrow screens each `#dash section.panel > h2` is a `role=button` (`foldApply()`/`foldToggle()` in `admin.js`, keyed by the h2's first text node,
state in `localStorage['panel-plegado']`); folded = `.collapsed` → CSS hides every child but the h2. Anything that must reveal a section calls `foldOpen()`
(`searchFor()` does it for «Buscar»). A test that clicks inside a panel on a phone profile must open it first.

**NIP-46 login** (`static/admin/nip46.js`, lazy-loaded by `loginRemote()`; vendored crypto in `static/admin/vendor/nostr.js`, built by
`scripts/build-admin-vendor.sh` from `tools/admin-vendor-entry.mjs`): client-initiated `nostrconnect://` flow over *this* relay (`ws(s)://<host>`, **no trailing slash** — Clave's pairing is picky) plus the relays in
`#login[data-extra-relays]` (prod: `wss://relay.powr.build`, Clave's own relay, the only one its push proxy watches; also in the Caddyfile CSP
`connect-src`). The panel listens/sends on all of them and dedupes. Open-in-Clave link = Clave's universal link `https://clave.casa/connect/?uri=<encodeURIComponent(nostrconnect uri)>` (`clave://connect?uri=` is the fallback scheme; the first button attempt with it did not open the app, while pasting the plain `nostrconnect://` link into Clave › Connect worked end to end on a real iPhone, 2026-10-03). The login
template carries a random `nonce` tag (a signature is single-use; two logins in the same second would otherwise be identical). First valid kind-24133 whose decrypted `result` equals the URI secret pairs the signer;
then only `sign_event` for the NIP-98 login event is requested (no `get_public_key`: the server checks the signature belongs to the owner).
`internal/server/mailbox.go` keeps 24133 events 10 min in memory and answers REQs that have `kinds:[24133]` + `#p`; it registers an
`OnEphemeralEvent` hook, which also stops khatru answering `mute: no one was listening`. **Gotcha:** khatru ranges over every `QueryEvents`
channel internally, so a query function must never return a nil channel (it deadlocked normal publishes). NIP-46 messages are ~400 chars, so
the content-length limit must stay above that (tests use 800). Untested with a real Clave (first attempt failed with "no relay specified" when the URI had only our relay,
with a trailing slash).

**Backup download** (`GET /admin/api/backup`, `admin/backup.go`): `VACUUM INTO <tmp next to the DB>` over its *own* `mode=ro` connection
(the panel's `_query_only` connection can't run VACUUM INTO), gzipped on the fly, tmp deleted after; one at a time (`backupBusy`, 429 otherwise),
refuses `Sec-Fetch-Site: cross-site`, logged as action `backup`. Frontend fetches it into a Blob (fine under the strict CSP) — fine for DBs up to a
few hundred MB; if it grows past that, switch to a plain `<a download>` navigation.

**Action history** (`moderation_log` table, `Store.LogAction/RecentActions`, `Panel.record`): every panel action and every NIP-86
mutation (wrapped by `audited` in `setupManagementAPI`) stores ts/source/action/target(full)/detail (the owner's own note; for relay-info
only the *names* of the fields touched). Panel logins are logged; logout only when a session existed (the endpoint is unauthenticated).
The stdout log line stays the old privacy-preserving one (8-char target, no note, no IP). Kept to the last 1 000 rows.
**Server status** (`admin/serverstatus.go`): `Statfs` of the DB directory + newest `nostr-relay-khatru-*.sqlite.gz` in `RELAY_BACKUP_DIR`;
compose bind-mounts `${BACKUP_DIR}` read-only to `/backups` and only sets the env when `BACKUP_DIR` is defined in `.env`.

**Noisy keys** (`activityLog.noisy`, `admin.NoisyKey`, `GET /admin/api/stats` → `activity.noisy`): per-*full*-pubkey counters of
rejected **events** (not filters), memory only, ≤1 000 keys (`pruneNoisyLocked` drops stale >24 h, then the quieter half), never
logged or persisted. This is the one place the panel shows third parties' full keys (still never IPs); the log/recent-rejections
keep the 8-char rule. khatru verifies signatures before policies, so a counted pubkey really signed the event.
**Relay info** now has 7 fields (`admin.infoFields`: name, description, icon, contact, tags, languages, postingPolicy ↔ settings
in `moderation_settings`); `effectiveInfo` merges settings over `infoFromConfig(cfg)`. In `OverwriteRelayInformation` the icon is
only overridden when a setting exists, because khatru has already resolved a relative `RELAY_ICON` against the public URL.

**Search** (`internal/admin/search.go`, `GET /admin/api/search?q=&kind=&next=`): `planSearch` decides what `q` is (npub/nprofile →
author; note1/nevent1 → id; 64 hex → author OR id (and shows a key summary); 6–63 hex → prefix of pubkey OR id; ≤5 digits → kind;
otherwise `content LIKE` with `%`/`_`/`\` escaped). **Text search excludes private kinds (4, 13, 14, 1059) in SQL and never
returns their content** (tested). Optional `since`/`until` (unix seconds, inclusive, `since ≤ until`, validated) are ANDed into the WHERE; the frontend turns the `<input type=date>` days into local start/end-of-day. Pagination is a `created_at:id` cursor (`created_at < ? OR (=? AND id < ?)`), 50 per page; the
total is `COUNT(*)` over a `LIMIT 10001` subquery (so "más de 10 000"). The key summary comes only with the first page and
only when the query reduces to one full key. It's a scan with `LIKE` — fine at this scale; revisit (FTS5) if the table gets huge.
Frontend: `eventItem()` is shared by *recent* and *search* rows; after any moderation action `act()` calls `refreshSearch()` so a
vetoed event disappears from the results. The 8-char keys in *Rejections* call `searchFor()`.

**Long-term stats** (`internal/stats`): table `activity_hourly(hour, metric, n)` in the same sqlite file (own connection, WAL).
`activityLog` accumulates per-hour *deltas* (`bumpLocked`/`TakeDeltas`) and `Server.statsLoop` flushes them every minute with an
**additive** upsert (`n = n + excluded.n`) — never write absolute in-memory totals, a restart mid-hour would overwrite the hour —
plus gauges with `MAX` upsert (`max:conns`, `max:db_bytes`, every minute; `max:events` = `SELECT COUNT(*)` only every 15 min).
Final flush on `Close` (waits on `done`). Pruned after 365 days, daily. `Stats.History(range)` zero-fills and groups: `24h`/`7d`
hourly, `30d`/`90d` daily UTC; served by `GET /admin/api/history?range=` (session required, 400 on unknown range). **Counters
only — a test asserts no content/keys/IPs appear.** Frontend: tabs in the activity panel; don't name a top-level JS variable
`history` (it's `window.history`). `Server.FlushStats(bool)` exists so tests needn't wait a minute.

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
once verified that every `.card .l`, `#config dt`, `#mod h3`, `#hist-summary dt` (open a long range tab first), tab name, header button and
`#dash h2` title appears in the help text (46 labels) — re-run that check after changing the panel. The activity section must not claim the
whole history is in memory: only the 60 min view is; 24 h+ is persisted (`internal/stats`). The NIP list in the help is static
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
healthcheck script (pings healthchecks.io via `PING_URL`/`PING_FAIL_URL` set in the cron line; `PING_FAIL_URL` only fires after `ALERT_THRESHOLD`=2 consecutive failures so one blip doesn't email — the ping URL is a secret, it lives only in the crontab, never in git) runs every 2 minutes from the same crontab (`RELAY_URL=https://relay.hivescope.xyz`; log in
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
