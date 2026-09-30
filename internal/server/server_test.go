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
	"sync"
	"testing"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip13"
	"github.com/nbd-wtf/go-nostr/nip19"
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

func TestNIP11_IconRelativePathIsResolvedAgainstThePublicURL(t *testing.T) {
	_, ts := start(t, map[string]string{"RELAY_ICON": "/icon.png"})
	if got := nip11Doc(t, ts)["icon"]; got != ts.URL+"/icon.png" {
		t.Fatalf("icon: %v, se esperaba %s/icon.png", got, ts.URL)
	}
	_, ts = start(t, map[string]string{"RELAY_ICON": "https://example.com/logo.webp"})
	if got := nip11Doc(t, ts)["icon"]; got != "https://example.com/logo.webp" {
		t.Fatalf("una URL absoluta se deja tal cual: %v", got)
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

// ---------- registro de actividad ----------

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestActivityLog_RecordsRejectionsWithoutContentOrIPs(t *testing.T) {
	out := &syncBuffer{}
	prev := LogOutput
	LogOutput = out
	defer func() { LogOutput = prev }()

	_, ts := start(t, map[string]string{"RELAY_MAX_CONTENT_LENGTH": "10"})
	r := connect(t, ts)
	k := newKeys()
	if err := publish(r, k.event(1, "un texto secreto demasiado largo", nil)); err == nil {
		t.Fatal("debería rechazarse")
	}
	time.Sleep(100 * time.Millisecond)

	log := out.String()
	if !strings.Contains(log, "reject event kind=1 pubkey="+k.pk[:8]) || !strings.Contains(log, "too long") {
		t.Fatalf("falta la línea de rechazo:\n%s", log)
	}
	if strings.Contains(log, "secreto") || strings.Contains(log, "127.0.0.1") || strings.Contains(log, k.pk) {
		t.Fatalf("el registro no debe llevar el contenido, las IPs ni el pubkey completo:\n%s", log)
	}
}

// ---------- panel de control (/admin/api) ----------

// adminAuth firma un evento NIP-98 para llamar a `path` con `method`.
func adminAuth(caller keys, ts *httptest.Server, method, path string) string {
	ev := caller.event(27235, "", nostr.Tags{{"u", ts.URL + path}, {"method", method}})
	j, _ := json.Marshal(ev)
	return "Nostr " + base64.StdEncoding.EncodeToString(j)
}

func adminCall(t *testing.T, ts *httptest.Server, method, path, auth, cookie string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "hs_admin", Value: cookie})
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res, out
}

func adminLogin(t *testing.T, ts *httptest.Server, owner keys) string {
	t.Helper()
	res, body := adminCall(t, ts, "POST", "/admin/api/login", adminAuth(owner, ts, "POST", "/admin/api/login"), "")
	if res.StatusCode != 200 {
		t.Fatalf("el login del dueño debería funcionar: %d %v", res.StatusCode, body)
	}
	for _, c := range res.Cookies() {
		if c.Name == "hs_admin" {
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Fatalf("la cookie debe ser HttpOnly y SameSite=Strict: %+v", c)
			}
			return c.Value
		}
	}
	t.Fatal("no se estableció la cookie de sesión")
	return ""
}

func TestAdminPanel_LoginRules(t *testing.T) {
	owner, stranger := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})

	if res, _ := adminCall(t, ts, "GET", "/admin/api/stats", "", ""); res.StatusCode != 401 {
		t.Fatalf("las estadísticas exigen sesión: %d", res.StatusCode)
	}
	if res, body := adminCall(t, ts, "POST", "/admin/api/login", adminAuth(stranger, ts, "POST", "/admin/api/login"), ""); res.StatusCode != 401 || !strings.Contains(fmt.Sprint(body["error"]), "owner") {
		t.Fatalf("un extraño no puede entrar: %d %v", res.StatusCode, body)
	}
	// firma para otra URL, otro método, caducada o repetida
	if res, _ := adminCall(t, ts, "POST", "/admin/api/login", adminAuth(owner, ts, "POST", "/admin/api/otra"), ""); res.StatusCode != 401 {
		t.Fatal("una firma para otra URL no vale")
	}
	if res, _ := adminCall(t, ts, "POST", "/admin/api/login", adminAuth(owner, ts, "GET", "/admin/api/login"), ""); res.StatusCode != 401 {
		t.Fatal("una firma para otro método no vale")
	}
	old := owner.event(27235, "", nostr.Tags{{"u", ts.URL + "/admin/api/login"}, {"method", "POST"}})
	old.CreatedAt = nostr.Now() - 600
	old.Sign(owner.sk)
	oj, _ := json.Marshal(old)
	if res, _ := adminCall(t, ts, "POST", "/admin/api/login", "Nostr "+base64.StdEncoding.EncodeToString(oj), ""); res.StatusCode != 401 {
		t.Fatal("una firma de hace 10 minutos no vale")
	}
	auth := adminAuth(owner, ts, "POST", "/admin/api/login")
	if res, _ := adminCall(t, ts, "POST", "/admin/api/login", auth, ""); res.StatusCode != 200 {
		t.Fatal("la primera vez sí")
	}
	if res, body := adminCall(t, ts, "POST", "/admin/api/login", auth, ""); res.StatusCode != 401 || !strings.Contains(fmt.Sprint(body["error"]), "already used") {
		t.Fatalf("la misma firma no puede repetirse: %d %v", res.StatusCode, body)
	}
}

