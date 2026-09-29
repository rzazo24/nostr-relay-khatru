package policies

import (
	"context"
	"slices"

	"github.com/nbd-wtf/go-nostr"
)

// AuthedFunc devuelve el pubkey con el que se ha autenticado (NIP-42) la conexión
// del contexto, o "" si no lo ha hecho. En producción es khatru.GetAuthed; se inyecta
// para poder probar las políticas sin una conexión websocket real.
type AuthedFunc func(ctx context.Context) string

// Los mensajes que empiezan por "auth-required: " hacen que khatru envíe solo el
// desafío AUTH al cliente, que puede autenticarse y repetir la petición.
const (
	authRequiredEvent = "auth-required: this relay requires you to authenticate (NIP-42) before publishing"
	authRequiredQuery = "auth-required: this relay requires you to authenticate (NIP-42) before querying"
)

// NewAuthRequiredEvent exige autenticación NIP-42 para publicar (si enabled).
func NewAuthRequiredEvent(enabled bool, authed AuthedFunc) func(ctx context.Context, event *nostr.Event) (bool, string) {
	return func(ctx context.Context, event *nostr.Event) (bool, string) {
		if enabled && authed(ctx) == "" {
			return true, authRequiredEvent
		}
		return false, ""
	}
}

// NewAuthRequiredFilter exige autenticación NIP-42 para consultar (si enabled).
func NewAuthRequiredFilter(enabled bool, authed AuthedFunc) func(ctx context.Context, filter nostr.Filter) (bool, string) {
	return func(ctx context.Context, filter nostr.Filter) (bool, string) {
		if enabled && authed(ctx) == "" {
			return true, authRequiredQuery
		}
		return false, ""
	}
}

// PrivateKinds son los kinds cuyo contenido solo pueden ver su autor y su
// destinatario (el pubkey del tag `p`), y siempre autenticados. Pensado para los
// mensajes directos (kind 4, y kind 1059 "gift wrap" de NIP-17/59).
type PrivateKinds []int

// IsPrivate dice si el kind es privado.
func (p PrivateKinds) IsPrivate(kind int) bool { return slices.Contains(p, kind) }

// Visible dice si `viewer` (pubkey autenticado, "" si no lo está) puede ver el evento.
func (p PrivateKinds) Visible(event *nostr.Event, viewer string) bool {
	if !p.IsPrivate(event.Kind) {
		return true
	}
	if viewer == "" {
		return false
	}
	return event.PubKey == viewer || event.Tags.FindWithValue("p", viewer) != nil
}

// NewPrivateFilter pide autenticarse cuando un filtro apunta EXPRESAMENTE a kinds
// privados (todos sus kinds lo son), que es como los clientes de mensajería piden
// sus DMs: así reciben el desafío AUTH y repiten la consulta autenticados. Los
// filtros ambiguos (sin kinds, o mezclados) no se rechazan: se sirven, pero sin los
// eventos privados que el solicitante no pueda ver (ver Visible).
func (p PrivateKinds) NewPrivateFilter(authed AuthedFunc) func(ctx context.Context, filter nostr.Filter) (bool, string) {
	return func(ctx context.Context, filter nostr.Filter) (bool, string) {
		if len(p) == 0 || len(filter.Kinds) == 0 || authed(ctx) != "" {
			return false, ""
		}
		for _, k := range filter.Kinds {
			if !p.IsPrivate(k) {
				return false, ""
			}
		}
		return true, "auth-required: these events are private, authenticate (NIP-42) to read the ones addressed to you"
	}
}
