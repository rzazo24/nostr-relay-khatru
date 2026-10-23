package policies

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip40"
)

// Salas del chat (chat.hivescope.xyz) que guardan su historial 24 h o 7 días. Sus mensajes y reacciones son kinds
// normales (los 20000–29999 nunca se guardan) y llevan siempre caducidad NIP-40: el relé no los guarda sin ella ni
// más allá del plazo más largo que ofrece el chat. El contenido va cifrado con la clave de la sala; el relé solo ve
// el tag `t` (un hash de la sala) y, en las reacciones, el `e` del mensaje.
const (
	ChatHistoryMessageKind  = 4201
	ChatHistoryReactionKind = 4203
	// ChatHistoryMaxKeep es el plazo más largo que ofrece el chat (7 días), más un margen para relojes atrasados.
	ChatHistoryMaxKeep = 7*24*time.Hour + 10*time.Minute
	// Un mensaje del chat tiene como mucho 1000 caracteres; cifrado y en base64 no llega a 6 KB.
	chatHistoryMaxContent = 8192
)

var (
	chatTopic = regexp.MustCompile(`^[0-9a-f]{32}$`)
	eventID   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// IsChatHistoryKind dice si el kind es de una sala del chat con historial.
func IsChatHistoryKind(kind int) bool {
	return kind == ChatHistoryMessageKind || kind == ChatHistoryReactionKind
}

// NewChatHistory construye una política RejectEvent para los kinds del chat con historial; los demás pasan sin mirar.
// `now` se inyecta para poder probarla.
func NewChatHistory(now func() time.Time) func(ctx context.Context, event *nostr.Event) (bool, string) {
	return func(ctx context.Context, event *nostr.Event) (bool, string) {
		if !IsChatHistoryKind(event.Kind) {
			return false, ""
		}
		exp := nip40.GetExpiration(event.Tags)
		if exp == -1 {
			return true, "invalid: chat history needs an expiration tag (NIP-40)"
		}
		if exp.Time().After(now().Add(ChatHistoryMaxKeep)) {
			return true, "invalid: chat history is kept for 7 days at most"
		}
		if len(event.Content) > chatHistoryMaxContent {
			return true, fmt.Sprintf("invalid: chat message is too long (%d bytes, the maximum is %d)", len(event.Content), chatHistoryMaxContent)
		}
		topics, targets := 0, 0
		for _, tag := range event.Tags {
			if len(tag) < 2 {
				continue
			}
			switch tag[0] {
			case "t":
				if !chatTopic.MatchString(tag[1]) {
					return true, "invalid: the room tag must be 32 hex characters"
				}
				topics++
			case "e":
				if !eventID.MatchString(tag[1]) {
					return true, "invalid: the reaction must point to a message id"
				}
				targets++
			}
		}
		if topics != 1 {
			return true, "invalid: chat history needs exactly one room tag"
		}
		if event.Kind == ChatHistoryReactionKind && targets != 1 {
			return true, "invalid: a reaction needs exactly one message (e tag)"
		}
		if event.Kind == ChatHistoryMessageKind && targets != 0 {
			return true, "invalid: a chat message does not point to other events"
		}
		return false, ""
	}
}
