package policies

import (
	"context"
	"fmt"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip13"
)

// NewPoW exige prueba de trabajo NIP-13: el id del evento debe empezar por al menos
// minBits bits a cero, y además el evento debe COMPROMETERSE a esa dificultad en el
// tag `nonce` (tercer elemento). Sin lo segundo, alguien podría publicar eventos
// que casualmente cumplen (un id con muchos ceros por suerte) sin haber minado.
//
// minBits <= 0 desactiva la política. Los borrados (kind 5) no exigen trabajo:
// retirar un evento propio no debería costar CPU.
func NewPoW(minBits int) func(ctx context.Context, event *nostr.Event) (bool, string) {
	return func(ctx context.Context, event *nostr.Event) (bool, string) {
		if minBits <= 0 || event.Kind == nostr.KindDeletion {
			return false, ""
		}
		if err := nip13.Check(event.ID, minBits); err != nil {
			return true, fmt.Sprintf("pow: difficulty %d is less than %d", nip13.Difficulty(event.ID), minBits)
		}
		if committed := nip13.CommittedDifficulty(event); committed < minBits {
			return true, fmt.Sprintf("pow: the target difficulty committed in the nonce tag (%d) is less than %d", committed, minBits)
		}
		return false, ""
	}
}
