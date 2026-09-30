package stats

import (
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

var now = time.Date(2026, 9, 30, 15, 30, 0, 0, time.UTC)

func TestAddIsAdditiveAcrossRestarts(t *testing.T) {
	s, path := open(t)
	h := Hour(now)
	s.Add(map[int64]map[string]int{h: {"saved": 3, "rejected": 2, "rej:rate-limited": 2}})
	s.Close()

	s2, err := Open(path) // "reinicio" a mitad de hora
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	s2.Add(map[int64]map[string]int{h: {"saved": 4, "rej:rate-limited": 1, "rejected": 1}})

	res, err := s2.History("24h", now)
	if err != nil {
		t.Fatal(err)
	}
	last := res.Buckets[len(res.Buckets)-1]
	if last.T != h || last.Saved != 7 || last.Rejected != 3 || res.Reasons["rate-limited"] != 3 {
		t.Fatalf("lo guardado antes del reinicio no debe pisarse: %+v %v", last, res.Reasons)
	}
}

func TestMaxKeepsTheHighestValueSeen(t *testing.T) {
	s, _ := open(t)
	h := Hour(now)
	s.SetMax(h, map[string]int64{MaxConns: 10, MaxDBBytes: 5000})
	s.SetMax(h, map[string]int64{MaxConns: 4, MaxDBBytes: 6000})
	res, _ := s.History("24h", now)
	b := res.Buckets[len(res.Buckets)-1]
	if b.MaxConns != 10 || b.DBBytes != 6000 {
		t.Fatalf("el máximo no baja: %+v", b)
	}
}

func TestHistoryRangesFillZerosAndGroupDays(t *testing.T) {
	s, _ := open(t)
	d := map[int64]map[string]int{
		Hour(now):                            {"saved": 1},
		Hour(now.Add(-2 * time.Hour)):        {"saved": 2},
		Hour(now.Add(-30 * time.Hour)):       {"saved": 4},  // ayer
		Hour(now.Add(-100 * 24 * time.Hour)): {"saved": 99}, // fuera del rango de 90 días
	}
	s.Add(d)

	h24, _ := s.History("24h", now)
	if len(h24.Buckets) != 24 || h24.Step != 3600 || h24.Totals.Saved != 3 {
		t.Fatalf("24h: %d tramos, step %d, total %d (lo de hace 30 h queda fuera)", len(h24.Buckets), h24.Step, h24.Totals.Saved)
	}
	if h24.Buckets[0].T != Hour(now)-23*3600 || h24.Buckets[23].T != Hour(now) {
		t.Fatal("los tramos van de la hora más vieja a la actual")
	}

	d30, _ := s.History("30d", now)
	if len(d30.Buckets) != 30 || d30.Step != 86400 || d30.Totals.Saved != 7 {
		t.Fatalf("30d: %d tramos, total %d", len(d30.Buckets), d30.Totals.Saved)
	}
	today, yesterday := d30.Buckets[29], d30.Buckets[28]
	if today.Saved != 3 || yesterday.Saved != 4 {
		t.Fatalf("agrupado por días UTC: hoy %d, ayer %d", today.Saved, yesterday.Saved)
	}

	d90, _ := s.History("90d", now)
	if d90.Totals.Saved != 7 {
		t.Fatalf("lo de hace 100 días queda fuera de 90d: %d", d90.Totals.Saved)
	}
}

func TestGrowthAndValidationAndPrune(t *testing.T) {
	s, _ := open(t)
	s.SetMax(Hour(now.Add(-48*time.Hour)), map[string]int64{MaxDBBytes: 1000, MaxEvents: 10})
	s.SetMax(Hour(now), map[string]int64{MaxDBBytes: 4000, MaxEvents: 25})
	res, _ := s.History("7d", now)
	if res.DBStart != 1000 || res.DBEnd != 4000 || res.EventsStart != 10 || res.EventsEnd != 25 {
		t.Fatalf("crecimiento del periodo: %+v", res)
	}
	if _, err := s.History("1y", now); err == nil {
		t.Fatal("un rango desconocido se rechaza")
	}
	s.Prune(Hour(now) - 24*3600)
	res, _ = s.History("7d", now)
	if res.DBStart != 4000 {
		t.Fatalf("Prune borra lo anterior: %+v", res)
	}
}
