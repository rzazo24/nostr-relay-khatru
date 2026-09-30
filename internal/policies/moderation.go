package policies

import (
	"context"
	"fmt"

	"github.com/nbd-wtf/go-nostr"
)

// Moderator es lo que la política necesita saber de las listas de moderación
// (las que se administran con NIP-86). Lo implementa moderation.Store.
type Moderator interface {
	IsPubKeyBanned(pubkey string) bool
	HasAllowlist() bool
	IsPubKeyAllowed(pubkey string) bool
	IsEventBanned(id string) bool
	KindBlocked(kind int) bool
}

// NewModeration rechaza lo que las listas de moderación vetan: pubkeys baneados,
// eventos baneados, kinds prohibidos y, si hay lista blanca de autores, cualquier
// pubkey que no esté en ella. El kind 5 (borrado) se libra de la regla de kinds. El dueño
// (`owner`, si no está vacío) queda siempre exento.
func NewModeration(m Moderator, owner string) func(ctx context.Context, event *nostr.Event) (bool, string) {
	return func(ctx context.Context, event *nostr.Event) (bool, string) {
		// El dueño (RELAY_PUBKEY) nunca queda bloqueado por las listas: si no, un baneo o una lista
		// blanca mal puestos le impedirían hasta deshacerlos.
		if owner != "" && event.PubKey == owner {
			return false, ""
		}
		if m.IsPubKeyBanned(event.PubKey) {
			return true, "blocked: this pubkey is banned from this relay"
		}
		if m.HasAllowlist() && !m.IsPubKeyAllowed(event.PubKey) {
			return true, "restricted: only allowed pubkeys can publish to this relay"
		}
		if m.IsEventBanned(event.ID) {
			return true, "blocked: this event is banned"
		}
		if event.Kind != nostr.KindDeletion && m.KindBlocked(event.Kind) {
			return true, fmt.Sprintf("blocked: kind %d is not accepted by this relay", event.Kind)
		}
		return false, ""
	}
}
