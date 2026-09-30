package policies

import (
	"context"
	"strings"
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip13"
)

func as(pubkey string) AuthedFunc { return func(context.Context) string { return pubkey } }

func TestAuthRequired(t *testing.T) {
	ev := &nostr.Event{Kind: 1}
	if reject, _ := NewAuthRequiredEvent(false, as(""))(context.Background(), ev); reject {
		t.Fatal("desactivado no rechaza")
	}
	reject, msg := NewAuthRequiredEvent(true, as(""))(context.Background(), ev)
	if !reject || !strings.HasPrefix(msg, "auth-required: ") {
		t.Fatalf("sin autenticar debería pedir AUTH: %v %q", reject, msg)
	}
	if reject, _ := NewAuthRequiredEvent(true, as("me"))(context.Background(), ev); reject {
		t.Fatal("autenticado pasa")
	}
	if reject, _ := NewAuthRequiredFilter(true, as(""))(context.Background(), nostr.Filter{}); !reject {
		t.Fatal("consultar sin autenticar debería pedir AUTH")
	}
}

func TestPrivateKindsVisibility(t *testing.T) {
	p := PrivateKinds{4, 1059}
	dm := &nostr.Event{Kind: 1059, PubKey: "ephemeral", Tags: nostr.Tags{{"p", "bob"}}}
	if !p.Visible(dm, "bob") {
		t.Fatal("el destinatario ve el gift wrap")
	}
	if p.Visible(dm, "carol") || p.Visible(dm, "") {
		t.Fatal("un tercero o un anónimo no lo ven")
	}
	sent := &nostr.Event{Kind: 4, PubKey: "alice", Tags: nostr.Tags{{"p", "bob"}}}
	if !p.Visible(sent, "alice") || !p.Visible(sent, "bob") || p.Visible(sent, "carol") {
		t.Fatal("un DM lo ven su autor y su destinatario")
	}
	if !p.Visible(&nostr.Event{Kind: 1, PubKey: "alice"}, "") {
		t.Fatal("un kind público lo ve cualquiera")
	}
}

func TestPrivateFilterAsksForAuthOnlyWhenTheFilterIsExplicitlyPrivate(t *testing.T) {
	p := PrivateKinds{4, 1059}
	f := p.NewPrivateFilter(as(""))
	if reject, msg := f(context.Background(), nostr.Filter{Kinds: []int{1059}}); !reject || !strings.HasPrefix(msg, "auth-required: ") {
		t.Fatal("un filtro explícito de kinds privados debería pedir AUTH")
	}
	for name, filter := range map[string]nostr.Filter{
		"sin kinds": {},
		"mixto":     {Kinds: []int{1, 1059}},
		"público":   {Kinds: []int{1}},
	} {
		if reject, _ := f(context.Background(), filter); reject {
			t.Errorf("%s: no debería rechazarse (se filtra al servir)", name)
		}
	}
	if reject, _ := p.NewPrivateFilter(as("bob"))(context.Background(), nostr.Filter{Kinds: []int{1059}}); reject {
		t.Fatal("autenticado puede pedirlo")
	}
	if reject, _ := (PrivateKinds{}).NewPrivateFilter(as(""))(context.Background(), nostr.Filter{Kinds: []int{1059}}); reject {
		t.Fatal("sin kinds privados configurados no hace nada")
	}
}

func TestPoW(t *testing.T) {
	sign := func(minBits int, target int) *nostr.Event {
		ev := nostr.Event{Kind: 1, CreatedAt: nostr.Now(), Content: "pow", PubKey: "0000000000000000000000000000000000000000000000000000000000000001"}
		if target > 0 {
			tag, err := nip13.DoWork(context.Background(), ev, target)
			if err != nil {
				t.Fatal(err)
			}
			ev.Tags = nostr.Tags{tag}
		}
		ev.ID = ev.GetID()
		return &ev
	}

	if reject, _ := NewPoW(0)(context.Background(), sign(0, 0)); reject {
		t.Fatal("desactivado")
	}
	p := NewPoW(8)
	if reject, msg := p(context.Background(), sign(8, 8)); reject {
		t.Fatalf("un evento minado a 8 bits debería pasar: %s", msg)
	}
	if reject, msg := p(context.Background(), sign(8, 0)); !reject || !strings.HasPrefix(msg, "pow: ") {
		t.Fatalf("sin trabajo debería rechazarse con 'pow:': %q", msg)
	}
	// minado a 4 bits comprometidos (aunque por suerte tuviera más) no llega a 8
	weak := sign(8, 4)
	if nip13.Difficulty(weak.ID) >= 8 {
		t.Skip("el evento minado a 4 bits tiene por suerte >= 8 bits; caso no representativo")
	}
	if reject, _ := p(context.Background(), weak); !reject {
		t.Fatal("dificultad insuficiente")
	}
	if reject, _ := p(context.Background(), &nostr.Event{Kind: nostr.KindDeletion, ID: strings.Repeat("f", 64)}); reject {
		t.Fatal("los borrados no exigen trabajo")
	}
}

type fakeMod struct {
	bannedPK, allowedPK, bannedEv map[string]bool
	blockedKinds                  map[int]bool
}

func (f fakeMod) IsPubKeyBanned(p string) bool  { return f.bannedPK[p] }
func (f fakeMod) HasAllowlist() bool            { return len(f.allowedPK) > 0 }
func (f fakeMod) IsPubKeyAllowed(p string) bool { return f.allowedPK[p] }
func (f fakeMod) IsEventBanned(id string) bool  { return f.bannedEv[id] }
func (f fakeMod) KindBlocked(k int) bool        { return f.blockedKinds[k] }

func TestModerationPolicy(t *testing.T) {
	m := fakeMod{bannedPK: map[string]bool{"spammer": true}, bannedEv: map[string]bool{"bad": true}, blockedKinds: map[int]bool{1984: true}}
	p := NewModeration(m, "boss")
	ctx := context.Background()
	if reject, _ := p(ctx, &nostr.Event{PubKey: "spammer", Kind: 1}); !reject {
		t.Fatal("pubkey baneado")
	}
	if reject, _ := p(ctx, &nostr.Event{PubKey: "ok", ID: "bad", Kind: 1}); !reject {
		t.Fatal("evento baneado")
	}
	if reject, _ := p(ctx, &nostr.Event{PubKey: "ok", Kind: 1984}); !reject {
		t.Fatal("kind prohibido")
	}
	if reject, _ := p(ctx, &nostr.Event{PubKey: "ok", Kind: nostr.KindDeletion}); reject {
		t.Fatal("el kind 5 se libra de la regla de kinds")
	}
	if reject, _ := p(ctx, &nostr.Event{PubKey: "ok", Kind: 1}); reject {
		t.Fatal("un evento normal pasa")
	}
	m.allowedPK = map[string]bool{"friend": true}
	p = NewModeration(m, "boss")
	if reject, _ := p(ctx, &nostr.Event{PubKey: "ok", Kind: 1}); !reject {
		t.Fatal("con lista blanca solo escriben los permitidos")
	}
	if reject, _ := p(ctx, &nostr.Event{PubKey: "friend", Kind: 1}); reject {
		t.Fatal("un permitido escribe")
	}
	// el dueño pasa siempre: baneado, fuera de la lista blanca y con el kind prohibido
	m.bannedPK["boss"] = true
	p = NewModeration(m, "boss")
	if reject, msg := p(ctx, &nostr.Event{PubKey: "boss", Kind: 1984}); reject {
		t.Fatalf("el dueño nunca debe quedar bloqueado: %s", msg)
	}
}
