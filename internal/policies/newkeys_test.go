package policies

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// fakeAges guarda la primera vez de cada clave en memoria.
type fakeAges struct {
	first   map[string]int64
	allowed map[string]bool
}

func (f *fakeAges) SeeKey(pk string, now time.Time) (int64, bool) {
	if t, ok := f.first[pk]; ok {
		return t, false
	}
	f.first[pk] = now.Unix()
	return now.Unix(), true
}
func (f *fakeAges) IsPubKeyAllowed(pk string) bool { return f.allowed[pk] }

func TestNewKeys_ProbationAndExemptions(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time { return now }
	ages := &fakeAges{first: map[string]int64{}, allowed: map[string]bool{"allowed": true}}
	opts := NewKeyOptions{Hours: 24, Kinds: map[int]bool{1: true, 30023: true}, Owner: "owner"}
	reject := NewNewKeys(ages, opts, clock)
	ev := func(pk string, kind int) *nostr.Event { return &nostr.Event{PubKey: pk, Kind: kind} }
	check := func(pk string, kind int) (bool, string) { return reject(context.Background(), ev(pk, kind)) }

	if r, msg := check("newbie", 1); !r || !strings.HasPrefix(msg, "restricted:") || !strings.Contains(msg, "24 hour") {
		t.Fatalf("una nota de una clave que se ve por primera vez se aplaza: %v %q", r, msg)
	}
	for _, kind := range []int{0, 3, 5, 7, 10002} {
		if r, msg := check("newbie", kind); r {
			t.Fatalf("el kind %d de una clave nueva sí se acepta (perfil, listas, borrados y reacciones): %q", kind, msg)
		}
	}
	if r, _ := check("newbie", 30023); !r {
		t.Fatal("los artículos también se aplazan (están en la lista)")
	}
	if r, _ := check("allowed", 1); r {
		t.Fatal("la lista blanca de autores queda exenta")
	}
	if r, _ := check("owner", 1); r {
		t.Fatal("el dueño queda exento")
	}
	if _, known := ages.first["owner"]; known {
		t.Fatal("al dueño ni se le anota")
	}

	// el mensaje cuenta las horas que faltan, hacia arriba
	now = now.Add(20*time.Hour + 30*time.Minute)
	if r, msg := check("newbie", 1); !r || !strings.Contains(msg, "about 4 hour") {
		t.Fatalf("a las 20 h 30 min faltan unas 4 h: %v %q", r, msg)
	}
	// cumplido el plazo, publica
	now = now.Add(4 * time.Hour)
	if r, msg := check("newbie", 1); r {
		t.Fatalf("pasadas 24 h ya puede publicar notas: %q", msg)
	}
	// una clave que se ve por primera vez más tarde vuelve a empezar su propio plazo
	if r, _ := check("second", 1); !r {
		t.Fatal("cada clave tiene su propio plazo")
	}
}

func TestNewKeys_OffJustRecordsAndEphemeralsAreIgnored(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ages := &fakeAges{first: map[string]int64{}}
	off := NewNewKeys(ages, NewKeyOptions{Hours: 0, Kinds: map[int]bool{1: true}}, func() time.Time { return now })
	if r, _ := off(context.Background(), &nostr.Event{PubKey: "a", Kind: 1}); r {
		t.Fatal("con 0 horas no se rechaza nada")
	}
	if _, known := ages.first["a"]; !known {
		t.Fatal("pero sí se anota cuándo se ve cada clave (para la insignia del panel y por si se activa después)")
	}
	for _, kind := range []int{20000, 20001, 29999} {
		off(context.Background(), &nostr.Event{PubKey: "flood" + string(rune('0'+kind%10)), Kind: kind})
	}
	if len(ages.first) != 1 {
		t.Fatalf("los efímeros no crean filas: %v", ages.first)
	}
	on := NewNewKeys(ages, NewKeyOptions{Hours: 24, Kinds: map[int]bool{1: true}}, func() time.Time { return now })
	if r, _ := on(context.Background(), &nostr.Event{PubKey: "x", Kind: 20001}); r {
		t.Fatal("los efímeros nunca se frenan por ser de una clave nueva")
	}
}
