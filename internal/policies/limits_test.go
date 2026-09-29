package policies

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

var fixedNow = time.Unix(1_800_000_000, 0)

func policy(l EventLimits) func(context.Context, *nostr.Event) (bool, string) {
	return NewEventLimits(l, func() time.Time { return fixedNow })
}

func note(content string, tags nostr.Tags) *nostr.Event {
	return &nostr.Event{Kind: 1, Content: content, Tags: tags, CreatedAt: nostr.Timestamp(fixedNow.Unix())}
}

func TestLimits_AcceptsAnOrdinaryNote(t *testing.T) {
	p := policy(EventLimits{MaxContentLength: 100, MaxEventTags: 10, MaxTagValueBytes: 64, MaxFutureSkew: time.Minute})
	if reject, msg := p(context.Background(), note("hola", nostr.Tags{{"t", "nostr"}})); reject {
		t.Fatalf("no debería rechazarse: %s", msg)
	}
}

func TestLimits_ContentCountsCharactersNotBytes(t *testing.T) {
	p := policy(EventLimits{MaxContentLength: 10})
	if reject, _ := p(context.Background(), note(strings.Repeat("😀", 10), nil)); reject {
		t.Fatal("10 emojis caben en un límite de 10 caracteres")
	}
	if reject, _ := p(context.Background(), note(strings.Repeat("a", 11), nil)); !reject {
		t.Fatal("11 caracteres deberían rechazarse")
	}
}

func TestLimits_TagsAndTagValues(t *testing.T) {
	p := policy(EventLimits{MaxEventTags: 2, MaxTagValueBytes: 5})
	if reject, _ := p(context.Background(), note("", nostr.Tags{{"a"}, {"b"}, {"c"}})); !reject {
		t.Fatal("demasiados tags")
	}
	if reject, _ := p(context.Background(), note("", nostr.Tags{{"t", "demasiado-largo"}})); !reject {
		t.Fatal("valor de tag demasiado largo")
	}
	if reject, _ := p(context.Background(), note("", nostr.Tags{{"t", "ok"}})); reject {
		t.Fatal("debería aceptarse")
	}
}

func TestLimits_FutureSkew(t *testing.T) {
	p := policy(EventLimits{MaxFutureSkew: time.Minute})
	ev := note("", nil)
	ev.CreatedAt = nostr.Timestamp(fixedNow.Add(30 * time.Second).Unix())
	if reject, _ := p(context.Background(), ev); reject {
		t.Fatal("30 s en el futuro está dentro del margen")
	}
	ev.CreatedAt = nostr.Timestamp(fixedNow.Add(2 * time.Minute).Unix())
	if reject, _ := p(context.Background(), ev); !reject {
		t.Fatal("2 min en el futuro deberían rechazarse")
	}
	ev.CreatedAt = nostr.Timestamp(fixedNow.Add(-48 * time.Hour).Unix())
	if reject, _ := p(context.Background(), ev); reject {
		t.Fatal("los eventos antiguos se aceptan (hace falta para copiar historial)")
	}
	if reject, _ := policy(EventLimits{})(context.Background(), func() *nostr.Event { e := note("", nil); e.CreatedAt += 1e6; return e }()); reject {
		t.Fatal("con el límite desactivado (0) se acepta cualquier fecha")
	}
}

func TestLimits_AlreadyExpiredEventsAreRejected(t *testing.T) {
	p := policy(EventLimits{})
	past := note("", nostr.Tags{{"expiration", strconv.FormatInt(fixedNow.Unix()-10, 10)}})
	if reject, _ := p(context.Background(), past); !reject {
		t.Fatal("un evento ya caducado debería rechazarse")
	}
	future := note("", nostr.Tags{{"expiration", strconv.FormatInt(fixedNow.Unix()+3600, 10)}})
	if reject, _ := p(context.Background(), future); reject {
		t.Fatal("un evento que caduca en el futuro es válido")
	}
}

func TestLimits_AllowedKinds(t *testing.T) {
	p := policy(EventLimits{AllowedKinds: []int{0, 1}})
	if reject, _ := p(context.Background(), &nostr.Event{Kind: 1, CreatedAt: nostr.Timestamp(fixedNow.Unix())}); reject {
		t.Fatal("kind 1 permitido")
	}
	if reject, _ := p(context.Background(), &nostr.Event{Kind: 30023, CreatedAt: nostr.Timestamp(fixedNow.Unix())}); !reject {
		t.Fatal("kind 30023 no está en la lista")
	}
	if reject, _ := p(context.Background(), &nostr.Event{Kind: nostr.KindDeletion, CreatedAt: nostr.Timestamp(fixedNow.Unix())}); reject {
		t.Fatal("los borrados (kind 5) se aceptan siempre")
	}
}
