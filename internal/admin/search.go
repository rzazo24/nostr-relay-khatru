package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/nbd-wtf/go-nostr/nip19"
)

const (
	searchPageSize  = 50
	searchCountCap  = 10000 // el contador se detiene aquí ("más de 10 000")
	searchSnippet   = 400
	maxQueryLength  = 200
	minPrefixLength = 6 // para buscar por el principio de una clave o un id
)

var hexPrefix = regexp.MustCompile(`^[0-9a-f]+$`)

// searchPlan es lo que se ha entendido de lo que escribió el dueño.
type searchPlan struct {
	where  []string
	args   []any
	what   string // descripción para mostrar ("eventos de la clave …")
	pubkey string // si la búsqueda se reduce a UNA clave completa, para el resumen de la clave
}

// likeEscape escapa los comodines de LIKE para buscar el texto tal cual.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// planSearch interpreta la consulta: npub / nprofile / note1 / nevent1, hex de 64 caracteres (clave o id),
// principio de una clave o id (hex, mínimo 6), número de kind, o texto a buscar en el contenido.
func planSearch(q string, kind *int) (*searchPlan, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) > maxQueryLength {
		return nil, fmt.Errorf("the search is too long (maximum %d characters)", maxQueryLength)
	}
	pl := &searchPlan{}
	lower := strings.ToLower(q)

	switch {
	case q == "":
		pl.what = "todos los eventos"
	case strings.HasPrefix(lower, "npub1") || strings.HasPrefix(lower, "nprofile1"):
		pk, err := decodePubkey(q)
		if err != nil {
			return nil, err
		}
		pl.where, pl.args, pl.pubkey, pl.what = []string{"pubkey = ?"}, []any{pk}, pk, "eventos de la clave "+pk[:12]+"…"
	case strings.HasPrefix(lower, "note1") || strings.HasPrefix(lower, "nevent1"):
		id, err := decodeEventID(q)
		if err != nil {
			return nil, err
		}
		pl.where, pl.args, pl.what = []string{"id = ?"}, []any{id}, "el evento "+id[:12]+"…"
	case hex64.MatchString(lower):
		// 64 caracteres hex: puede ser una clave o un id; se buscan las dos cosas
		pl.where, pl.args, pl.pubkey, pl.what = []string{"(pubkey = ? OR id = ?)"}, []any{lower, lower}, lower, "la clave o el id "+lower[:12]+"…"
	case len(lower) >= minPrefixLength && len(lower) < 64 && hexPrefix.MatchString(lower):
		pl.where, pl.args, pl.what = []string{"(pubkey LIKE ? ESCAPE '\\' OR id LIKE ? ESCAPE '\\')"}, []any{lower + "%", lower + "%"}, "claves o ids que empiezan por "+lower
	default:
		if n, err := strconv.Atoi(q); err == nil && len(q) <= 5 && kind == nil {
			kind = &n
			pl.what = fmt.Sprintf("eventos de tipo %d", n)
		} else {
			// texto en el contenido; los mensajes privados no se miran (ni se enseñan)
			pl.where = append(pl.where, `content LIKE ? ESCAPE '\'`, "kind NOT IN (4, 13, 14, 1059)")
			pl.args = append(pl.args, "%"+likeEscape(q)+"%")
			pl.what = fmt.Sprintf("eventos cuyo contenido contiene «%s»", q)
		}
	}
	if kind != nil {
		pl.where = append(pl.where, "kind = ?")
		pl.args = append(pl.args, *kind)
		if q != "" && !strings.Contains(pl.what, "tipo") {
			pl.what += fmt.Sprintf(" (tipo %d)", *kind)
		} else if q == "" {
			pl.what = fmt.Sprintf("eventos de tipo %d", *kind)
		}
	}
	return pl, nil
}

type searchResult struct {
	Query   string        `json:"query"`
	What    string        `json:"what"`
	Total   int           `json:"total"`
	TotalOK bool          `json:"totalExact"` // false = "más de searchCountCap"
	Events  []recentEvent `json:"events"`
	Next    string        `json:"next,omitempty"` // cursor de la página siguiente
	Key     *keySummary   `json:"key,omitempty"`
}

type keySummary struct {
	PubKey  string      `json:"pubkey"`
	Npub    string      `json:"npub"`
	Events  int         `json:"events"`
	First   int64       `json:"first"`
	Last    int64       `json:"last"`
	ByKind  []kindCount `json:"byKind"`
	Name    string      `json:"name,omitempty"` // del último perfil (kind 0) guardado
	Banned  bool        `json:"banned"`
	Allowed bool        `json:"allowed"`
	IsOwner bool        `json:"isOwner"`
}

