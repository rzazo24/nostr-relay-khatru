package admin

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
)

// bunkerEntry es una línea del registro de hivescope-bunker: qué firmó, rechazó o emparejó (sin contenido ni claves).
type bunkerEntry struct {
	Time    int64  `json:"t"`
	Outcome string `json:"outcome"`
	Conn    string `json:"conn,omitempty"`
	Profile string `json:"profile,omitempty"`
	Kind    *int   `json:"kind,omitempty"`
	ID      string `json:"id,omitempty"`
	Method  string `json:"method,omitempty"`
	Client  string `json:"client,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

const bunkerShown = 300

// bunker devuelve las últimas firmas del bunker, la más reciente primero. El archivo lo escribe el bunker (fuera del contenedor)
// y llega montado en solo lectura; si no está configurado o no existe todavía, se dice en vez de fallar.
func (p *Panel) bunker(w http.ResponseWriter, r *http.Request) {
	if p.o.BunkerJournal == "" {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "reason": "not-configured"})
		return
	}
	f, err := os.Open(p.o.BunkerJournal)
	if errors.Is(err, fs.ErrNotExist) {
		writeJSON(w, http.StatusOK, map[string]any{"available": true, "entries": []bunkerEntry{}})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "reason": "unreadable"})
		return
	}
	defer f.Close()
	var all []bunkerEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var e bunkerEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Outcome != "" {
			all = append(all, e)
		}
	}
	if len(all) > bunkerShown {
		all = all[len(all)-bunkerShown:]
	}
	out := make([]bunkerEntry, len(all))
	for i, e := range all {
		out[len(all)-1-i] = e
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "entries": out})
}
