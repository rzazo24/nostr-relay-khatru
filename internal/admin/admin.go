// Package admin implementa el panel de control del relé: una API de solo lectura con
// las estadísticas y la actividad reciente, protegida para que solo entre el dueño
// (RELAY_PUBKEY). La entrada es una firma Nostr (NIP-98) que se cambia por una sesión
// corta en una cookie; la interfaz web está en static/admin (la sirve Caddy).
package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nbd-wtf/go-nostr"

	"github.com/rzazo24/nostr-relay-khatru/internal/moderation"
	"github.com/rzazo24/nostr-relay-khatru/internal/stats"
)

const (
	cookieName      = "hs_admin"
	sessionLifetime = time.Hour
	maxSessions     = 5
	// NIP-98 admite una desviación de reloj de 60 s; se recuerda cada firma usada un poco
	// más para que no se pueda reutilizar (repetir) mientras siga siendo válida.
	authWindow = 60 * time.Second
	replayTTL  = 3 * time.Minute
	// Intentos de inicio de sesión por IP y minuto (evita probar firmas a lo loco).
	loginAttemptsPerMinute = 10
)

// Minute es la actividad de un minuto concreto.
type Minute struct {
	T             int64 `json:"t"` // inicio del minuto (unix)
	Saved         int   `json:"saved"`
	Ephemeral     int   `json:"ephemeral"`
	Rejected      int   `json:"rejected"`
	Authenticated int   `json:"authenticated"`
}

// Rejection es un rechazo reciente (sin contenido ni IPs).
type Rejection struct {
	T      int64  `json:"t"`
	What   string `json:"what"` // "event" o "filter"
	Kind   int    `json:"kind"`
	Pubkey string `json:"pubkey"` // solo un trozo corto
	Reason string `json:"reason"`
}

// NoisyKey es una clave de la que el relé ha rechazado eventos (recuento desde el arranque, solo en memoria).
type NoisyKey struct {
	Pubkey string `json:"pubkey"` // completo: solo se manda al dueño con sesión, para poder banearla o buscarla
	Count  int    `json:"count"`
	Kind   int    `json:"kind"`   // tipo del último evento rechazado
	Reason string `json:"reason"` // motivo más frecuente
	Last   int64  `json:"last"`
	Mine   bool   `json:"mine"`
}

// Activity es lo que el panel necesita del registro de actividad del servidor.
type Activity interface {
	Noisy(limit int) []NoisyKey
	Minutes(now time.Time, n int) []Minute
	Rejections(limit int) []Rejection
	ReasonTotals() map[string]int
}

// Moderation es lo que el panel necesita de las listas de moderación.
type Moderation interface {
	CountBannedPubKeys() int
	CountAllowedPubKeys() int
	CountBannedEvents() int
	CountBlockedIPs() int
	AllowedKinds() []int
	DisallowedKinds() []int
	// FirstSeen dice cuándo (unix) vio el relé por primera vez una clave (para la insignia «nueva» de los eventos).
	FirstSeen(pubkey string) (int64, bool)
}

// Options agrupa lo que necesita el panel.
type Options struct {
	Owner        string // pubkey (hex) del dueño; vacío = panel desactivado
	PublicURL    string // URL pública https (si vacía, se deduce de la petición)
	DBPath       string
	BackupDir    string // dónde deja scripts/backup-db.sh las copias (vacío = no se muestra el estado de las copias)
	Version      string
	StartedAt    time.Time
	Activity     Activity
	Moderation   Moderation
	Stats        *stats.Store      // histórico persistente (más de una hora)
	Store        *moderation.Store // para las acciones de moderación del panel
	Effects      Effects
	Info         func() InfoView             // nombre, descripción e icono que anuncia NIP-11 ahora mismo
	InfoDefaults InfoView                    // los de la configuración (a los que vuelve "restaurar")
	Log          func(action, target string) // deja constancia (sin contenido ni IPs) de cada acción
	Connections  func() int64
	NewKeyHours  int            // RELAY_NEW_KEY_HOURS: la insignia «nueva» dura al menos 24 h, o este periodo si es más largo
	Config       map[string]any // límites y NIPs, tal cual se muestran (solo lectura)
	Now          func() time.Time
}

