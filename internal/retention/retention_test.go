package retention

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

var now = time.Unix(1_800_000_000, 0)

func ago(days int) nostr.Timestamp {
	return nostr.Timestamp(now.Add(-time.Duration(days) * 24 * time.Hour).Unix())
}

type store struct {
	events  []*nostr.Event
	deleted []string
}

// Query imita al almacén: del más nuevo al más viejo, respetando Until y Limit.
func (s *store) Query(ctx context.Context, f nostr.Filter) (chan *nostr.Event, error) {
	var out []*nostr.Event
	for _, e := range s.events {
		if f.Until != nil && e.CreatedAt > *f.Until {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	ch := make(chan *nostr.Event, len(out))
	for _, e := range out {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func (s *store) Delete(ctx context.Context, e *nostr.Event) error {
	s.deleted = append(s.deleted, e.ID)
	kept := s.events[:0]
	for _, x := range s.events {
		if x.ID != e.ID {
			kept = append(kept, x)
		}
	}
	s.events = kept
	return nil
}

func ev(id string, kind int, pubkey string, created nostr.Timestamp) *nostr.Event {
	return &nostr.Event{ID: id, Kind: kind, PubKey: pubkey, CreatedAt: created}
}

func TestSweep_DeletesOldRegularEventsOnly(t *testing.T) {
	s := &store{events: []*nostr.Event{
		ev("vieja-nota", 1, "x", ago(200)),
		ev("vieja-reaccion", 7, "x", ago(181)),
		ev("vieja-privado", 1059, "x", ago(400)),
		ev("vieja-borrado", 5, "x", ago(300)),
		ev("vieja-perfil", 0, "x", ago(500)),            // reemplazable: estado actual de la cuenta
		ev("vieja-contactos", 3, "x", ago(500)),         // idem
		ev("vieja-lista", 10002, "x", ago(500)),         // idem
		ev("vieja-direccionable", 30023, "x", ago(500)), // idem
		ev("vieja-del-dueno", 1, "dueno", ago(900)),     // lo del dueño se conserva
		ev("reciente", 1, "x", ago(10)),
		ev("justo-dentro", 1, "x", ago(179)),
	}}
	res, err := SweepOnce(context.Background(), s.Query, s.Delete, now, 180, "dueno")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(s.deleted)
	want := []string{"vieja-borrado", "vieja-nota", "vieja-privado", "vieja-reaccion"}
	if len(s.deleted) != len(want) {
		t.Fatalf("borrados %v, se esperaba %v", s.deleted, want)
	}
	for i := range want {
		if s.deleted[i] != want[i] {
			t.Fatalf("borrados %v, se esperaba %v", s.deleted, want)
		}
	}
	if res.Deleted != 4 {
		t.Fatalf("resultado: %+v", res)
	}
}

func TestSweep_DisabledWhenDaysIsZero(t *testing.T) {
	s := &store{events: []*nostr.Event{ev("a", 1, "x", ago(9999))}}
	res, err := SweepOnce(context.Background(), s.Query, s.Delete, now, 0, "")
	if err != nil || res.Deleted != 0 || len(s.deleted) != 0 {
		t.Fatalf("con 0 días no se borra nada: %+v %v", res, err)
	}
}

func TestSweep_PagesThroughMoreThanOneBatchSkippingKeptEvents(t *testing.T) {
	s := &store{}
	// 1.300 eventos viejos: 1.000 notas borrables y 300 perfiles (intocables) entremezclados
	for i := 0; i < 1300; i++ {
		kind := 1
		if i%13 < 3 {
			kind = 10002
		}
		s.events = append(s.events, ev("e"+string(rune('a'+i%26))+time.Duration(i).String(), kind, "x", ago(200)-nostr.Timestamp(i)))
	}
	kept := 0
	for _, e := range s.events {
		if !Eligible(e, "") {
			kept++
		}
	}
	res, err := SweepOnce(context.Background(), s.Query, s.Delete, now, 180, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1300-kept || len(s.events) != kept {
		t.Fatalf("debería borrar todas las notas (%d) y dejar los %d intocables; borró %d y quedan %d", 1300-kept, kept, res.Deleted, len(s.events))
	}
}

func TestSweep_StopsAtTheCapPerPass(t *testing.T) {
	s := &store{}
	for i := 0; i < maxPerSweep+1500; i++ {
		s.events = append(s.events, ev(time.Duration(i).String(), 1, "x", ago(200)-nostr.Timestamp(i)))
	}
	res, err := SweepOnce(context.Background(), s.Query, s.Delete, now, 180, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted < maxPerSweep || res.Deleted > maxPerSweep+batchSize || len(s.events) == 0 {
		t.Fatalf("debería pararse cerca del tope (%d) y dejar el resto para la siguiente pasada: borró %d, quedan %d", maxPerSweep, res.Deleted, len(s.events))
	}
	res2, _ := SweepOnce(context.Background(), s.Query, s.Delete, now, 180, "")
	if len(s.events) != 0 || res2.Deleted == 0 {
		t.Fatalf("la siguiente pasada termina el trabajo: quedan %d", len(s.events))
	}
}

func TestSweep_PropagatesErrors(t *testing.T) {
	boom := errors.New("boom")
	if _, err := SweepOnce(context.Background(), func(context.Context, nostr.Filter) (chan *nostr.Event, error) { return nil, boom }, nil, now, 10, ""); !errors.Is(err, boom) {
		t.Fatal("error de consulta")
	}
	s := &store{events: []*nostr.Event{ev("a", 1, "x", ago(50))}}
	if _, err := SweepOnce(context.Background(), s.Query, func(context.Context, *nostr.Event) error { return boom }, now, 10, ""); !errors.Is(err, boom) {
		t.Fatal("error de borrado")
	}
}
