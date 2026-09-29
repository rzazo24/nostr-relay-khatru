package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip13"
	"github.com/nbd-wtf/go-nostr/nip77"

	"github.com/rzazo24/nostr-relay-khatru/internal/config"
)

// start levanta un relé real (SQLite en un directorio temporal) tras un servidor HTTP
// de pruebas. Los límites de velocidad se suben para que no estorben.
func start(t *testing.T, env map[string]string) (*Server, *httptest.Server) {
	t.Helper()
	all := map[string]string{
		"RELAY_DB_PATH":           filepath.Join(t.TempDir(), "relay.sqlite"),
		"RELAY_EVENTS_PER_MINUTE": "1000000", "RELAY_EVENTS_BURST": "1000000",
		"RELAY_REQS_PER_MINUTE": "1000000", "RELAY_REQS_BURST": "1000000",
		"RELAY_CONNS_PER_MINUTE": "1000000", "RELAY_CONNS_BURST": "1000000",
	}
	for k, v := range env {
		all[k] = v
	}
	cfg, err := config.Load(func(k string) string { return all[k] })
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Relay)
	srv.Relay.ServiceURL = ts.URL
	t.Cleanup(func() { ts.Close(); srv.Close() })
	return srv, ts
}

func wsURL(ts *httptest.Server) string { return "ws" + strings.TrimPrefix(ts.URL, "http") }

