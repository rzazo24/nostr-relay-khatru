// Package retention borra los eventos antiguos para que la base de datos no crezca sin límite.
//
// Qué se borra: los eventos "regulares" (notas, reacciones, mensajes privados, borrados,
// reposts...; nostr.IsRegularKind) más viejos que N días. Qué NO se borra: los que describen el
// estado actual de una cuenta (perfil, contactos, listas de relés: kinds reemplazables y
// direccionables, que ya son uno por cuenta y tipo), y todo lo publicado por el dueño del
// relé. Los efímeros nunca se guardan.
package retention

import (
	"context"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip40"
)

// QueryFunc y DeleteFunc tienen la firma de los métodos QueryEvents y DeleteEvent del almacén.
type QueryFunc func(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error)
type DeleteFunc func(ctx context.Context, evt *nostr.Event) error

const (
	batchSize = 500
	// Tope de borrados por pasada: si hay una montaña de eventos viejos (p. ej. la primera vez
	// que se activa), se elimina poco a poco en varias pasadas en vez de bloquear la base de golpe.
	maxPerSweep = 20000
)

// Result resume una pasada.
type Result struct {
	Deleted int
	Scanned int
}

// Eligible dice si un evento puede borrarse por antigüedad.
func Eligible(ev *nostr.Event, keepPubKey string) bool {
	return nostr.IsRegularKind(ev.Kind) && (keepPubKey == "" || ev.PubKey != keepPubKey)
}

// SweepOnce borra, como mucho maxPerSweep, los eventos elegibles anteriores a now-days.
// days <= 0 no hace nada. Las consultas son del más nuevo al más viejo, así que se va bajando
// por fechas: lo que no es elegible (perfiles, listas, lo del dueño) se salta sin volver a mirarlo.
func SweepOnce(ctx context.Context, query QueryFunc, del DeleteFunc, now time.Time, days int, keepPubKey string) (Result, error) {
	var res Result
	if days <= 0 {
		return res, nil
	}
	until := nostr.Timestamp(now.Add(-time.Duration(days) * 24 * time.Hour).Unix())
	for res.Deleted < maxPerSweep {
		ch, err := query(ctx, nostr.Filter{Until: &until, Limit: batchSize})
		if err != nil {
			return res, err
		}
		var batch []*nostr.Event
		for ev := range ch {
			batch = append(batch, ev)
		}
		if len(batch) == 0 {
			return res, nil
		}
		oldest := batch[0].CreatedAt
		for _, ev := range batch {
			res.Scanned++
			if ev.CreatedAt < oldest {
				oldest = ev.CreatedAt
			}
			if !Eligible(ev, keepPubKey) {
				continue
			}
			if err := del(ctx, ev); err != nil {
				return res, err
			}
			res.Deleted++
		}
		if len(batch) < batchSize {
			return res, nil
		}
		// La siguiente página empieza justo antes del evento más viejo visto. Los no elegibles de
		// esta página (que siguen ahí) quedan por encima y no se vuelven a pedir.
		if oldest == 0 {
			return res, nil
		}
		next := oldest - 1
		until = next
	}
	return res, nil
}

// Start hace una pasada poco después de arrancar y luego cada `interval`, hasta que ctx se cancela.
func Start(ctx context.Context, query QueryFunc, del DeleteFunc, days int, keepPubKey string, interval time.Duration, onSwept func(Result, error)) {
	if days <= 0 {
		return
	}
	run := func() {
		res, err := SweepOnce(ctx, query, del, time.Now(), days, keepPubKey)
		if onSwept != nil {
			onSwept(res, err)
		}
	}
	select {
	case <-time.After(time.Minute):
		run()
	case <-ctx.Done():
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// SweepExpired borra los eventos de `kinds` cuya caducidad NIP-40 ya pasó. khatru tiene su propio barrido de caducados,
// pero cada hora y solo de lo que ha visto desde que arrancó: estos kinds (las salas del chat con historial) prometen a
// sus usuarios un plazo, así que se barren aparte, sin depender de eso.
func SweepExpired(ctx context.Context, query QueryFunc, del DeleteFunc, now time.Time, kinds []int) (Result, error) {
	var res Result
	if len(kinds) == 0 {
		return res, nil
	}
	cut := nostr.Timestamp(now.Unix())
	var until *nostr.Timestamp
	for res.Scanned < 10*maxPerSweep {
		ch, err := query(ctx, nostr.Filter{Kinds: kinds, Until: until, Limit: batchSize})
		if err != nil {
			return res, err
		}
		var batch []*nostr.Event
		for ev := range ch {
			batch = append(batch, ev)
		}
		if len(batch) == 0 {
			return res, nil
		}
		oldest := batch[0].CreatedAt
		for _, ev := range batch {
			res.Scanned++
			if ev.CreatedAt < oldest {
				oldest = ev.CreatedAt
			}
			if exp := nip40.GetExpiration(ev.Tags); exp == -1 || exp > cut {
				continue
			}
			if err := del(ctx, ev); err != nil {
				return res, err
			}
			res.Deleted++
		}
		if len(batch) < batchSize || oldest == 0 {
			return res, nil
		}
		next := oldest - 1
		until = &next
	}
	return res, nil
}

// StartExpired barre los caducados de `kinds` poco después de arrancar y luego cada `interval`, hasta que ctx se cancela.
func StartExpired(ctx context.Context, query QueryFunc, del DeleteFunc, kinds []int, interval time.Duration, onSwept func(Result, error)) {
	run := func() {
		res, err := SweepExpired(ctx, query, del, time.Now(), kinds)
		if onSwept != nil {
			onSwept(res, err)
		}
	}
	select {
	case <-time.After(30 * time.Second):
		run()
	case <-ctx.Done():
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
