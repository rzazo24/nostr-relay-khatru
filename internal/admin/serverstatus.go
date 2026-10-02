package admin

import (
	"os"
	"path/filepath"
	"syscall"
)

// serverStatus es lo que el panel enseña del servidor donde corre el relé: el disco de la base de datos y
// la última copia de seguridad (si se le ha dicho dónde mirar con RELAY_BACKUP_DIR).
func (p *Panel) serverStatus() map[string]any {
	out := map[string]any{}
	var st syscall.Statfs_t
	if err := syscall.Statfs(filepath.Dir(p.o.DBPath), &st); err == nil {
		out["diskTotal"] = int64(st.Blocks) * int64(st.Bsize)
		out["diskFree"] = int64(st.Bavail) * int64(st.Bsize)
	}
	backup := map[string]any{"configured": p.o.BackupDir != ""}
	if p.o.BackupDir != "" {
		last, count, size := latestBackup(p.o.BackupDir)
		backup["count"], backup["last"], backup["lastBytes"] = count, last, size
	}
	out["backup"] = backup
	return out
}

// latestBackup mira en `dir` los archivos nostr-relay-khatru-*.sqlite.gz (los que deja scripts/backup-db.sh) y
// devuelve la fecha (unix) y el tamaño del más reciente, y cuántos hay. Sin copias devuelve ceros.
func latestBackup(dir string) (last int64, count int, size int64) {
	files, _ := filepath.Glob(filepath.Join(dir, "nostr-relay-khatru-*.sqlite.gz"))
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		count++
		if t := fi.ModTime().Unix(); t > last {
			last, size = t, fi.Size()
		}
	}
	return last, count, size
}
