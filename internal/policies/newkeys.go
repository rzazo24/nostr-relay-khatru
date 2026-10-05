package policies

import (
	"context"
	"fmt"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// KeyAges es lo que la política necesita saber de cuándo se vio cada clave por primera vez (lo implementa moderation.Store).
type KeyAges interface {
	// SeeKey anota la clave si es nueva y devuelve su primera vez (unix).
	SeeKey(pubkey string, now time.Time) (first int64, wasNew bool)
	IsPubKeyAllowed(pubkey string) bool
}

// NewKeyOptions configura el periodo de prueba de las claves nuevas.
type NewKeyOptions struct {
	Hours int          // 0 = desactivado: solo se anota cuándo se ve cada clave
	Kinds map[int]bool // tipos de evento que se aplazan durante la prueba (por defecto, las notas)
	Owner string       // el dueño nunca queda en prueba
}

// NewNewKeys anota la primera vez que se ve cada clave que publica eventos que se guardan y, si Hours > 0, rechaza los tipos de
// `Kinds` de las claves que el relé conoce desde hace menos de Hours horas. El perfil, las listas, las reacciones y los borrados
// siguen pudiendo publicarse (una clave nueva puede darse de alta); la lista blanca de autores y el dueño quedan exentos.
//
// Los eventos efímeros no se anotan ni se frenan: no se guardan y una inundación de claves de usar y tirar llenaría la tabla.
// Va la última de la cadena, para que lo que otras reglas ya rechazan no cree filas.
func NewNewKeys(ages KeyAges, o NewKeyOptions, now func() time.Time) func(ctx context.Context, event *nostr.Event) (bool, string) {
	return func(ctx context.Context, event *nostr.Event) (bool, string) {
		if event.Kind >= 20000 && event.Kind < 30000 {
			return false, ""
		}
		if o.Owner != "" && event.PubKey == o.Owner {
			return false, ""
		}
		t := now()
		first, _ := ages.SeeKey(event.PubKey, t)
		if o.Hours <= 0 || !o.Kinds[event.Kind] || ages.IsPubKeyAllowed(event.PubKey) {
			return false, ""
		}
		wait := time.Duration(o.Hours)*time.Hour - t.Sub(time.Unix(first, 0))
		if wait <= 0 {
			return false, ""
		}
		hours := int((wait + time.Hour - 1) / time.Hour) // hacia arriba: «unas 3 horas», nunca «0 horas»
		return true, fmt.Sprintf("restricted: new keys cannot publish this kind of event yet; try again in about %d hour(s) (you can publish your profile now)", hours)
	}
}
