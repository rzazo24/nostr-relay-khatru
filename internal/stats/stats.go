// Package stats guarda la actividad del relé en la propia base de datos, agregada por horas, para que
// el panel de control pueda enseñar más que la última hora (el histórico por minuto vive solo en
// memoria y se pierde al reiniciar). Solo son CONTADORES: nada de contenido, claves ni IPs.
//
// Tabla activity_hourly(hour, metric, n):
//   - contadores ("saved", "ephemeral", "authenticated", "rejected", "rej:<motivo>"): se SUMAN, así un
//     reinicio a mitad de hora no pisa lo ya guardado;
//   - medidas ("max:conns", "max:db_bytes", "max:events"): se guarda el MÁXIMO visto en esa hora.
package stats

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	MaxConns   = "max:conns"
	MaxDBBytes = "max:db_bytes"
	MaxEvents  = "max:events"
)

// Hour devuelve el inicio (unix) de la hora a la que pertenece t.
func Hour(t time.Time) int64 { return t.Unix() / 3600 * 3600 }

type Store struct{ db *sql.DB }

// Open abre (o crea) la tabla en el archivo SQLite `path` (el mismo que usa el almacén de eventos).
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS activity_hourly (hour INTEGER NOT NULL, metric TEXT NOT NULL, n INTEGER NOT NULL, PRIMARY KEY (hour, metric)) WITHOUT ROWID`); err != nil {
		db.Close()
		return nil, fmt.Errorf("stats: creando la tabla: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Add suma `deltas` a los contadores de cada hora, en una sola transacción.
func (s *Store) Add(deltas map[int64]map[string]int) error {
	if len(deltas) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO activity_hourly (hour, metric, n) VALUES (?, ?, ?) ON CONFLICT(hour, metric) DO UPDATE SET n = n + excluded.n`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for hour, m := range deltas {
		for metric, n := range m {
			if n == 0 {
				continue
			}
			if _, err := stmt.Exec(hour, metric, n); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

// SetMax guarda, para una hora, el máximo entre lo que ya había y lo nuevo.
func (s *Store) SetMax(hour int64, values map[string]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO activity_hourly (hour, metric, n) VALUES (?, ?, ?) ON CONFLICT(hour, metric) DO UPDATE SET n = MAX(n, excluded.n)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for metric, n := range values {
		if _, err := stmt.Exec(hour, metric, n); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Prune borra lo anterior a `before` (inicio de hora, unix).
func (s *Store) Prune(before int64) error {
	_, err := s.db.Exec(`DELETE FROM activity_hourly WHERE hour < ?`, before)
	return err
}

// raw devuelve hora -> métrica -> valor para las horas de [from, to].
func (s *Store) raw(from, to int64) (map[int64]map[string]int64, error) {
	rows, err := s.db.Query(`SELECT hour, metric, n FROM activity_hourly WHERE hour >= ? AND hour <= ?`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[string]int64{}
	for rows.Next() {
		var h, n int64
		var metric string
		if err := rows.Scan(&h, &metric, &n); err != nil {
			return nil, err
		}
		if out[h] == nil {
			out[h] = map[string]int64{}
		}
		out[h][metric] = n
	}
	return out, rows.Err()
}

// Bucket es un tramo (una hora o un día) del histórico.
type Bucket struct {
	T             int64 `json:"t"` // inicio del tramo (unix, UTC)
	Saved         int64 `json:"saved"`
	Ephemeral     int64 `json:"ephemeral"`
	Rejected      int64 `json:"rejected"`
	Authenticated int64 `json:"authenticated"`
	MaxConns      int64 `json:"maxConns"`
	DBBytes       int64 `json:"dbBytes"` // máximo de la medida en el tramo (0 = sin datos)
	Events        int64 `json:"events"`
}

// Result es el histórico de un periodo con sus totales.
type Result struct {
	Range   string           `json:"range"`
	Step    int64            `json:"step"` // segundos de cada tramo: 3600 o 86400
	Buckets []Bucket         `json:"buckets"`
	Totals  Bucket           `json:"totals"`
	Reasons map[string]int64 `json:"reasons"`
	// Primer y último dato de tamaño de base de datos y de eventos guardados del periodo (0 = sin datos):
	// su diferencia es cuánto ha crecido.
	DBStart     int64 `json:"dbStart"`
	DBEnd       int64 `json:"dbEnd"`
	EventsStart int64 `json:"eventsStart"`
	EventsEnd   int64 `json:"eventsEnd"`
}

// Ranges admitidos: nombre -> (nº de tramos, segundos por tramo).
var Ranges = map[string][2]int64{
	"24h": {24, 3600},
	"7d":  {168, 3600},
	"30d": {30, 86400},
	"90d": {90, 86400},
}

// History devuelve el periodo `name` terminando en la hora/día de `now`, con ceros en los tramos sin datos.
func (s *Store) History(name string, now time.Time) (*Result, error) {
	r, ok := Ranges[name]
	if !ok {
		return nil, fmt.Errorf("range must be one of 24h, 7d, 30d, 90d")
	}
	count, step := r[0], r[1]
	end := now.Unix() / step * step // inicio del tramo actual
	start := end - (count-1)*step
	raw, err := s.raw(start, end+step-1)
	if err != nil {
		return nil, err
	}
	res := &Result{Range: name, Step: step, Buckets: make([]Bucket, count), Reasons: map[string]int64{}}
	for i := range res.Buckets {
		res.Buckets[i].T = start + int64(i)*step
	}
	for hour, m := range raw {
		idx := (hour/step*step - start) / step
		if idx < 0 || idx >= count {
			continue
		}
		b := &res.Buckets[idx]
		for metric, n := range m {
			switch {
			case metric == "saved":
				b.Saved += n
			case metric == "ephemeral":
				b.Ephemeral += n
			case metric == "rejected":
				b.Rejected += n
			case metric == "authenticated":
				b.Authenticated += n
			case metric == MaxConns:
				b.MaxConns = max(b.MaxConns, n)
			case metric == MaxDBBytes:
				b.DBBytes = max(b.DBBytes, n)
			case metric == MaxEvents:
				b.Events = max(b.Events, n)
			case strings.HasPrefix(metric, "rej:"):
				res.Reasons[strings.TrimPrefix(metric, "rej:")] += n
			}
		}
	}
	for _, b := range res.Buckets {
		res.Totals.Saved += b.Saved
		res.Totals.Ephemeral += b.Ephemeral
		res.Totals.Rejected += b.Rejected
		res.Totals.Authenticated += b.Authenticated
		res.Totals.MaxConns = max(res.Totals.MaxConns, b.MaxConns)
	}
	for _, b := range res.Buckets {
		if b.DBBytes > 0 {
			if res.DBStart == 0 {
				res.DBStart = b.DBBytes
			}
			res.DBEnd = b.DBBytes
		}
		if b.Events > 0 {
			if res.EventsStart == 0 {
				res.EventsStart = b.Events
			}
			res.EventsEnd = b.Events
		}
	}
	return res, nil
}

// EventCount cuenta los eventos guardados (se muestrea cada poco, no en cada petición).
func (s *Store) EventCount() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM event`).Scan(&n)
	return n, err
}
