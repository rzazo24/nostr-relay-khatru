package server

import (
	"context"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// Los mensajes de NIP-46 (inicio de sesión con un firmador remoto, como Clave en el iPhone) viajan en eventos
// efímeros de tipo 24133, que un relé normal reenvía a quien esté escuchando en ese instante y olvida. En el móvil eso
// no basta: mientras apruebas en la app firmadora, Safari suspende la página y su conexión, y la respuesta se pierde.
// Este buzón guarda esos eventos unos minutos, solo en memoria, y los entrega a quien los pida después con un
// filtro por `#p` (el destinatario). El contenido va cifrado de extremo a extremo (NIP-44): el relé no lo lee.
const (
	mailboxKind    = 24133
	mailboxTTL     = 10 * time.Minute
	mailboxMax     = 500
	mailboxMaxSize = 16 * 1024 // bytes de contenido; los mensajes de NIP-46 son pequeños
)

type mailbox struct {
	mu     sync.Mutex
	now    func() time.Time
	events []mailboxEntry // del más antiguo al más nuevo
}

type mailboxEntry struct {
	ev *nostr.Event
	at time.Time
}

func newMailbox(now func() time.Time) *mailbox { return &mailbox{now: now} }

// Add guarda un evento de NIP-46 (los demás se ignoran). Al pasar el máximo se descartan los más antiguos.
func (m *mailbox) Add(ev *nostr.Event) {
	if ev.Kind != mailboxKind || len(ev.Content) > mailboxMaxSize {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	for _, e := range m.events {
		if e.ev.ID == ev.ID {
			return
		}
	}
	cp := *ev
	m.events = append(m.events, mailboxEntry{ev: &cp, at: m.now()})
	if len(m.events) > mailboxMax {
		m.events = m.events[len(m.events)-mailboxMax:]
	}
}

func (m *mailbox) pruneLocked() {
	cutoff := m.now().Add(-mailboxTTL)
	i := 0
	for i < len(m.events) && m.events[i].at.Before(cutoff) {
		i++
	}
	m.events = m.events[i:]
}

// Query devuelve los eventos guardados que cumplen el filtro, el más nuevo primero. Solo contesta a filtros que
// piden el tipo 24133 y un destinatario concreto (`#p`): no se puede pedir «todo lo que haya».
func (m *mailbox) Query(filter nostr.Filter) []*nostr.Event {
	wantsKind := false
	for _, k := range filter.Kinds {
		if k == mailboxKind {
			wantsKind = true
		}
	}
	if !wantsKind || len(filter.Tags["p"]) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	var out []*nostr.Event
	for i := len(m.events) - 1; i >= 0; i-- {
		if ev := m.events[i].ev; filter.Matches(ev) {
			cp := *ev
			out = append(out, &cp)
			if filter.Limit > 0 && len(out) >= filter.Limit {
				break
			}
		}
	}
	return out
}

// query adapta Query a la cadena QueryEvents de khatru.
func (m *mailbox) query(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	found := m.Query(filter)
	// Nunca se devuelve un canal nil: khatru recorre a veces todos los QueryEvents con `range`, y un nil bloquearía
	// para siempre la publicación de eventos normales. Un canal vacío y ya cerrado es el «sin resultados» correcto.
	ch := make(chan *nostr.Event, len(found))
	for _, ev := range found {
		ch <- ev
	}
	close(ch)
	return ch, nil
}
