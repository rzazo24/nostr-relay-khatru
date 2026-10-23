package policies

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

func TestChatHistory(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	reject := NewChatHistory(func() time.Time { return now })
	topic := strings.Repeat("a", 32)
	msgID := strings.Repeat("b", 64)
	in := func(d time.Duration) string { return strconv.FormatInt(now.Add(d).Unix(), 10) }

	cases := []struct {
		name   string
		ev     nostr.Event
		reject bool
	}{
		{"otro kind pasa sin mirar", nostr.Event{Kind: 1}, false},
		{"mensaje con 24 h", nostr.Event{Kind: ChatHistoryMessageKind, Tags: nostr.Tags{{"t", topic}, {"expiration", in(24 * time.Hour)}}}, false},
		{"mensaje con 7 días", nostr.Event{Kind: ChatHistoryMessageKind, Tags: nostr.Tags{{"t", topic}, {"expiration", in(7 * 24 * time.Hour)}}}, false},
		{"reacción", nostr.Event{Kind: ChatHistoryReactionKind, Tags: nostr.Tags{{"t", topic}, {"e", msgID}, {"expiration", in(time.Hour)}}}, false},
		{"sin caducidad", nostr.Event{Kind: ChatHistoryMessageKind, Tags: nostr.Tags{{"t", topic}}}, true},
		{"más de 7 días", nostr.Event{Kind: ChatHistoryMessageKind, Tags: nostr.Tags{{"t", topic}, {"expiration", in(8 * 24 * time.Hour)}}}, true},
		{"sin sala", nostr.Event{Kind: ChatHistoryMessageKind, Tags: nostr.Tags{{"expiration", in(time.Hour)}}}, true},
		{"dos salas", nostr.Event{Kind: ChatHistoryMessageKind, Tags: nostr.Tags{{"t", topic}, {"t", topic}, {"expiration", in(time.Hour)}}}, true},
		{"sala que no es un hash", nostr.Event{Kind: ChatHistoryMessageKind, Tags: nostr.Tags{{"t", "bitcoin"}, {"expiration", in(time.Hour)}}}, true},
		{"reacción sin mensaje", nostr.Event{Kind: ChatHistoryReactionKind, Tags: nostr.Tags{{"t", topic}, {"expiration", in(time.Hour)}}}, true},
		{"mensaje que apunta a otro", nostr.Event{Kind: ChatHistoryMessageKind, Tags: nostr.Tags{{"t", topic}, {"e", msgID}, {"expiration", in(time.Hour)}}}, true},
		{"contenido enorme", nostr.Event{Kind: ChatHistoryMessageKind, Content: strings.Repeat("x", 9000), Tags: nostr.Tags{{"t", topic}, {"expiration", in(time.Hour)}}}, true},
	}
	for _, c := range cases {
		got, why := reject(context.Background(), &c.ev)
		if got != c.reject {
			t.Errorf("%s: rechazado=%v (%q), se esperaba %v", c.name, got, why, c.reject)
		}
	}
}