// Panel atiende /admin/api/*.
type Panel struct {
	o  Options
	db *sql.DB

	mu       sync.Mutex
	sessions map[string]time.Time // token -> caducidad
	used     map[string]time.Time // id de firma -> hasta cuándo se recuerda
	attempts map[string][]time.Time

	cacheAt  time.Time
	cacheVal *eventStats

	// /stats.json (público): documento ya serializado y cuándo se calculó
	pubMu   sync.Mutex
	pubAt   time.Time
	pubBody []byte
}

// New abre la base de datos en modo solo consulta y prepara el panel.
func New(o Options) (*Panel, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	p := &Panel{o: o, sessions: map[string]time.Time{}, used: map[string]time.Time{}, attempts: map[string][]time.Time{}}
	// Se abre aunque no haya dueño (el panel queda desactivado, pero /stats.json es público y necesita leer).
	// `_query_only` impide cualquier escritura desde este panel, aunque hubiera un fallo.
	db, err := sql.Open("sqlite3", "file:"+o.DBPath+"?_busy_timeout=5000&_query_only=true")
	if err != nil {
		return nil, err
	}
	p.db = db
	return p, nil
}

func (p *Panel) Close() {
	if p.db != nil {
		p.db.Close()
	}
}

// Mount registra las rutas en el mux del relé.
func (p *Panel) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /admin/api/login", p.login)
	mux.HandleFunc("POST /admin/api/logout", p.logout)
	mux.HandleFunc("GET /admin/api/session", p.requireSession(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]any{"ok": true}) }))
	mux.HandleFunc("GET /admin/api/stats", p.requireSession(p.stats))
	mux.HandleFunc("GET /admin/api/history", p.requireSession(p.history))
	mux.HandleFunc("GET /admin/api/search", p.requireSession(p.search))
	mux.HandleFunc("GET /admin/api/backup", p.requireSession(p.backup))
	mux.HandleFunc("GET /stats.json", p.publicStats) // público a propósito: solo agregados (ver publicstats.go)
	p.mountModeration(mux)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ---------- inicio de sesión (NIP-98) ----------

func (p *Panel) login(w http.ResponseWriter, r *http.Request) {
	if p.o.Owner == "" {
		fail(w, http.StatusForbidden, "the admin panel is disabled (RELAY_PUBKEY is not set)")
		return
	}
	now := p.o.Now()
	if !p.allowAttempt(clientIP(r), now) {
		fail(w, http.StatusTooManyRequests, "too many attempts, wait a minute")
		return
	}
	ev, err := parseAuthHeader(r.Header.Get("Authorization"))
	if err != nil {
		fail(w, http.StatusUnauthorized, err.Error())
		return
	}
	if msg := p.validateAuth(ev, r, now); msg != "" {
		fail(w, http.StatusUnauthorized, msg)
		return
	}

	token := randomToken()
	p.mu.Lock()
	p.used[ev.ID] = now.Add(replayTTL)
	p.pruneLocked(now)
	if len(p.sessions) >= maxSessions { // se descarta la más antigua
		var oldest string
		for t, exp := range p.sessions {
			if oldest == "" || exp.Before(p.sessions[oldest]) {
				oldest = t
			}
		}
		delete(p.sessions, oldest)
	}
	p.sessions[token] = now.Add(sessionLifetime)
	p.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/admin", MaxAge: int(sessionLifetime.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r),
	})
	p.record("login", "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "expiresIn": int(sessionLifetime.Seconds())})
}

