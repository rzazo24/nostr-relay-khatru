package admin

import (
	"compress/gzip"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
)

// backupBusy impide preparar dos copias a la vez (cada una escribe un archivo temporal del tamaño de la base de datos).
var backupBusy atomic.Bool

// backup entrega una copia consistente de la base de datos, comprimida (.sqlite.gz, con el mismo nombre que las de
// scripts/backup-db.sh, así que scripts/restore-db.sh la acepta). Se hace con VACUUM INTO, que es seguro mientras el
// relé sigue escribiendo; el archivo temporal se crea junto a la base de datos y se borra al terminar.
func (p *Panel) backup(w http.ResponseWriter, r *http.Request) {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		fail(w, http.StatusForbidden, "cross-site request refused")
		return
	}
	if p.db == nil || p.o.DBPath == "" {
		fail(w, http.StatusServiceUnavailable, "backup is not available")
		return
	}
	if !backupBusy.CompareAndSwap(false, true) {
		fail(w, http.StatusTooManyRequests, "a backup is already being prepared, try again in a moment")
		return
	}
	defer backupBusy.Store(false)

	var nonce [6]byte
	rand.Read(nonce[:])
	tmp := filepath.Join(filepath.Dir(p.o.DBPath), ".panel-backup-"+hex.EncodeToString(nonce[:])+".sqlite")
	defer os.Remove(tmp)
	// Conexión propia y de solo lectura (mode=ro): la del panel usa _query_only, que SQLite no deja combinar con VACUUM INTO.
	// El original no se modifica; la copia se escribe en `tmp`.
	src, err := sql.Open("sqlite3", "file:"+p.o.DBPath+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not open the database")
		return
	}
	defer src.Close()
	if _, err := src.ExecContext(r.Context(), `VACUUM INTO ?`, tmp); err != nil {
		fail(w, http.StatusInternalServerError, "could not prepare the backup")
		return
	}
	f, err := os.Open(tmp)
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not read the backup")
		return
	}
	defer f.Close()

	name := "nostr-relay-khatru-" + p.o.Now().UTC().Format("20060102T150405Z") + ".sqlite.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	zw := gzip.NewWriter(w)
	_, cerr := io.Copy(zw, f)
	if err := zw.Close(); cerr == nil {
		cerr = err
	}
	if cerr != nil {
		return // la descarga se cortó (cliente cerrado): no se anota como hecha
	}
	fi, _ := f.Stat()
	size := int64(0)
	if fi != nil {
		size = fi.Size()
	}
	p.record("backup", "", "", fmt.Sprintf("%s (base de datos de %.1f MB sin comprimir)", name, float64(size)/1048576))
}