func (p *Panel) search(w http.ResponseWriter, r *http.Request) {
	if p.db == nil {
		fail(w, http.StatusServiceUnavailable, "search is not available")
		return
	}
	var kind *int
	if ks := strings.TrimSpace(r.URL.Query().Get("kind")); ks != "" {
		n, err := strconv.Atoi(ks)
		if err != nil || n < 0 || n > 65535 {
			fail(w, http.StatusBadRequest, "kind must be an integer between 0 and 65535")
			return
		}
		kind = &n
	}
	q := r.URL.Query().Get("q")
	pl, err := planSearch(q, kind)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	// Rango de fechas opcional (segundos unix, ambos extremos incluidos). El panel manda el principio y el final de
	// los días elegidos en la hora local del navegador.
	var since, until int64 = -1, -1
	for name, dst := range map[string]*int64{"since": &since, "until": &until} {
		if v := strings.TrimSpace(r.URL.Query().Get(name)); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 || n > 1<<40 {
				fail(w, http.StatusBadRequest, name+" must be a unix timestamp in seconds")
				return
			}
			*dst = n
		}
	}
	if since >= 0 && until >= 0 && since > until {
		fail(w, http.StatusBadRequest, "since must not be after until")
		return
	}
	if since >= 0 {
		pl.where, pl.args = append(pl.where, "created_at >= ?"), append(pl.args, since)
	}
	if until >= 0 {
		pl.where, pl.args = append(pl.where, "created_at <= ?"), append(pl.args, until)
	}

	where := strings.Join(pl.where, " AND ")
	if where == "" {
		where = "1=1"
	}
	ctx := r.Context()
	res := &searchResult{Query: strings.TrimSpace(q), What: pl.what, Events: []recentEvent{}}

	// total (con tope, para no recorrer una base enorme solo por contar)
	cntArgs := append(append([]any{}, pl.args...), searchCountCap+1)
	if err := p.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT 1 FROM event WHERE `+where+` LIMIT ?)`, cntArgs...).Scan(&res.Total); err != nil {
		fail(w, http.StatusInternalServerError, "could not search")
		return
	}
	res.TotalOK = res.Total <= searchCountCap
	if !res.TotalOK {
		res.Total = searchCountCap
	}

	// página (cursor "created_at:id", del más nuevo al más viejo)
	pageWhere, args := where, append([]any{}, pl.args...)
	if cur := r.URL.Query().Get("next"); cur != "" {
		parts := strings.SplitN(cur, ":", 2)
		ts, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || len(parts) != 2 || !hex64.MatchString(parts[1]) {
			fail(w, http.StatusBadRequest, "invalid page cursor")
			return
		}
		pageWhere += " AND (created_at < ? OR (created_at = ? AND id < ?))"
		args = append(args, ts, ts, parts[1])
	}
	args = append(args, searchPageSize+1)
	rows, err := p.db.QueryContext(ctx, `SELECT id, pubkey, kind, created_at, content FROM event WHERE `+pageWhere+` ORDER BY created_at DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not search")
		return
	}
	var lastTS int64
	var lastID string
	for rows.Next() {
		var e recentEvent
		var content string
		if err := rows.Scan(&e.ID, &e.PubKey, &e.Kind, &e.CreatedAt, &content); err != nil {
			rows.Close()
			fail(w, http.StatusInternalServerError, "could not search")
			return
		}
		if len(res.Events) == searchPageSize { // la fila extra solo indica que hay más páginas
			res.Next = fmt.Sprintf("%d:%s", lastTS, lastID)
			break
		}
		e.Mine = e.PubKey == p.o.Owner
		if !privateKinds[e.Kind] {
			e.Content = snippet(content, searchSnippet)
		}
		res.Events = append(res.Events, e)
		lastTS, lastID = e.CreatedAt, e.ID
	}
	rows.Close()

	// resumen de la clave, solo en la primera página y si la búsqueda se reduce a una clave completa
	if pl.pubkey != "" && r.URL.Query().Get("next") == "" {
		res.Key = p.keySummary(ctx, pl.pubkey)
	}
	writeJSON(w, http.StatusOK, res)
}

// keySummary resume lo que el relé sabe de una clave: cuántos eventos guarda, desde cuándo, de qué tipos,
// el nombre de su último perfil y su estado de moderación. Devuelve nil si no hay nada de esa clave.
func (p *Panel) keySummary(ctx context.Context, pk string) *keySummary {
	ks := &keySummary{PubKey: pk, ByKind: []kindCount{}, IsOwner: pk == p.o.Owner}
	if npub, err := nip19.EncodePublicKey(pk); err == nil {
		ks.Npub = npub
	}
	if p.o.Store != nil {
		ks.Banned, ks.Allowed = p.o.Store.IsPubKeyBanned(pk), p.o.Store.IsPubKeyAllowed(pk)
	}
	row := p.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MIN(created_at),0), COALESCE(MAX(created_at),0) FROM event WHERE pubkey = ?`, pk)
	if err := row.Scan(&ks.Events, &ks.First, &ks.Last); err != nil {
		return nil
	}
	if rows, err := p.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM event WHERE pubkey = ? GROUP BY kind ORDER BY COUNT(*) DESC, kind LIMIT 12`, pk); err == nil {
		for rows.Next() {
			var k kindCount
			if rows.Scan(&k.Kind, &k.Count) == nil {
				ks.ByKind = append(ks.ByKind, k)
			}
		}
		rows.Close()
	}
	var profile string
	if err := p.db.QueryRowContext(ctx, `SELECT content FROM event WHERE pubkey = ? AND kind = 0 ORDER BY created_at DESC LIMIT 1`, pk).Scan(&profile); err == nil {
		var prof struct {
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
		}
		if json.Unmarshal([]byte(profile), &prof) == nil {
			ks.Name = snippet(strings.TrimSpace(firstNonEmpty(prof.DisplayName, prof.Name)), 60)
		}
	}
	if ks.Events == 0 && !ks.Banned && !ks.Allowed && !ks.IsOwner {
		return nil // nada que contar de esta clave (puede ser un id)
	}
	return ks
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