func (p *Panel) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		p.mu.Lock()
		_, had := p.sessions[c.Value]
		delete(p.sessions, c.Value)
		p.mu.Unlock()
		if had { // sin sesión no se anota nada (cualquiera puede llamar a este endpoint)
			p.record("logout", "", "", "")
		}
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/admin", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p.o.Owner == "" {
			fail(w, http.StatusForbidden, "the admin panel is disabled (RELAY_PUBKEY is not set)")
			return
		}
		token := ""
		if c, err := r.Cookie(cookieName); err == nil {
			token = c.Value
		}
		now := p.o.Now()
		p.mu.Lock()
		exp, ok := p.sessions[token]
		if ok && now.After(exp) {
			ok = false
		}
		p.mu.Unlock()
		if !ok {
			fail(w, http.StatusUnauthorized, "not logged in")
			return
		}
		next(w, r)
	}
}

func parseAuthHeader(h string) (*nostr.Event, error) {
	const prefix = "Nostr "
	if !strings.HasPrefix(h, prefix) {
		return nil, errString("missing Authorization: Nostr <base64 event>")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(h, prefix))
	if err != nil {
		return nil, errString("the auth event is not valid base64")
	}
	var ev nostr.Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, errString("the auth event is not valid JSON")
	}
	return &ev, nil
}

type errString string

func (e errString) Error() string { return string(e) }