func connect(t *testing.T, ts *httptest.Server) *nostr.Relay {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := nostr.RelayConnect(ctx, wsURL(ts))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

type keys struct{ sk, pk string }

func newKeys() keys {
	sk := nostr.GeneratePrivateKey()
	pk, _ := nostr.GetPublicKey(sk)
	return keys{sk, pk}
}

func (k keys) event(kind int, content string, tags nostr.Tags) nostr.Event {
	ev := nostr.Event{Kind: kind, Content: content, Tags: tags, CreatedAt: nostr.Now(), PubKey: k.pk}
	if err := ev.Sign(k.sk); err != nil {
		panic(err)
	}
	return ev
}

func (k keys) auth(t *testing.T, r *nostr.Relay) {
	t.Helper()
	// El relé envía el desafío AUTH al conectar, pero llega de forma asíncrona:
	// se reintenta un momento por si aún no ha llegado.
	deadline := time.Now().Add(3 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := r.Auth(ctx, func(e *nostr.Event) error { return e.Sign(k.sk) })
		cancel()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no se pudo autenticar: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func publish(r *nostr.Relay, ev nostr.Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.Publish(ctx, ev)
}

// fetch hace una consulta y devuelve los eventos y, si el relé la cerró, el motivo.
func fetch(t *testing.T, r *nostr.Relay, f nostr.Filter) (events []*nostr.Event, closed string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sub, err := r.Subscribe(ctx, nostr.Filters{f})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsub()
	for {
		select {
		case ev := <-sub.Events:
			events = append(events, ev)
		case <-sub.EndOfStoredEvents:
			return events, ""
		case reason := <-sub.ClosedReason:
			return events, reason
		case <-ctx.Done():
			t.Fatal("la consulta no terminó a tiempo")
		}
	}
}

func nip11Doc(t *testing.T, ts *httptest.Server) map[string]any {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL, nil)
	req.Header.Set("Accept", "application/nostr+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var doc map[string]any
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func nips(doc map[string]any) []int {
	var out []int
	for _, v := range doc["supported_nips"].([]any) {
		out = append(out, int(v.(float64)))
	}
	slices.Sort(out)
	return out
}

// ---------- NIP-11 ----------

func TestNIP11_AdvertisesOnlyWhatIsEnabled(t *testing.T) {
	_, ts := start(t, nil)
	got := nips(nip11Doc(t, ts))
	if want := []int{1, 9, 11, 40, 42, 45, 70, 77}; !slices.Equal(got, want) {
		t.Fatalf("NIPs por defecto: %v, se esperaba %v", got, want)
	}

	if up := nip11Doc(t, ts)["limitation"].(map[string]any)["created_at_upper_limit"]; up != float64(900) {
		t.Fatalf("created_at_upper_limit debería anunciar los 900 s configurados: %v", up)
	}

	_, ts = start(t, map[string]string{"RELAY_MIN_POW": "8", "RELAY_PUBKEY": newKeys().pk, "RELAY_AUTH_REQUIRED": "true"})
	doc := nip11Doc(t, ts)
	if want := []int{1, 9, 11, 13, 40, 42, 45, 70, 77, 86}; !slices.Equal(nips(doc), want) {
		t.Fatalf("con NIP-13 y NIP-86 activados: %v", nips(doc))
	}
	lim := doc["limitation"].(map[string]any)
	if lim["min_pow_difficulty"].(float64) != 8 || lim["auth_required"] != true || lim["restricted_writes"] != false {
		t.Fatalf("limitation: %v", lim)
	}
}

// ---------- NIP-13 ----------

func TestNIP13_RequiresProofOfWork(t *testing.T) {
	_, ts := start(t, map[string]string{"RELAY_MIN_POW": "8"})
	r := connect(t, ts)
	k := newKeys()

	err := publish(r, k.event(1, "sin trabajo", nil))
	if err == nil || !strings.Contains(err.Error(), "pow:") {
		t.Fatalf("sin PoW debería rechazarse con 'pow:', hubo: %v", err)
	}

	ev := nostr.Event{Kind: 1, Content: "minado", CreatedAt: nostr.Now(), PubKey: k.pk}
	tag, err := nip13.DoWork(context.Background(), ev, 8)
	if err != nil {
		t.Fatal(err)
	}
	ev.Tags = nostr.Tags{tag}
	ev.Sign(k.sk)
	if err := publish(r, ev); err != nil {
		t.Fatalf("un evento minado a 8 bits debería aceptarse: %v", err)
	}
}

// ---------- NIP-42 / NIP-59 (mensajes privados) ----------

func TestNIP42_PrivateKindsOnlyReachTheirRecipient(t *testing.T) {
	_, ts := start(t, nil)
	anon, alice, bob, carol := connect(t, ts), connect(t, ts), connect(t, ts), connect(t, ts)
	sender, ephemeral := newKeys(), newKeys()
	bobKeys, carolKeys := newKeys(), newKeys()

	// carol escucha TODO (sin kinds) desde antes: no debe recibir el mensaje en vivo
	carolCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	carolKeys.auth(t, carol)
	live, err := carol.Subscribe(carolCtx, nostr.Filters{{}})
	if err != nil {
		t.Fatal(err)
	}
	<-live.EndOfStoredEvents

	// un "gift wrap" (kind 1059) firmado con una clave efímera y dirigido a bob
	wrap := ephemeral.event(1059, "cifrado", nostr.Tags{{"p", bobKeys.pk}})
	if err := publish(alice, wrap); err != nil {
		t.Fatalf("publicar un mensaje privado no exige nada especial: %v", err)
	}
	// y un kind 4 clásico de sender a bob, más una nota pública
	dm := sender.event(4, "hola bob", nostr.Tags{{"p", bobKeys.pk}})
	publish(alice, dm)
	publish(alice, sender.event(1, "nota pública", nil))

	// un anónimo que pide explícitamente kinds privados recibe el desafío AUTH
	if _, closed := fetch(t, anon, nostr.Filter{Kinds: []int{1059}}); !strings.HasPrefix(closed, "auth-required:") {
		t.Fatalf("debería pedir autenticación, cerró con: %q", closed)
	}
	// una consulta genérica de un anónimo no incluye lo privado
	generic, _ := fetch(t, anon, nostr.Filter{})
	for _, ev := range generic {
		if ev.Kind == 1059 || ev.Kind == 4 {
			t.Fatalf("un anónimo no debería ver kind %d", ev.Kind)
		}
	}
	if len(generic) != 1 {
		t.Fatalf("sí debería ver la nota pública: %d eventos", len(generic))
	}

	// bob, autenticado, ve el gift wrap y el DM
	bobKeys.auth(t, bob)
	got, closed := fetch(t, bob, nostr.Filter{Kinds: []int{1059, 4}})
	if closed != "" || len(got) != 2 {
		t.Fatalf("bob debería ver sus 2 mensajes privados: %d eventos, cierre %q", len(got), closed)
	}

	// carol, autenticada, no ve nada de eso ni siquiera pidiéndolo por autor
	got, _ = fetch(t, carol, nostr.Filter{Kinds: []int{1059, 4}})
	if len(got) != 0 {
		t.Fatalf("carol no debería ver mensajes de otros: %d", len(got))
	}
	got, _ = fetch(t, carol, nostr.Filter{Authors: []string{sender.pk}})
	for _, ev := range got {
		if ev.Kind == 4 {
			t.Fatal("carol no debería ver el DM de sender")
		}
	}

	// y la suscripción en vivo de carol tampoco recibió lo privado
	select {
	case ev := <-live.Events:
		if ev.Kind == 1059 || ev.Kind == 4 {
			t.Fatalf("carol recibió en vivo un evento privado (kind %d)", ev.Kind)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

func TestNIP42_DeletingAPrivateMessageStillWorks(t *testing.T) {
	_, ts := start(t, nil)
	r := connect(t, ts)
	alice, bob := newKeys(), newKeys()
	dm := alice.event(4, "secreto", nostr.Tags{{"p", bob.pk}})
	if err := publish(r, dm); err != nil {
		t.Fatal(err)
	}
	if err := publish(r, alice.event(5, "", nostr.Tags{{"e", dm.ID}})); err != nil {
		t.Fatalf("el borrado debería aceptarse: %v", err)
	}
	alice.auth(t, r)
	if got, _ := fetch(t, r, nostr.Filter{Kinds: []int{4}, Authors: []string{alice.pk}}); len(got) != 0 {
		t.Fatal("el DM debería haberse borrado de verdad")
	}
}

func TestNIP42_AuthRequiredMode(t *testing.T) {
	_, ts := start(t, map[string]string{"RELAY_AUTH_REQUIRED": "true"})
	r := connect(t, ts)
	k := newKeys()

	if err := publish(r, k.event(1, "sin auth", nil)); err == nil || !strings.HasPrefix(err.Error(), "msg: auth-required:") && !strings.Contains(err.Error(), "auth-required") {
		t.Fatalf("publicar sin autenticar debería pedir AUTH: %v", err)
	}
	if _, closed := fetch(t, r, nostr.Filter{Kinds: []int{1}}); !strings.HasPrefix(closed, "auth-required:") {
		t.Fatalf("consultar sin autenticar debería pedir AUTH: %q", closed)
	}

	k.auth(t, r)
	if err := publish(r, k.event(1, "con auth", nil)); err != nil {
		t.Fatalf("autenticado debería poder publicar: %v", err)
	}
	if got, closed := fetch(t, r, nostr.Filter{Kinds: []int{1}}); closed != "" || len(got) != 1 {
		t.Fatalf("autenticado debería poder consultar: %d eventos, cierre %q", len(got), closed)
	}
}

// ---------- NIP-70 ----------

func TestNIP70_ProtectedEventsNeedTheirAuthor(t *testing.T) {
	_, ts := start(t, nil)
	r := connect(t, ts)
	author := newKeys()
	ev := author.event(1, "protegido", nostr.Tags{{"-"}})

	if err := publish(r, ev); err == nil || !strings.Contains(err.Error(), "auth-required") {
		t.Fatalf("un evento protegido sin autenticar debería pedir AUTH: %v", err)
	}
	other := newKeys()
	other.auth(t, r)
	if err := publish(r, ev); err == nil || !strings.Contains(err.Error(), "must be published by event author") {
		t.Fatalf("autenticado como otro debería rechazarse: %v", err)
	}
	rAuthor := connect(t, ts)
	author.auth(t, rAuthor)
	if err := publish(rAuthor, ev); err != nil {
		t.Fatalf("su autor autenticado debería poder: %v", err)
	}
}

// ---------- NIP-86 ----------

// manage llama a la API de gestión con autenticación NIP-98 firmada por `caller`.
func manage(t *testing.T, ts *httptest.Server, caller keys, method string, params ...any) map[string]any {
	t.Helper()
	if params == nil {
		params = []any{}
	}
	body, _ := json.Marshal(map[string]any{"method": method, "params": params})
	hash := sha256.Sum256(body)
	authEv := caller.event(27235, "", nostr.Tags{{"u", ts.URL}, {"method", "POST"}, {"payload", hex.EncodeToString(hash[:])}})
	evJSON, _ := json.Marshal(authEv)

	req, _ := http.NewRequest("POST", ts.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/nostr+json+rpc")
	req.Header.Set("Authorization", "Nostr "+base64.StdEncoding.EncodeToString(evJSON))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("respuesta no JSON: %s", raw)
	}
	return out
}

func TestNIP86_OnlyTheOwnerCanManage(t *testing.T) {
	owner, stranger := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})

	if resp := manage(t, ts, stranger, "listbannedpubkeys"); resp["error"] == nil || !strings.Contains(fmt.Sprint(resp["error"]), "unauthorized") {
		t.Fatalf("un extraño no puede: %v", resp)
	}
	if resp := manage(t, ts, owner, "listbannedpubkeys"); resp["error"] != nil {
		t.Fatalf("el dueño sí: %v", resp)
	}
	methods := manage(t, ts, owner, "supportedmethods")["result"].([]any)
	if !slices.Contains(methods, any("banpubkey")) || !slices.Contains(methods, any("disallowkind")) {
		t.Fatalf("métodos anunciados: %v", methods)
	}
}

func TestNIP86_DisabledWithoutAnOwner(t *testing.T) {
	_, ts := start(t, nil)
	resp := manage(t, ts, newKeys(), "listbannedpubkeys")
	if resp["error"] == nil || !strings.Contains(fmt.Sprint(resp["error"]), "disabled") {
		t.Fatalf("sin RELAY_PUBKEY la API debería estar desactivada: %v", resp)
	}
}

func TestNIP86_BanAndAllowPubkeys(t *testing.T) {
	owner, spammer, friend := newKeys(), newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	r := connect(t, ts)

	if err := publish(r, spammer.event(1, "antes del ban", nil)); err != nil {
		t.Fatal(err)
	}
	manage(t, ts, owner, "banpubkey", spammer.pk, "spam")
	if err := publish(r, spammer.event(1, "después", nil)); err == nil || !strings.Contains(err.Error(), "banned") {
		t.Fatalf("un pubkey baneado no puede publicar: %v", err)
	}
	list := manage(t, ts, owner, "listbannedpubkeys")["result"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["reason"] != "spam" {
		t.Fatalf("lista de baneados: %v", list)
	}

	// con un pubkey permitido, solo escriben los permitidos y NIP-11 lo refleja
	manage(t, ts, owner, "allowpubkey", friend.pk, "amigo")
	if err := publish(r, newKeys().event(1, "extraño", nil)); err == nil || !strings.Contains(err.Error(), "restricted") {
		t.Fatalf("con lista blanca, un extraño no puede publicar: %v", err)
	}
	if err := publish(r, friend.event(1, "amigo", nil)); err != nil {
		t.Fatalf("un permitido sí: %v", err)
	}
	if nip11Doc(t, ts)["limitation"].(map[string]any)["restricted_writes"] != true {
		t.Fatal("NIP-11 debería anunciar restricted_writes")
	}
}

func TestNIP86_KindsEventsAndRelayInfo(t *testing.T) {
	owner, author := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	r := connect(t, ts)

	manage(t, ts, owner, "disallowkind", 7)
	if err := publish(r, author.event(7, "+", nil)); err == nil || !strings.Contains(err.Error(), "kind 7") {
		t.Fatalf("kind prohibido: %v", err)
	}
	manage(t, ts, owner, "allowkind", 7)
	if err := publish(r, author.event(7, "+", nil)); err != nil {
		t.Fatalf("permitido de nuevo: %v", err)
	}

	bad := author.event(1, "vetar esto", nil)
	publish(r, bad)
	manage(t, ts, owner, "banevent", bad.ID, "ilegal")
	if got, _ := fetch(t, r, nostr.Filter{IDs: []string{bad.ID}}); len(got) != 0 {
		t.Fatal("banear un evento ya guardado lo borra")
	}
	if err := publish(r, bad); err == nil || !strings.Contains(err.Error(), "banned") {
		t.Fatalf("y no puede volver: %v", err)
	}

	manage(t, ts, owner, "changerelayname", "Nombre nuevo")
	manage(t, ts, owner, "changerelaydescription", "Descripción nueva")
	doc := nip11Doc(t, ts)
	if doc["name"] != "Nombre nuevo" || doc["description"] != "Descripción nueva" {
		t.Fatalf("NIP-11 tras los cambios: %v %v", doc["name"], doc["description"])
	}
}

func TestNIP86_BannedEventsAndListsSurviveARestart(t *testing.T) {
	owner, spammer := newKeys(), newKeys()
	dir := t.TempDir()
	env := map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_DB_PATH": filepath.Join(dir, "r.sqlite")}

	srv, ts := start(t, env)
	manage(t, ts, owner, "banpubkey", spammer.pk, "spam")
	ts.Close()
	srv.Close()

	_, ts2 := start(t, env)
	if err := publish(connect(t, ts2), spammer.event(1, "otra vez", nil)); err == nil {
		t.Fatal("el ban debería persistir tras reiniciar")
	}
}

// ---------- NIP-77 (Negentropy) ----------

func TestNIP77_SyncsMoreEventsThanTheQueryLimit(t *testing.T) {
	_, ts := start(t, map[string]string{"RELAY_MAX_LIMIT": "100"})
	r := connect(t, ts)
	author := newKeys()
	const total = 350 // por encima de RELAY_MAX_LIMIT: una consulta normal devolvería 100
	now := nostr.Now()
	for i := 0; i < total; i++ {
		ev := nostr.Event{Kind: 1, Content: fmt.Sprintf("n%d", i), CreatedAt: now - nostr.Timestamp(i), PubKey: author.pk}
		ev.Sign(author.sk)
		if err := publish(r, ev); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := fetch(t, r, nostr.Filter{Kinds: []int{1}, Limit: 1000}); len(got) != 100 {
		t.Fatalf("una consulta normal se acota a RELAY_MAX_LIMIT (100): %d", len(got))
	}

	store := &slicestore.SliceStore{MaxLimit: 10000}
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	local := eventstore.RelayWrapper{Store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := nip77.NegentropySync(ctx, local, wsURL(ts), nostr.Filter{Kinds: []int{1}}, nip77.Down); err != nil {
		t.Fatalf("la sincronización NIP-77 falló: %v", err)
	}
	got, err := local.QuerySync(ctx, nostr.Filter{Kinds: []int{1}, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != total {
		t.Fatalf("la sincronización debería traer los %d eventos, trajo %d", total, len(got))
	}
}