func TestAdminPanel_DisabledWithoutAnOwner(t *testing.T) {
	_, ts := start(t, nil)
	if res, _ := adminCall(t, ts, "POST", "/admin/api/login", adminAuth(newKeys(), ts, "POST", "/admin/api/login"), ""); res.StatusCode != 403 {
		t.Fatalf("sin RELAY_PUBKEY el panel está desactivado: %d", res.StatusCode)
	}
	if res, _ := adminCall(t, ts, "GET", "/admin/api/stats", "", "cualquiera"); res.StatusCode != 403 {
		t.Fatal("y las estadísticas también")
	}
}

func TestAdminPanel_SessionsAndStats(t *testing.T) {
	owner, someone := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_MAX_CONTENT_LENGTH": "50"})
	r := connect(t, ts)

	publish(r, owner.event(1, "nota del dueño", nil))
	publish(r, someone.event(1, "nota de un desconocido\ncon salto", nil))
	publish(r, someone.event(4, "contenido privado", nostr.Tags{{"p", owner.pk}}))
	publish(r, someone.event(1, strings.Repeat("x", 80), nil)) // rechazada por tamaño
	time.Sleep(150 * time.Millisecond)

	cookie := adminLogin(t, ts, owner)
	if res, _ := adminCall(t, ts, "GET", "/admin/api/session", "", cookie); res.StatusCode != 200 {
		t.Fatal("la sesión debería valer")
	}
	if res, _ := adminCall(t, ts, "GET", "/admin/api/stats", "", "token-falso"); res.StatusCode != 401 {
		t.Fatal("un token inventado no vale")
	}

	res, body := adminCall(t, ts, "GET", "/admin/api/stats", "", cookie)
	if res.StatusCode != 200 || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("stats: %d, cache=%q", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	events := body["events"].(map[string]any)
	if events["total"].(float64) != 3 || events["pubkeys"].(float64) != 2 {
		t.Fatalf("recuento de eventos: %v", events)
	}
	var sawPrivate, sawStranger bool
	for _, e := range events["recent"].([]any) {
		ev := e.(map[string]any)
		if ev["kind"].(float64) == 4 {
			sawPrivate = true
			if _, has := ev["content"]; has {
				t.Fatal("el contenido de un mensaje privado no debe llegar al panel")
			}
		}
		if ev["pubkey"] == someone.pk && ev["kind"].(float64) == 1 {
			sawStranger = true
			if strings.Contains(fmt.Sprint(ev["content"]), "\n") || ev["mine"] != false {
				t.Fatalf("el desconocido: %v", ev)
			}
		}
	}
	if !sawPrivate || !sawStranger {
		t.Fatalf("faltan eventos recientes: %v", events["recent"])
	}
	activity := body["activity"].(map[string]any)
	mod := body["moderation"].(map[string]any)
	if _, isArray := mod["allowedKinds"].([]any); !isArray {
		t.Fatalf("una lista vacía debe ser [] y no null: %v", mod["allowedKinds"])
	}
	if len(activity["minutes"].([]any)) != 60 {
		t.Fatal("el histórico son 60 minutos")
	}
	if activity["reasons"].(map[string]any)["invalid"] != float64(1) {
		t.Fatalf("debería constar el rechazo por tamaño: %v", activity["reasons"])
	}
	rej := activity["rejections"].([]any)
	if len(rej) == 0 || rej[0].(map[string]any)["pubkey"] != someone.pk[:8] || len(rej[0].(map[string]any)["pubkey"].(string)) != 8 {
		t.Fatalf("los rechazos llevan solo un trozo del pubkey: %v", rej)
	}
	if body["config"].(map[string]any)["maxContentLength"] != float64(50) || body["connections"].(float64) < 1 {
		t.Fatalf("config/conexiones: %v %v", body["config"], body["connections"])
	}

	// cerrar sesión invalida el token
	adminCall(t, ts, "POST", "/admin/api/logout", "", cookie)
	if res, _ := adminCall(t, ts, "GET", "/admin/api/stats", "", cookie); res.StatusCode != 401 {
		t.Fatal("tras cerrar sesión el token ya no vale")
	}
}

func TestAdminPanel_ReadOnlyAndNoInterferenceWithNostr(t *testing.T) {
	owner := newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	r := connect(t, ts)
	cookie := adminLogin(t, ts, owner)
	// el panel usa la misma base de datos: publicar mientras se consultan estadísticas debe seguir funcionando
	for i := 0; i < 5; i++ {
		if err := publish(r, owner.event(1, fmt.Sprintf("n%d", i), nil)); err != nil {
			t.Fatal(err)
		}
		if res, _ := adminCall(t, ts, "GET", "/admin/api/stats", "", cookie); res.StatusCode != 200 {
			t.Fatal("las estadísticas deberían seguir respondiendo")
		}
	}
	if nip11Doc(t, ts)["name"] == nil {
		t.Fatal("NIP-11 debe seguir funcionando junto a /admin")
	}
}

// ---------- panel de control: moderación (fase 2) ----------

// adminPost hace una acción del panel (POST JSON con la cookie de sesión).
func adminPost(t *testing.T, ts *httptest.Server, cookie, path string, body map[string]any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	j, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", ts.URL+"/admin/api/mod/"+path, bytes.NewReader(j))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "hs_admin", Value: cookie})
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestAdminMod_RequiresSessionSameOriginAndJSON(t *testing.T) {
	owner, spammer := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	cookie := adminLogin(t, ts, owner)
	body := map[string]any{"pubkey": spammer.pk}

	if code, _ := adminPost(t, ts, "", "ban-pubkey", body, nil); code != 401 {
		t.Fatalf("sin sesión: %d", code)
	}
	if code, _ := adminPost(t, ts, "token-falso", "ban-pubkey", body, nil); code != 401 {
		t.Fatalf("token falso: %d", code)
	}
	if code, _ := adminPost(t, ts, cookie, "ban-pubkey", body, map[string]string{"Origin": "https://evil.example"}); code != 403 {
		t.Fatalf("otro origen debe rechazarse: %d", code)
	}
	if code, _ := adminPost(t, ts, cookie, "ban-pubkey", body, map[string]string{"Sec-Fetch-Site": "cross-site"}); code != 403 {
		t.Fatalf("una petición entre sitios debe rechazarse: %d", code)
	}
	if code, _ := adminPost(t, ts, cookie, "ban-pubkey", body, map[string]string{"Content-Type": "text/plain"}); code != 415 {
		t.Fatalf("solo JSON: %d", code)
	}
	// desde el propio origen sí
	if code, out := adminPost(t, ts, cookie, "ban-pubkey", body, map[string]string{"Origin": ts.URL, "Sec-Fetch-Site": "same-origin"}); code != 200 || out["ok"] != true {
		t.Fatalf("la petición legítima debe funcionar: %d %v", code, out)
	}
}

func TestAdminMod_BanUnbanPubkeyAndDeleteTheirEvents(t *testing.T) {
	owner, spammer := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	r := connect(t, ts)
	cookie := adminLogin(t, ts, owner)

	for i := 0; i < 3; i++ {
		publish(r, spammer.event(1, fmt.Sprintf("spam %d", i), nil))
	}
	publish(r, owner.event(1, "nota del dueño", nil))

	// el dueño no se puede banear a sí mismo
	if code, out := adminPost(t, ts, cookie, "ban-pubkey", map[string]any{"pubkey": owner.pk}, nil); code != 400 || !strings.Contains(fmt.Sprint(out["error"]), "owner") {
		t.Fatalf("no se puede banear al dueño: %d %v", code, out)
	}
	// datos inválidos
	if code, _ := adminPost(t, ts, cookie, "ban-pubkey", map[string]any{"pubkey": "no-es-una-clave"}, nil); code != 400 {
		t.Fatal("una clave inválida debe rechazarse")
	}
	if code, _ := adminPost(t, ts, cookie, "ban-pubkey", map[string]any{"pubkey": spammer.pk, "reason": strings.Repeat("x", 500)}, nil); code != 400 {
		t.Fatal("un motivo demasiado largo debe rechazarse")
	}

	// se banea aceptando un npub y borrando sus eventos guardados
	npub, _ := nip19.EncodePublicKey(spammer.pk)
	code, out := adminPost(t, ts, cookie, "ban-pubkey", map[string]any{"pubkey": npub, "reason": "spam", "deleteEvents": true}, nil)
	if code != 200 || out["deleted"] != float64(3) {
		t.Fatalf("debería banear y borrar 3 eventos: %d %v", code, out)
	}
	if got, _ := fetch(t, r, nostr.Filter{Authors: []string{spammer.pk}}); len(got) != 0 {
		t.Fatalf("sus eventos deberían haberse borrado: %d", len(got))
	}
	if got, _ := fetch(t, r, nostr.Filter{Authors: []string{owner.pk}}); len(got) != 1 {
		t.Fatal("los del dueño no se tocan")
	}
	if err := publish(r, spammer.event(1, "otra vez", nil)); err == nil || !strings.Contains(err.Error(), "banned") {
		t.Fatalf("baneado no puede publicar: %v", err)
	}

	// aparece en la lista con su motivo y se puede quitar sin activar la lista blanca
	_, lists := adminCall(t, ts, "GET", "/admin/api/moderation", "", cookie)
	if _, isArray := lists["disallowedKinds"].([]any); !isArray {
		t.Fatalf("disallowedKinds vacío debe ser []: %v", lists["disallowedKinds"])
	}
	banned := lists["bannedPubkeys"].([]any)
	if len(banned) != 1 || banned[0].(map[string]any)["reason"] != "spam" || banned[0].(map[string]any)["key"] != spammer.pk {
		t.Fatalf("lista de baneados: %v", banned)
	}
	adminPost(t, ts, cookie, "unban-pubkey", map[string]any{"pubkey": spammer.pk}, nil)
	if err := publish(r, spammer.event(1, "perdonado", nil)); err != nil {
		t.Fatalf("tras quitar el baneo puede publicar (y el relé sigue abierto): %v", err)
	}
	if nip11Doc(t, ts)["limitation"].(map[string]any)["restricted_writes"] != false {
		t.Fatal("quitar un baneo no debe activar la lista blanca")
	}
}

func TestAdminMod_AllowlistNeverLocksTheOwnerOut(t *testing.T) {
	owner, friend, stranger := newKeys(), newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	r := connect(t, ts)
	cookie := adminLogin(t, ts, owner)

	adminPost(t, ts, cookie, "allow-pubkey", map[string]any{"pubkey": friend.pk, "reason": "amigo"}, nil)
	if err := publish(r, stranger.event(1, "extraño", nil)); err == nil || !strings.Contains(err.Error(), "restricted") {
		t.Fatalf("con lista blanca, un extraño no publica: %v", err)
	}
	if err := publish(r, friend.event(1, "amigo", nil)); err != nil {
		t.Fatalf("el permitido sí: %v", err)
	}
	if err := publish(r, owner.event(1, "dueño", nil)); err != nil {
		t.Fatalf("el dueño nunca queda fuera de su propio relé: %v", err)
	}
	adminPost(t, ts, cookie, "unallow-pubkey", map[string]any{"pubkey": friend.pk}, nil)
	if err := publish(r, stranger.event(1, "ya abierto", nil)); err != nil {
		t.Fatalf("sin lista blanca vuelve a ser abierto: %v", err)
	}
}

func TestAdminMod_EventsKindsAndIPs(t *testing.T) {
	owner, author := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	r := connect(t, ts)
	cookie := adminLogin(t, ts, owner)

	bad := author.event(1, "vetar", nil)
	publish(r, bad)
	note, _ := nip19.EncodeNote(bad.ID)
	code, out := adminPost(t, ts, cookie, "ban-event", map[string]any{"id": note, "reason": "ilegal"}, nil)
	if code != 200 || out["deleted"] != true {
		t.Fatalf("vetar un evento guardado lo borra: %d %v", code, out)
	}
	if err := publish(r, bad); err == nil || !strings.Contains(err.Error(), "banned") {
		t.Fatalf("y no puede volver: %v", err)
	}
	adminPost(t, ts, cookie, "unban-event", map[string]any{"id": bad.ID}, nil)
	if err := publish(r, bad); err != nil {
		t.Fatalf("tras quitar el veto vuelve a aceptarse: %v", err)
	}

	adminPost(t, ts, cookie, "kind", map[string]any{"kind": 7, "rule": "disallow"}, nil)
	if err := publish(r, author.event(7, "+", nil)); err == nil || !strings.Contains(err.Error(), "kind 7") {
		t.Fatalf("kind prohibido: %v", err)
	}
	adminPost(t, ts, cookie, "kind", map[string]any{"kind": 7, "rule": "clear"}, nil)
	if err := publish(r, author.event(7, "+", nil)); err != nil {
		t.Fatalf("regla quitada: %v", err)
	}
	if code, _ := adminPost(t, ts, cookie, "kind", map[string]any{"kind": 99999, "rule": "allow"}, nil); code != 400 {
		t.Fatal("un kind fuera de rango se rechaza")
	}
	if code, _ := adminPost(t, ts, cookie, "kind", map[string]any{"kind": 7, "rule": "borrar"}, nil); code != 400 {
		t.Fatal("una regla desconocida se rechaza")
	}

	if code, _ := adminPost(t, ts, cookie, "ip", map[string]any{"ip": "no-es-ip", "action": "block"}, nil); code != 400 {
		t.Fatal("una IP inválida se rechaza")
	}
	adminPost(t, ts, cookie, "ip", map[string]any{"ip": "203.0.113.7", "action": "block", "reason": "abuso"}, nil)
	_, lists := adminCall(t, ts, "GET", "/admin/api/moderation", "", cookie)
	ips := lists["blockedIPs"].([]any)
	if len(ips) != 1 || ips[0].(map[string]any)["key"] != "203.0.113.7" {
		t.Fatalf("IPs bloqueadas: %v", ips)
	}
	adminPost(t, ts, cookie, "ip", map[string]any{"ip": "203.0.113.7", "action": "unblock"}, nil)
	_, lists = adminCall(t, ts, "GET", "/admin/api/moderation", "", cookie)
	if len(lists["blockedIPs"].([]any)) != 0 {
		t.Fatal("IP desbloqueada")
	}
}

func TestAdminMod_RelayInfoAndReset(t *testing.T) {
	owner := newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_NAME": "Original", "RELAY_DESCRIPTION": "Desc original"})
	cookie := adminLogin(t, ts, owner)

	if code, _ := adminPost(t, ts, cookie, "info", map[string]any{"name": ""}, nil); code != 400 {
		t.Fatal("el nombre no puede quedar vacío")
	}
	if code, _ := adminPost(t, ts, cookie, "info", map[string]any{"icon": "javascript:alert(1)"}, nil); code != 400 {
		t.Fatal("un icono que no es https ni ruta se rechaza")
	}
	code, out := adminPost(t, ts, cookie, "info", map[string]any{"name": "Nuevo nombre", "description": "EN | ES", "icon": "https://example.com/i.png"}, nil)
	if code != 200 {
		t.Fatalf("info: %d %v", code, out)
	}
	doc := nip11Doc(t, ts)
	if doc["name"] != "Nuevo nombre" || doc["description"] != "EN | ES" || doc["icon"] != "https://example.com/i.png" {
		t.Fatalf("NIP-11 tras el cambio: %v %v %v", doc["name"], doc["description"], doc["icon"])
	}
	_, lists := adminCall(t, ts, "GET", "/admin/api/moderation", "", cookie)
	if lists["info"].(map[string]any)["name"] != "Nuevo nombre" || lists["infoDefaults"].(map[string]any)["name"] != "Original" || lists["infoOverrides"].(map[string]any)["name"] != true {
		t.Fatalf("info/valores por defecto/cambios: %v", lists)
	}
	if code, _ := adminPost(t, ts, cookie, "info", map[string]any{"icon": ""}, nil); code != 200 || nip11Doc(t, ts)["icon"] != "" {
		t.Fatalf("un icono vacío quita el icono: %v", nip11Doc(t, ts)["icon"])
	}
	adminPost(t, ts, cookie, "info", map[string]any{"reset": []any{"name", "description", "icon"}}, nil)
	doc = nip11Doc(t, ts)
	if doc["name"] != "Original" || doc["description"] != "Desc original" {
		t.Fatalf("tras restaurar vuelve a la configuración: %v %v", doc["name"], doc["description"])
	}
}

func TestAdminMod_ActionsAreLoggedWithoutFullKeys(t *testing.T) {
	out := &syncBuffer{}
	prev := LogOutput
	LogOutput = out
	defer func() { LogOutput = prev }()
	owner, spammer := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	cookie := adminLogin(t, ts, owner)
	adminPost(t, ts, cookie, "ban-pubkey", map[string]any{"pubkey": spammer.pk, "reason": "motivo secreto"}, nil)
	log := out.String()
	if !strings.Contains(log, "admin action=ban-pubkey target="+spammer.pk[:8]) {
		t.Fatalf("falta la línea de auditoría:\n%s", log)
	}
	if strings.Contains(log, spammer.pk) || strings.Contains(log, "secreto") {
		t.Fatalf("el registro no debe llevar la clave completa ni el motivo:\n%s", log)
	}
}