// validateAuth comprueba la firma NIP-98: evento kind 27235 del dueño, para esta URL y
// método, reciente y no usado antes. Devuelve "" si es válido, o el motivo.
func (p *Panel) validateAuth(ev *nostr.Event, r *http.Request, now time.Time) string {
	if ev.Kind != 27235 {
		return "the auth event must be kind 27235 (NIP-98)"
	}
	if ok, err := ev.CheckSignature(); err != nil || !ok {
		return "invalid signature"
	}
	if ev.PubKey != p.o.Owner {
		return "unauthorized: only the relay owner can log in"
	}
	if d := now.Sub(ev.CreatedAt.Time()); d > authWindow || d < -authWindow {
		return "the auth event is too old or too far in the future"
	}
	u := ev.Tags.GetFirst([]string{"u"})
	if u == nil || len(*u) < 2 || normalizeURL((*u)[1]) != normalizeURL(p.expectedURL(r)) {
		return "the \"u\" tag doesn't match this URL"
	}
	m := ev.Tags.GetFirst([]string{"method"})
	if m == nil || len(*m) < 2 || !strings.EqualFold((*m)[1], r.Method) {
		return "the \"method\" tag doesn't match"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if until, seen := p.used[ev.ID]; seen && now.Before(until) {
		return "this signature was already used"
	}
	return ""
}

func (p *Panel) expectedURL(r *http.Request) string {
	if p.o.PublicURL != "" {
		return strings.TrimRight(p.o.PublicURL, "/") + r.URL.Path
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host + r.URL.Path
}

func normalizeURL(u string) string { return strings.TrimRight(strings.ToLower(u), "/") }

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	return r.RemoteAddr
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (p *Panel) allowAttempt(ip string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	recent := p.attempts[ip][:0]
	for _, t := range p.attempts[ip] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= loginAttemptsPerMinute {
		p.attempts[ip] = recent
		return false
	}
	p.attempts[ip] = append(recent, now)
	return true
}

func (p *Panel) pruneLocked(now time.Time) {
	for id, until := range p.used {
		if now.After(until) {
			delete(p.used, id)
		}
	}
	for t, exp := range p.sessions {
		if now.After(exp) {
			delete(p.sessions, t)
		}
	}
	for ip, list := range p.attempts {
		if len(list) == 0 || now.Sub(list[len(list)-1]) > time.Minute {
			delete(p.attempts, ip)
		}
	}
}

// ---------- estadísticas ----------

type kindCount struct {
	Kind  int `json:"kind"`
	Count int `json:"count"`
}

type recentEvent struct {
	ID        string `json:"id"`
	PubKey    string `json:"pubkey"`
	Kind      int    `json:"kind"`
	CreatedAt int64  `json:"createdAt"`
	Content   string `json:"content,omitempty"`
	Mine      bool   `json:"mine"`
	NewKey    bool   `json:"newKey"` // el relé vio esta clave por primera vez hace poco (ver newKeyBadge)
}

type dayCount struct {
	Day   string `json:"day"` // AAAA-MM-DD (UTC)
	Count int    `json:"count"`
}

type eventStats struct {
	Total   int           `json:"total"`
	PubKeys int           `json:"pubkeys"`
	ByKind  []kindCount   `json:"byKind"`
	Oldest  int64         `json:"oldest"`
	Newest  int64         `json:"newest"`
	Last24h int           `json:"last24h"`
	PerDay  []dayCount    `json:"perDay"`
	Recent  []recentEvent `json:"recent"`
}

// Kinds cuyo contenido no se muestra en el panel (mensajes privados).
var privateKinds = map[int]bool{4: true, 1059: true, 14: true, 13: true}

func (p *Panel) eventStats(ctx context.Context, now time.Time) (*eventStats, error) {
	p.mu.Lock()
	if p.cacheVal != nil && now.Sub(p.cacheAt) < 10*time.Second { // no se repiten las consultas a cada petición
		v := p.cacheVal
		p.mu.Unlock()
		return v, nil
	}
	p.mu.Unlock()

	st := &eventStats{ByKind: []kindCount{}, Recent: []recentEvent{}, PerDay: []dayCount{}}
	row := p.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(DISTINCT pubkey), COALESCE(MIN(created_at),0), COALESCE(MAX(created_at),0) FROM event`)
	if err := row.Scan(&st.Total, &st.PubKeys, &st.Oldest, &st.Newest); err != nil {
		return nil, err
	}
	if err := p.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event WHERE created_at >= ?`, now.Add(-24*time.Hour).Unix()).Scan(&st.Last24h); err != nil {
		return nil, err
	}
	// Eventos guardados por día de creación (UTC), los últimos 14: sirve para ver el ritmo de crecimiento.
	drows, err := p.db.QueryContext(ctx, `SELECT strftime('%Y-%m-%d', created_at, 'unixepoch') AS d, COUNT(*) FROM event WHERE created_at >= ? GROUP BY d ORDER BY d`, now.Add(-14*24*time.Hour).Unix())
	if err != nil {
		return nil, err
	}
	for drows.Next() {
		var d dayCount
		if err := drows.Scan(&d.Day, &d.Count); err != nil {
			drows.Close()
			return nil, err
		}
		st.PerDay = append(st.PerDay, d)
	}
	drows.Close()

	rows, err := p.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM event GROUP BY kind ORDER BY COUNT(*) DESC, kind LIMIT 30`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k kindCount
		if err := rows.Scan(&k.Kind, &k.Count); err != nil {
			rows.Close()
			return nil, err
		}
		st.ByKind = append(st.ByKind, k)
	}
	rows.Close()

	rows, err = p.db.QueryContext(ctx, `SELECT id, pubkey, kind, created_at, content FROM event ORDER BY created_at DESC LIMIT 30`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e recentEvent
		var content string
		if err := rows.Scan(&e.ID, &e.PubKey, &e.Kind, &e.CreatedAt, &content); err != nil {
			rows.Close()
			return nil, err
		}
		e.Mine = e.PubKey == p.o.Owner
		if !privateKinds[e.Kind] {
			e.Content = snippet(content, 200)
		}
		st.Recent = append(st.Recent, e)
	}
	rows.Close()

	p.mu.Lock()
	p.cacheAt, p.cacheVal = now, st
	p.mu.Unlock()
	return st, nil
}

// snippet recorta el contenido y quita los caracteres de control (el panel lo muestra como texto).
func snippet(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= max {
			b.WriteString("…")
			break
		}
		if r < 0x20 && r != '\n' {
			continue
		}
		if r == '\n' {
			r = ' '
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func (p *Panel) dbBytes() int64 {
	var total int64
	for _, suffix := range []string{"", "-wal"} {
		if fi, err := os.Stat(p.o.DBPath + suffix); err == nil {
			total += fi.Size()
		}
	}
	return total
}

func (p *Panel) stats(w http.ResponseWriter, r *http.Request) {
	now := p.o.Now()
	ev, err := p.eventStats(r.Context(), now)
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not read the statistics")
		return
	}
	ev = p.markNewKeys(ev, now)
	var conns int64
	if p.o.Connections != nil {
		conns = p.o.Connections()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"now":         now.Unix(),
		"version":     p.o.Version,
		"startedAt":   p.o.StartedAt.Unix(),
		"connections": conns,
		"dbBytes":     p.dbBytes(),
		"server":      p.serverStatus(),
		"events":      ev,
		"activity": map[string]any{
			"minutes":    p.o.Activity.Minutes(now, 60),
			"reasons":    p.o.Activity.ReasonTotals(),
			"rejections": p.o.Activity.Rejections(40),
			"noisy":      markMine(p.o.Activity.Noisy(10), p.o.Owner),
		},
		"moderation": map[string]any{
			"bannedPubkeys":   p.o.Moderation.CountBannedPubKeys(),
			"allowedPubkeys":  p.o.Moderation.CountAllowedPubKeys(),
			"bannedEvents":    p.o.Moderation.CountBannedEvents(),
			"blockedIPs":      p.o.Moderation.CountBlockedIPs(),
			"allowedKinds":    nonNil(p.o.Moderation.AllowedKinds()),
			"disallowedKinds": nonNil(p.o.Moderation.DisallowedKinds()),
		},
		"config": p.o.Config,
	})
}

// history devuelve la actividad de un periodo largo (24h, 7d, 30d o 90d) a partir de lo persistido.
func (p *Panel) history(w http.ResponseWriter, r *http.Request) {
	if p.o.Stats == nil {
		fail(w, http.StatusServiceUnavailable, "the history is not available")
		return
	}
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "24h"
	}
	res, err := p.o.Stats.History(name, p.o.Now())
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// markMine marca la clave del dueño (el panel no ofrece banearla) y nunca devuelve null.
func markMine(keys []NoisyKey, owner string) []NoisyKey {
	if keys == nil {
		return []NoisyKey{}
	}
	for i := range keys {
		keys[i].Mine = keys[i].Pubkey == owner
	}
	return keys
}

// newKeyBadge es cuánto tiempo se considera «nueva» una clave en el panel: 24 h como mínimo, o el periodo de prueba si es más largo.
func (p *Panel) newKeyBadge() time.Duration {
	return max(24*time.Hour, time.Duration(p.o.NewKeyHours)*time.Hour)
}

// isNewKey dice si el relé vio esta clave por primera vez hace menos de newKeyBadge. El dueño nunca consta como nueva.
func (p *Panel) isNewKey(pubkey string, now time.Time) bool {
	if pubkey == p.o.Owner || p.o.Moderation == nil {
		return false
	}
	first, ok := p.o.Moderation.FirstSeen(pubkey)
	return ok && now.Sub(time.Unix(first, 0)) < p.newKeyBadge()
}

// markNewKeys devuelve una copia de las estadísticas con la insignia «nueva» puesta en los eventos recientes. Se calcula aquí y no dentro
// de la caché de eventStats porque depende de la hora y de lo que el relé vaya viendo; y sobre una copia, porque la caché se comparte.
func (p *Panel) markNewKeys(ev *eventStats, now time.Time) *eventStats {
	cp := *ev
	cp.Recent = make([]recentEvent, len(ev.Recent))
	for i, e := range ev.Recent {
		e.NewKey = p.isNewKey(e.PubKey, now)
		cp.Recent[i] = e
	}
	return &cp
}
