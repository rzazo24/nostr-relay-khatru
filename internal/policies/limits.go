// Package policies contiene las reglas de aceptación del relé. Es un relé
// abierto: no exige identidad ni lista blanca de autores; solo pone límites a lo
// que se acepta para que un cliente no pueda llenar el disco ni romper a los demás.
package policies

import (
	"context"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip40"
)

// EventLimits son los límites por evento. Un valor 0 desactiva ese límite
// (menos AllowedKinds, donde vacío significa "todos").
type EventLimits struct {
	MaxContentLength int           // caracteres (runas), no bytes
	MaxEventTags     int           // número de tags
	MaxTagValueBytes int           // bytes de cada elemento de cada tag
	MaxFutureSkew    time.Duration // cuánto puede adelantarse created_at respecto al reloj del relé
	AllowedKinds     []int
}

// NewEventLimits construye una política khatru RejectEvent que rechaza los
// eventos que se pasan de los límites. `now` se inyecta para poder probarla.
//
// Los borrados (kind 5) no se saltan los límites de tamaño, pero sí la lista de
// kinds permitidos: quitar un evento propio no debería bloquearse por política.
func NewEventLimits(l EventLimits, now func() time.Time) func(ctx context.Context, event *nostr.Event) (bool, string) {
	return func(ctx context.Context, event *nostr.Event) (bool, string) {
		if len(l.AllowedKinds) > 0 && event.Kind != nostr.KindDeletion && !slices.Contains(l.AllowedKinds, event.Kind) {
			return true, fmt.Sprintf("blocked: kind %d is not accepted by this relay", event.Kind)
		}

		if l.MaxContentLength > 0 {
			if n := utf8.RuneCountInString(event.Content); n > l.MaxContentLength {
				return true, fmt.Sprintf("invalid: content is too long (%d characters, the maximum is %d)", n, l.MaxContentLength)
			}
		}

		if l.MaxEventTags > 0 && len(event.Tags) > l.MaxEventTags {
			return true, fmt.Sprintf("invalid: too many tags (%d, the maximum is %d)", len(event.Tags), l.MaxEventTags)
		}

		if l.MaxTagValueBytes > 0 {
			for _, tag := range event.Tags {
				for _, v := range tag {
					if len(v) > l.MaxTagValueBytes {
						return true, fmt.Sprintf("invalid: a tag value is too long (%d bytes, the maximum is %d)", len(v), l.MaxTagValueBytes)
					}
				}
			}
		}

		if l.MaxFutureSkew > 0 && event.CreatedAt.Time().After(now().Add(l.MaxFutureSkew)) {
			return true, fmt.Sprintf("invalid: created_at is too far in the future (more than %s ahead of the relay's clock)", l.MaxFutureSkew)
		}

		// NIP-40: un evento que ya nació caducado no tiene sentido guardarlo.
		if exp := nip40.GetExpiration(event.Tags); exp != -1 && exp <= nostr.Timestamp(now().Unix()) {
			return true, "invalid: the event has already expired (NIP-40 expiration)"
		}

		return false, ""
	}
}
