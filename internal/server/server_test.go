package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	"github.com/rzazo24/nostr-relay-khatru/internal/moderation"
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

// ---------- retención ----------

func oldEvent(k keys, kind int, content string, daysAgo int) nostr.Event {
	ev := nostr.Event{Kind: kind, Content: content, CreatedAt: nostr.Now() - nostr.Timestamp(daysAgo*86400), PubKey: k.pk}
	ev.Sign(k.sk)
	return ev
}

func TestRetention_RealStoreKeepsWhatItShould(t *testing.T) {
	owner, someone := newKeys(), newKeys()
	srv, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_RETENTION_DAYS": "30"})
	r := connect(t, ts)

	oldNote := oldEvent(someone, 1, "nota vieja", 100)
	oldReaction := oldEvent(someone, 7, "+", 40)
	oldProfile := oldEvent(someone, 0, `{"name":"x"}`, 400) // estado de la cuenta: se conserva
	oldOwner := oldEvent(owner, 1, "vieja del dueño", 400)  // del dueño: se conserva
	recent := oldEvent(someone, 1, "reciente", 5)
	for _, ev := range []nostr.Event{oldNote, oldReaction, oldProfile, oldOwner, recent} {
		if err := publish(r, ev); err != nil {
			t.Fatalf("publicar %q: %v", ev.Content, err)
		}
	}

	res, err := srv.RunRetentionOnce(context.Background())
	if err != nil || res.Deleted != 2 {
		t.Fatalf("debería borrar la nota y la reacción viejas: %+v %v", res, err)
	}
	for id, want := range map[string]bool{oldNote.ID: false, oldReaction.ID: false, oldProfile.ID: true, oldOwner.ID: true, recent.ID: true} {
		got, _ := fetch(t, r, nostr.Filter{IDs: []string{id}})
		if (len(got) == 1) != want {
			t.Errorf("evento %s: presente=%v, se esperaba %v", id[:8], len(got) == 1, want)
		}
	}
}

func TestRetention_OffByDefault(t *testing.T) {
	someone := newKeys()
	srv, ts := start(t, nil)
	r := connect(t, ts)
	ev := oldEvent(someone, 1, "muy vieja", 2000)
	publish(r, ev)
	res, err := srv.RunRetentionOnce(context.Background())
	if err != nil || res.Deleted != 0 {
		t.Fatalf("sin RELAY_RETENTION_DAYS no se borra nada: %+v %v", res, err)
	}
	if got, _ := fetch(t, r, nostr.Filter{IDs: []string{ev.ID}}); len(got) != 1 {
		t.Fatal("el evento debería seguir ahí")
	}
}

func TestAdminPanel_ShowsGrowthPerDayAndRetention(t *testing.T) {
	owner, someone := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_RETENTION_DAYS": "90"})
	r := connect(t, ts)
	publish(r, oldEvent(someone, 1, "hoy", 0))
	publish(r, oldEvent(someone, 1, "hoy 2", 0))
	publish(r, oldEvent(someone, 1, "hace 3 días", 3))
	cookie := adminLogin(t, ts, owner)
	_, body := adminCall(t, ts, "GET", "/admin/api/stats", "", cookie)
	perDay := body["events"].(map[string]any)["perDay"].([]any)
	if len(perDay) != 2 {
		t.Fatalf("dos días con eventos: %v", perDay)
	}
	if perDay[len(perDay)-1].(map[string]any)["count"] != float64(2) {
		t.Fatalf("hoy hay 2 eventos: %v", perDay)
	}
	if body["config"].(map[string]any)["retentionDays"] != float64(90) {
		t.Fatalf("la retención debe verse en la configuración: %v", body["config"])
	}
}

// ---------- histórico persistente ----------

func TestHistory_PersistsActivityAndSurvivesARestart(t *testing.T) {
	owner, someone := newKeys(), newKeys()
	dir := t.TempDir()
	env := map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_DB_PATH": filepath.Join(dir, "r.sqlite"), "RELAY_MAX_CONTENT_LENGTH": "20"}

	srv, ts := start(t, env)
	r := connect(t, ts)
	publish(r, someone.event(1, "uno", nil))
	publish(r, someone.event(1, "dos", nil))
	publish(r, someone.event(1, strings.Repeat("x", 50), nil)) // rechazada: demasiado larga
	time.Sleep(150 * time.Millisecond)
	srv.FlushStats(true)

	cookie := adminLogin(t, ts, owner)
	if res, _ := adminCall(t, ts, "GET", "/admin/api/history", "", ""); res.StatusCode != 401 {
		t.Fatal("el histórico exige sesión")
	}
	if res, body := adminCall(t, ts, "GET", "/admin/api/history?range=1y", "", cookie); res.StatusCode != 400 {
		t.Fatalf("un rango desconocido se rechaza: %d %v", res.StatusCode, body)
	}
	_, body := adminCall(t, ts, "GET", "/admin/api/history?range=24h", "", cookie)
	buckets := body["buckets"].([]any)
	if len(buckets) != 24 || body["step"] != float64(3600) {
		t.Fatalf("24 tramos de una hora: %d %v", len(buckets), body["step"])
	}
	totals := body["totals"].(map[string]any)
	if totals["saved"] != float64(2) || totals["rejected"] != float64(1) {
		t.Fatalf("totales guardados/rechazados: %v", totals)
	}
	if body["reasons"].(map[string]any)["invalid"] != float64(1) {
		t.Fatalf("motivos: %v", body["reasons"])
	}
	if body["dbEnd"].(float64) <= 0 || body["eventsEnd"] != float64(2) {
		t.Fatalf("tamaño de la base de datos y eventos: %v %v", body["dbEnd"], body["eventsEnd"])
	}

	// "reinicio": se cierra y se abre otro relé sobre la misma base de datos; lo anterior sigue y se suma
	ts.Close()
	srv.Close()
	srv2, ts2 := start(t, env)
	r2 := connect(t, ts2)
	publish(r2, someone.event(1, "tres", nil))
	time.Sleep(150 * time.Millisecond)
	srv2.FlushStats(false)
	cookie2 := adminLogin(t, ts2, owner)
	_, body2 := adminCall(t, ts2, "GET", "/admin/api/history?range=7d", "", cookie2)
	if body2["totals"].(map[string]any)["saved"] != float64(3) || len(body2["buckets"].([]any)) != 168 {
		t.Fatalf("tras reiniciar deben sumarse 2+1 guardados en 168 tramos: %v", body2["totals"])
	}
	_, body3 := adminCall(t, ts2, "GET", "/admin/api/history?range=30d", "", cookie2)
	if len(body3["buckets"].([]any)) != 30 || body3["step"] != float64(86400) || body3["totals"].(map[string]any)["saved"] != float64(3) {
		t.Fatalf("30 días agrupados por día: %v", body3["totals"])
	}
}

func TestHistory_HoldsNoContentOrKeys(t *testing.T) {
	owner, someone := newKeys(), newKeys()
	srv, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_MAX_CONTENT_LENGTH": "5"})
	r := connect(t, ts)
	publish(r, someone.event(1, "contenido secreto largo", nil))
	time.Sleep(100 * time.Millisecond)
	srv.FlushStats(true)
	cookie := adminLogin(t, ts, owner)
	req, _ := http.NewRequest("GET", ts.URL+"/admin/api/history?range=24h", nil)
	req.AddCookie(&http.Cookie{Name: "hs_admin", Value: cookie})
	res, _ := http.DefaultClient.Do(req)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(raw), "secreto") || strings.Contains(string(raw), someone.pk[:8]) || strings.Contains(string(raw), "127.0.0.1") {
		t.Fatalf("el histórico solo debe llevar contadores: %s", raw)
	}
}

// ---------- búsqueda en el panel ----------

func searchCall(t *testing.T, ts *httptest.Server, cookie, query string) (int, map[string]any) {
	t.Helper()
	res, body := adminCall(t, ts, "GET", "/admin/api/search?"+query, "", cookie)
	return res.StatusCode, body
}

func searchIDs(body map[string]any) []string {
	var out []string
	for _, e := range body["events"].([]any) {
		out = append(out, e.(map[string]any)["id"].(string))
	}
	return out
}

func TestAdminSearch_ByKeyIdKindPrefixAndText(t *testing.T) {
	owner, ana, bea := newKeys(), newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	r := connect(t, ts)
	cookie := adminLogin(t, ts, owner)

	profile := ana.event(0, `{"name":"ana","display_name":"Ana Pérez"}`, nil)
	note1 := ana.event(1, "Hola mundo, 100% real", nil)
	note2 := bea.event(1, "otra nota con HOLA dentro", nil)
	react := bea.event(7, "+", nil)
	dm := ana.event(4, "hola en privado", nostr.Tags{{"p", bea.pk}})
	for _, ev := range []nostr.Event{profile, note1, note2, react, dm} {
		if err := publish(r, ev); err != nil {
			t.Fatal(err)
		}
	}

	if code, _ := searchCall(t, ts, "", "q=x"); code != 401 {
		t.Fatal("la búsqueda exige sesión")
	}

	// por clave (hex y npub): sus 3 eventos, y el resumen de la clave
	npub, _ := nip19.EncodePublicKey(ana.pk)
	for _, q := range []string{ana.pk, npub} {
		code, body := searchCall(t, ts, cookie, "q="+q)
		if code != 200 || body["total"] != float64(3) {
			t.Fatalf("eventos de ana (%s…): %d %v", q[:10], code, body["total"])
		}
		key := body["key"].(map[string]any)
		if key["events"] != float64(3) || key["name"] != "Ana Pérez" || key["npub"] != npub || key["banned"] != false {
			t.Fatalf("resumen de la clave: %v", key)
		}
	}
	// por id (hex y note1)
	note, _ := nip19.EncodeNote(note2.ID)
	for _, q := range []string{note2.ID, note} {
		code, body := searchCall(t, ts, cookie, "q="+q)
		if code != 200 || body["total"] != float64(1) || searchIDs(body)[0] != note2.ID {
			t.Fatalf("por id: %d %v", code, body)
		}
	}
	// por el principio de una clave o de un id
	if _, body := searchCall(t, ts, cookie, "q="+bea.pk[:10]); body["total"] != float64(2) {
		t.Fatalf("por prefijo de clave: %v", body["total"])
	}
	if _, body := searchCall(t, ts, cookie, "q="+note1.ID[:8]); body["total"] != float64(1) {
		t.Fatalf("por prefijo de id: %v", body["total"])
	}
	// por tipo: como número y con el filtro
	if _, body := searchCall(t, ts, cookie, "q=7"); body["total"] != float64(1) {
		t.Fatalf("q=7 es el tipo 7: %v", body["total"])
	}
	if _, body := searchCall(t, ts, cookie, "q="+bea.pk+"&kind=1"); body["total"] != float64(1) {
		t.Fatalf("clave + filtro de tipo: %v", body["total"])
	}
	// texto: sin distinguir mayúsculas; los comodines se buscan tal cual; lo privado no se busca
	if _, body := searchCall(t, ts, cookie, "q=hola"); body["total"] != float64(2) {
		t.Fatalf("texto 'hola' (2 notas públicas; el mensaje privado no cuenta): %v", body["total"])
	}
	if _, body := searchCall(t, ts, cookie, "q=100%25"); body["total"] != float64(1) {
		t.Fatalf("el %% se busca literalmente: %v", body["total"])
	}
	if _, body := searchCall(t, ts, cookie, "q="+"%25"); body["total"] != float64(1) {
		t.Fatalf("un %% suelto solo encuentra el texto que lo contiene, no todo: %v", body["total"])
	}
	// el mensaje privado aparece buscando por su clave, pero sin contenido
	_, body := searchCall(t, ts, cookie, "q="+ana.pk+"&kind=4")
	ev := body["events"].([]any)[0].(map[string]any)
	if ev["kind"] != float64(4) || ev["content"] != nil && ev["content"] != "" {
		t.Fatalf("un mensaje privado no enseña su contenido: %v", ev)
	}
	// sin consulta: todo, y los botones de acción saben qué es tuyo
	if _, body := searchCall(t, ts, cookie, ""); body["total"] != float64(5) {
		t.Fatalf("sin consulta = todos: %v", body["total"])
	}
	// validación
	if code, _ := searchCall(t, ts, cookie, "q="+strings.Repeat("a", 300)); code != 400 {
		t.Fatal("una consulta demasiado larga se rechaza")
	}
	if code, _ := searchCall(t, ts, cookie, "q=npub1noesvalida"); code != 400 {
		t.Fatal("un npub inválido se rechaza")
	}
	if code, _ := searchCall(t, ts, cookie, "kind=99999"); code != 400 {
		t.Fatal("un kind fuera de rango se rechaza")
	}
	if code, _ := searchCall(t, ts, cookie, "next=basura"); code != 400 {
		t.Fatal("un cursor inválido se rechaza")
	}
}

func TestAdminSearch_PaginatesAndShowsModerationState(t *testing.T) {
	owner, spammer := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	r := connect(t, ts)
	cookie := adminLogin(t, ts, owner)
	for i := 0; i < 120; i++ {
		ev := nostr.Event{Kind: 1, Content: fmt.Sprintf("spam %d", i), CreatedAt: nostr.Now() - nostr.Timestamp(i/3), PubKey: spammer.pk} // varios con el mismo created_at
		ev.Sign(spammer.sk)
		if err := publish(r, ev); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	next := ""
	pages := 0
	for {
		code, body := searchCall(t, ts, cookie, "q="+spammer.pk+"&next="+next)
		if code != 200 {
			t.Fatalf("página %d: %d %v", pages, code, body)
		}
		for _, id := range searchIDs(body) {
			if seen[id] {
				t.Fatalf("evento repetido entre páginas: %s", id)
			}
			seen[id] = true
		}
		pages++
		if body["total"] != float64(120) {
			t.Fatalf("total: %v", body["total"])
		}
		n, _ := body["next"].(string)
		if n == "" {
			break
		}
		next = n
	}
	if len(seen) != 120 || pages != 3 {
		t.Fatalf("120 eventos en 3 páginas de 50/50/20: %d eventos, %d páginas", len(seen), pages)
	}

	adminPost(t, ts, cookie, "ban-pubkey", map[string]any{"pubkey": spammer.pk, "reason": "spam"}, nil)
	_, body := searchCall(t, ts, cookie, "q="+spammer.pk)
	key := body["key"].(map[string]any)
	if key["banned"] != true {
		t.Fatalf("el resumen debe reflejar que está baneada: %v", key)
	}
	if _, second := searchCall(t, ts, cookie, "q="+spammer.pk+"&next="+body["next"].(string)); second["key"] != nil {
		t.Fatal("el resumen de la clave solo va en la primera página")
	}
}

func TestOpenConnectionsNeverGoNegative(t *testing.T) {
	srv, ts := start(t, nil)
	for i := 0; i < 5; i++ {
		r := connect(t, ts)
		r.Close()
	}
	deadline := time.Now().Add(3 * time.Second)
	for srv.conns.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // por si llegara el segundo OnDisconnect de khatru
	if n := srv.conns.Load(); n != 0 {
		t.Fatalf("tras cerrar todas las conexiones debe haber 0 abiertas, hay %d", n)
	}
	r := connect(t, ts)
	defer r.Close()
	time.Sleep(200 * time.Millisecond)
	if n := srv.conns.Load(); n != 1 {
		t.Fatalf("con una conexión abierta debe haber 1, hay %d", n)
	}
}

func TestNIP11AdvertisesDirectoryFields(t *testing.T) {
	_, ts := start(t, map[string]string{"RELAY_CONTACT": "npub1ejemplo", "RELAY_TAGS": "general,open", "RELAY_LANGUAGES": "en,es"})
	req, _ := http.NewRequest("GET", ts.URL, nil)
	req.Header.Set("Accept", "application/nostr+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var doc struct {
		Contact string   `json:"contact"`
		Tags    []string `json:"tags"`
		Langs   []string `json:"language_tags"`
	}
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.Contact != "npub1ejemplo" || len(doc.Tags) != 2 || doc.Tags[1] != "open" || len(doc.Langs) != 2 {
		t.Fatalf("NIP-11: %+v", doc)
	}
}

func TestAdminMod_ContactTagsLanguagesAndPolicy(t *testing.T) {
	owner := newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_CONTACT": "a@b.c", "RELAY_TAGS": "general", "RELAY_LANGUAGES": "en"})
	cookie := adminLogin(t, ts, owner)
	_, lists := adminCall(t, ts, "GET", "/admin/api/moderation", "", cookie)
	info := lists["info"].(map[string]any)
	if info["contact"] != "a@b.c" || len(info["tags"].([]any)) != 1 || info["postingPolicy"] != "" {
		t.Fatalf("al principio salen los de la configuración: %v", info)
	}
	for name, body := range map[string]map[string]any{
		"idioma inválido":   {"languages": "en, no es"},
		"demasiadas":        {"tags": "a,b,c,d,e,f,g,h,i,j,k,l,m"},
		"política no https": {"postingPolicy": "http://x.example/normas"},
		"etiqueta larga":    {"tags": strings.Repeat("x", 40)},
		"lista no válida":   {"tags": 5},
	} {
		if code, _ := adminPost(t, ts, cookie, "info", body, nil); code != 400 {
			t.Fatalf("%s debe rechazarse (400), dio %d", name, code)
		}
	}
	code, out := adminPost(t, ts, cookie, "info", map[string]any{"contact": "raul@example.com", "tags": " general, open ,General,, es ", "languages": "en, es", "postingPolicy": "https://example.com/normas"}, nil)
	if code != 200 {
		t.Fatalf("info: %d %v", code, out)
	}
	doc := nip11Doc(t, ts)
	tags, langs := doc["tags"].([]any), doc["language_tags"].([]any)
	if doc["contact"] != "raul@example.com" || len(tags) != 3 || tags[1] != "open" || len(langs) != 2 || doc["posting_policy"] != "https://example.com/normas" {
		t.Fatalf("NIP-11 tras el cambio (sin repetidos ni vacíos): %v", doc)
	}
	// vaciar de verdad una lista (y el contacto) es un cambio válido
	if code, _ := adminPost(t, ts, cookie, "info", map[string]any{"tags": "", "contact": ""}, nil); code != 200 {
		t.Fatal("vaciar etiquetas y contacto es válido")
	}
	if d := nip11Doc(t, ts); d["tags"] != nil || d["contact"] != "" {
		t.Fatalf("tras vaciar: %v %v", d["tags"], d["contact"])
	}
	adminPost(t, ts, cookie, "info", map[string]any{"reset": []any{"contact", "tags", "languages", "postingPolicy"}}, nil)
	d := nip11Doc(t, ts)
	if d["contact"] != "a@b.c" || len(d["tags"].([]any)) != 1 || d["posting_policy"] != nil {
		t.Fatalf("restaurar vuelve a la configuración: %v", d)
	}
	_, lists = adminCall(t, ts, "GET", "/admin/api/moderation", "", cookie)
	if lists["infoOverrides"].(map[string]any)["tags"] != false {
		t.Fatalf("sin cambios tras restaurar: %v", lists["infoOverrides"])
	}
}

func TestAdminStats_NoisyKeysFromRealRejections(t *testing.T) {
	owner, spammer := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_MAX_CONTENT_LENGTH": "10"})
	r := connect(t, ts)
	for i := 0; i < 4; i++ {
		publish(r, spammer.event(1, strings.Repeat("x", 50), nil)) // demasiado largo: se rechaza
	}
	cookie := adminLogin(t, ts, owner)
	_, stats := adminCall(t, ts, "GET", "/admin/api/stats", "", cookie)
	noisy := stats["activity"].(map[string]any)["noisy"].([]any)
	if len(noisy) != 1 {
		t.Fatalf("una clave ruidosa: %v", noisy)
	}
	n := noisy[0].(map[string]any)
	if n["pubkey"] != spammer.pk || n["count"] != float64(4) || n["mine"] != false {
		t.Fatalf("ranking: %v", n)
	}
	if res, _ := adminCall(t, ts, "GET", "/admin/api/stats", "", ""); res.StatusCode != 401 {
		t.Fatal("sin sesión no se enseñan las claves completas")
	}
}

func TestAdminHistory_RecordsPanelAndNIP86Actions(t *testing.T) {
	owner, spammer, other := newKeys(), newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	cookie := adminLogin(t, ts, owner)
	adminPost(t, ts, cookie, "ban-pubkey", map[string]any{"pubkey": spammer.pk, "reason": "spam de prueba"}, nil)
	adminPost(t, ts, cookie, "ip", map[string]any{"ip": "203.0.113.7", "action": "block", "reason": "scraper"}, nil)
	adminPost(t, ts, cookie, "kind", map[string]any{"kind": 7, "rule": "disallow"}, nil)
	adminPost(t, ts, cookie, "info", map[string]any{"tags": "a,b", "contact": "x@y.z"}, nil)
	adminPost(t, ts, cookie, "unban-pubkey", map[string]any{"pubkey": spammer.pk}, nil)
	adminPost(t, ts, cookie, "ban-pubkey", map[string]any{"pubkey": strings.Repeat("0", 64)}, nil)
	adminPost(t, ts, cookie, "ban-pubkey", map[string]any{"pubkey": owner.pk}, nil) // rechazada: no debe anotarse
	// por NIP-86 (como lo haría un cliente de administración)
	manage(t, ts, owner, "banpubkey", other.pk, "desde nip86")

	_, lists := adminCall(t, ts, "GET", "/admin/api/moderation", "", cookie)
	hist := lists["history"].([]any)
	type row struct{ source, action, target, detail string }
	var got []row
	for _, h := range hist {
		m := h.(map[string]any)
		got = append(got, row{m["source"].(string), m["action"].(string), m["target"].(string), m["detail"].(string)})
	}
	has := func(r row) bool {
		for _, g := range got {
			if g == r {
				return true
			}
		}
		return false
	}
	for _, want := range []row{
		{"panel", "login", "", ""},
		{"panel", "ban-pubkey", spammer.pk, "spam de prueba"},
		{"panel", "ip-block", "203.0.113.7", "scraper"},
		{"panel", "kind-disallow", "7", ""},
		{"panel", "info", "", "cambiados: contacto, etiquetas"},
		{"panel", "unban-pubkey", spammer.pk, ""},
		{"nip86", "ban-pubkey", other.pk, "desde nip86"},
	} {
		if !has(want) {
			t.Fatalf("falta en el historial %+v\nhay: %+v", want, got)
		}
	}
	if len(got) != 8 { // 7 anteriores + la del ban de la clave 000…; la rechazada del dueño no cuenta
		t.Fatalf("se esperaban 8 entradas y hay %d: %+v", len(got), got)
	}
	if hist[0].(map[string]any)["source"] != "nip86" {
		t.Fatalf("la más reciente va primero: %v", hist[0])
	}
}

func TestModerationLog_KeepsOnlyTheLatest(t *testing.T) {
	st, err := moderation.Open(filepath.Join(t.TempDir(), "m.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := 0; i < 1100; i++ {
		st.LogAction("panel", "ban-event", fmt.Sprintf("%064x", i), "")
	}
	all := st.RecentActions(5000)
	if len(all) != 1000 || all[0].Target != fmt.Sprintf("%064x", 1099) || all[len(all)-1].Target != fmt.Sprintf("%064x", 100) {
		t.Fatalf("se conservan las 1000 últimas: %d, primera %s", len(all), all[0].Target[56:])
	}
}

func TestAdminStats_ServerStatusDiskAndBackups(t *testing.T) {
	owner := newKeys()
	dir := t.TempDir()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_BACKUP_DIR": dir})
	cookie := adminLogin(t, ts, owner)
	status := func() map[string]any {
		_, stats := adminCall(t, ts, "GET", "/admin/api/stats", "", cookie)
		return stats["server"].(map[string]any)
	}
	st := status()
	if st["diskTotal"].(float64) <= 0 || st["diskFree"].(float64) <= 0 || st["diskFree"].(float64) > st["diskTotal"].(float64) {
		t.Fatalf("disco: %v", st)
	}
	b := st["backup"].(map[string]any)
	if b["configured"] != true || b["count"] != float64(0) || b["last"] != float64(0) {
		t.Fatalf("sin copias: %v", b)
	}
	old, recent := filepath.Join(dir, "nostr-relay-khatru-20260101T000000Z.sqlite.gz"), filepath.Join(dir, "nostr-relay-khatru-20260102T000000Z.sqlite.gz")
	os.WriteFile(old, []byte("a"), 0o644)
	os.WriteFile(recent, []byte("bbb"), 0o644)
	os.WriteFile(filepath.Join(dir, "otra-cosa.txt"), []byte("x"), 0o644) // no cuenta
	os.Chtimes(old, time.Unix(1700000000, 0), time.Unix(1700000000, 0))
	os.Chtimes(recent, time.Unix(1800000000, 0), time.Unix(1800000000, 0))
	_, stats := adminCall(t, ts, "GET", "/admin/api/stats", "", cookie) // la caché de estadísticas puede tardar; el estado del servidor se calcula siempre
	b = stats["server"].(map[string]any)["backup"].(map[string]any)
	if b["count"] != float64(2) || b["last"] != float64(1800000000) || b["lastBytes"] != float64(3) {
		t.Fatalf("copias: %v", b)
	}
	// sin RELAY_BACKUP_DIR no se enseña nada de copias
	_, ts2 := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	cookie2 := adminLogin(t, ts2, owner)
	_, stats2 := adminCall(t, ts2, "GET", "/admin/api/stats", "", cookie2)
	if stats2["server"].(map[string]any)["backup"].(map[string]any)["configured"] != false {
		t.Fatal("sin RELAY_BACKUP_DIR las copias no están configuradas")
	}
}

func TestAdminBackup_DownloadsAConsistentGzippedCopy(t *testing.T) {
	owner, someone := newKeys(), newKeys()
	srv, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	r := connect(t, ts)
	for i := 0; i < 5; i++ {
		if err := publish(r, someone.event(1, fmt.Sprintf("nota %d", i), nil)); err != nil {
			t.Fatal(err)
		}
	}
	if res, _ := adminCall(t, ts, "GET", "/admin/api/backup", "", ""); res.StatusCode != 401 {
		t.Fatal("sin sesión no se puede descargar la base de datos")
	}
	cookie := adminLogin(t, ts, owner)
	req, _ := http.NewRequest("GET", ts.URL+"/admin/api/backup", nil)
	req.AddCookie(&http.Cookie{Name: "hs_admin", Value: cookie})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/gzip" || !strings.Contains(res.Header.Get("Content-Disposition"), "nostr-relay-khatru-") || !strings.Contains(res.Header.Get("Content-Disposition"), ".sqlite.gz") {
		t.Fatalf("respuesta: %d %v", res.StatusCode, res.Header)
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "copy.sqlite")
	f, _ := os.Create(out)
	if _, err := io.Copy(f, zr); err != nil {
		t.Fatal(err)
	}
	f.Close()
	db, err := sql.Open("sqlite3", "file:"+out+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event WHERE pubkey = ?`, someone.pk).Scan(&n); err != nil || n != 5 {
		t.Fatalf("la copia debe abrirse y tener las 5 notas: %d %v", n, err)
	}
	if ok, _ := db.Query(`PRAGMA integrity_check`); ok != nil {
		var v string
		ok.Next()
		ok.Scan(&v)
		ok.Close()
		if v != "ok" {
			t.Fatalf("integrity_check: %s", v)
		}
	}
	// no deja archivos temporales junto a la base de datos y queda anotada en el historial
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(srv.cfg.DBPath), ".panel-backup-*"))
	if len(left) != 0 {
		t.Fatalf("archivos temporales sin borrar: %v", left)
	}
	_, lists := adminCall(t, ts, "GET", "/admin/api/moderation", "", cookie)
	found := false
	for _, h := range lists["history"].([]any) {
		if h.(map[string]any)["action"] == "backup" {
			found = true
		}
	}
	if !found {
		t.Fatal("la descarga debe quedar en el historial")
	}
	// una petición de otro sitio se rechaza aunque lleve la cookie
	req2, _ := http.NewRequest("GET", ts.URL+"/admin/api/backup", nil)
	req2.AddCookie(&http.Cookie{Name: "hs_admin", Value: cookie})
	req2.Header.Set("Sec-Fetch-Site", "cross-site")
	if res2, _ := http.DefaultClient.Do(req2); res2.StatusCode != 403 {
		t.Fatalf("cross-site debe dar 403, dio %d", res2.StatusCode)
	}
}

func TestAdminSearch_DateRange(t *testing.T) {
	owner, ana := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk})
	r := connect(t, ts)
	cookie := adminLogin(t, ts, owner)
	day := int64(86400)
	base := time.Now().Unix()
	at := func(daysAgo int64, content string) nostr.Event {
		ev := nostr.Event{Kind: 1, Content: content, CreatedAt: nostr.Timestamp(base - daysAgo*day), PubKey: ana.pk}
		ev.Sign(ana.sk)
		if err := publish(r, ev); err != nil {
			t.Fatal(err)
		}
		return ev
	}
	old, mid, recent := at(10, "vieja marca"), at(5, "media marca"), at(1, "reciente marca")
	total := func(q string) (float64, []string) {
		code, body := searchCall(t, ts, cookie, q)
		if code != 200 {
			t.Fatalf("%s → %d %v", q, code, body)
		}
		return body["total"].(float64), searchIDs(body)
	}
	k := "q=" + ana.pk
	if n, _ := total(k); n != 3 {
		t.Fatalf("sin fechas: %v", n)
	}
	if n, ids := total(fmt.Sprintf("%s&since=%d", k, base-6*day)); n != 2 || ids[0] != recent.ID || ids[1] != mid.ID {
		t.Fatalf("desde hace 6 días (media y reciente): %v %v", n, ids)
	}
	if n, ids := total(fmt.Sprintf("%s&until=%d", k, base-6*day)); n != 1 || ids[0] != old.ID {
		t.Fatalf("hasta hace 6 días (solo la vieja): %v %v", n, ids)
	}
	if n, ids := total(fmt.Sprintf("%s&since=%d&until=%d", k, base-7*day, base-2*day)); n != 1 || ids[0] != mid.ID {
		t.Fatalf("entre hace 7 y 2 días (solo la media): %v %v", n, ids)
	}
	// los extremos están incluidos
	if n, _ := total(fmt.Sprintf("%s&since=%d&until=%d", k, mid.CreatedAt, mid.CreatedAt)); n != 1 {
		t.Fatalf("since=until=created_at del evento lo incluye: %v", n)
	}
	// se combina con el texto y con el tipo, y funciona sin consulta
	if n, _ := total(fmt.Sprintf("q=marca&since=%d", base-6*day)); n != 2 {
		t.Fatalf("texto + fechas: %v", n)
	}
	if n, _ := total(fmt.Sprintf("kind=1&since=%d&until=%d", base-7*day, base-2*day)); n != 1 {
		t.Fatalf("tipo + fechas sin texto: %v", n)
	}
	if n, _ := total(fmt.Sprintf("since=%d", base+day)); n != 0 {
		t.Fatalf("un rango en el futuro no da nada: %v", n)
	}
	for name, q := range map[string]string{
		"since no numérico": "since=ayer", "until negativo": "until=-5", "since > until": fmt.Sprintf("since=%d&until=%d", base, base-day),
	} {
		if code, _ := searchCall(t, ts, cookie, q); code != 400 {
			t.Fatalf("%s debe dar 400, dio %d", name, code)
		}
	}
}

func TestMailbox_Nip46MessagesAreReplayedToTheRecipient(t *testing.T) {
	_, ts := start(t, nil)
	signer, client, other := newKeys(), newKeys(), newKeys()
	pub := connect(t, ts)

	// el firmador responde cuando nadie escucha: antes se rechazaba con «mute»; ahora el buzón lo guarda
	reply := signer.event(24133, "cifrado", nostr.Tags{{"p", client.pk}})
	if err := publish(pub, reply); err != nil {
		t.Fatalf("un evento 24133 sin oyentes debe aceptarse: %v", err)
	}
	publish(pub, signer.event(24133, "para otro", nostr.Tags{{"p", other.pk}}))
	publish(pub, signer.event(20001, "efímero cualquiera", nostr.Tags{{"p", client.pk}})) // no es 24133: no se guarda

	late := connect(t, ts) // el cliente vuelve después (la página estuvo suspendida)
	got, _ := fetch(t, late, nostr.Filter{Kinds: []int{24133}, Tags: nostr.TagMap{"p": {client.pk}}})
	if len(got) != 1 || got[0].ID != reply.ID {
		t.Fatalf("debe recibir solo lo suyo: %d eventos", len(got))
	}
	if got, _ := fetch(t, late, nostr.Filter{Kinds: []int{24133}}); len(got) != 0 {
		t.Fatal("sin `#p` no se entrega nada (no se puede pedir todo lo que haya)")
	}
	if got, _ := fetch(t, late, nostr.Filter{Kinds: []int{20001}, Tags: nostr.TagMap{"p": {client.pk}}}); len(got) != 0 {
		t.Fatal("el resto de efímeros siguen sin guardarse")
	}
	if got, _ := fetch(t, late, nostr.Filter{Kinds: []int{24133}, Since: ptrTs(nostr.Now() + 100), Tags: nostr.TagMap{"p": {client.pk}}}); len(got) != 0 {
		t.Fatal("since se respeta")
	}
}

func ptrTs(t nostr.Timestamp) *nostr.Timestamp { return &t }

func TestMailbox_ExpiresAndIsBounded(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := newMailbox(func() time.Time { return now })
	k := newKeys()
	for i := 0; i < mailboxMax+50; i++ {
		ev := nostr.Event{Kind: 24133, Content: "x", CreatedAt: nostr.Timestamp(now.Unix()) + nostr.Timestamp(i), PubKey: k.pk, Tags: nostr.Tags{{"p", k.pk}}}
		ev.Sign(k.sk)
		m.Add(&ev)
	}
	f := nostr.Filter{Kinds: []int{24133}, Tags: nostr.TagMap{"p": {k.pk}}}
	if n := len(m.Query(f)); n != mailboxMax {
		t.Fatalf("máximo %d, hay %d", mailboxMax, n)
	}
	big := nostr.Event{Kind: 24133, Content: strings.Repeat("a", mailboxMaxSize+1), CreatedAt: nostr.Now(), PubKey: k.pk, Tags: nostr.Tags{{"p", k.pk}}}
	big.Sign(k.sk)
	m.Add(&big)
	if n := len(m.Query(f)); n != mailboxMax {
		t.Fatal("un mensaje demasiado grande no se guarda")
	}
	now = now.Add(mailboxTTL + time.Second)
	if n := len(m.Query(f)); n != 0 {
		t.Fatalf("a los %v desaparece todo, quedan %d", mailboxTTL, n)
	}
}

// ---------- estadísticas públicas (/stats.json) ----------

func publicGet(t *testing.T, ts *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	res, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestPublicStats(t *testing.T) {
	owner, someone := newKeys(), newKeys()
	_, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_MAX_CONTENT_LENGTH": "50"})
	r := connect(t, ts)
	publish(r, owner.event(1, "nota secreta del dueño", nil))
	publish(r, someone.event(1, "nota de un desconocido", nil))
	publish(r, someone.event(7, "+", nostr.Tags{{"e", strings.Repeat("a", 64)}}))
	publish(r, someone.event(4, "contenido privado", nostr.Tags{{"p", owner.pk}}))
	publish(r, someone.event(1, strings.Repeat("x", 80), nil)) // rechazada por tamaño
	time.Sleep(150 * time.Millisecond)

	// sin sesión ni firma
	res, raw := publicGet(t, ts, "/stats.json")
	if res.StatusCode != 200 {
		t.Fatalf("las estadísticas públicas no piden sesión: %d %s", res.StatusCode, raw)
	}
	if got := res.Header.Get("Cache-Control"); got != "public, max-age=30" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "*" || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("cabeceras: %v", res.Header)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}

	// solo cifras agregadas: nada de claves, IPs, contenido, motivos de rechazo ni tipos privados
	for _, secret := range []string{owner.pk, someone.pk, someone.pk[:8], "nota secreta", "desconocido", "contenido privado", "127.0.0.1", "recent", "rejections", "noisy", "reason", "ip"} {
		if strings.Contains(strings.ToLower(raw), strings.ToLower(secret)) {
			t.Fatalf("/stats.json no debe contener %q: %s", secret, raw)
		}
	}
	events := body["events"].(map[string]any)
	if events["total"].(float64) != 4 || events["authors"].(float64) != 2 {
		t.Fatalf("recuento: %v", events)
	}
	kinds := map[float64]float64{}
	for _, k := range events["byKind"].([]any) {
		kk := k.(map[string]any)
		kinds[kk["kind"].(float64)] = kk["count"].(float64)
	}
	if kinds[1] != 2 || kinds[7] != 1 {
		t.Fatalf("por tipo: %v", kinds)
	}
	if _, has := kinds[4]; has {
		t.Fatal("los mensajes privados no aparecen en el desglose por tipo")
	}
	if body["connections"].(float64) < 1 || body["startedAt"].(float64) <= 0 || body["now"].(float64) <= 0 {
		t.Fatalf("conexiones/arranque: %v", body)
	}
	day := body["last24h"].(map[string]any)
	hours := day["hours"].([]any)
	if len(hours) != 24 {
		t.Fatalf("24 horas: %d", len(hours))
	}
	prev := float64(0)
	for _, h := range hours {
		hh := h.(map[string]any)
		if hh["t"].(float64) <= prev {
			t.Fatal("las horas van en orden")
		}
		prev = hh["t"].(float64)
		for _, k := range []string{"saved", "ephemeral"} {
			if _, ok := hh[k]; !ok {
				t.Fatalf("falta %q en %v", k, hh)
			}
		}
	}
	for _, k := range []string{"saved", "ephemeral", "rejected"} {
		if _, ok := day[k]; !ok {
			t.Fatalf("falta el total %q: %v", k, day)
		}
	}

	// un POST no vale y una ruta cercana no existe
	if resp, err := http.Post(ts.URL+"/stats.json", "application/json", nil); err == nil {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			t.Fatal("solo GET")
		}
	}
}

func TestPublicStats_WorkWithoutAnOwner(t *testing.T) {
	_, ts := start(t, nil) // sin RELAY_PUBKEY el panel está desactivado, pero la página de inicio sigue pudiendo enseñar cifras
	res, raw := publicGet(t, ts, "/stats.json")
	if res.StatusCode != 200 || !strings.Contains(raw, `"events"`) {
		t.Fatalf("%d %s", res.StatusCode, raw)
	}
}

// ---------- claves nuevas (RELAY_NEW_KEY_HOURS) ----------

func TestNewKeys_ProbationEndToEnd(t *testing.T) {
	owner, newbie := newKeys(), newKeys()
	srv, ts := start(t, map[string]string{"RELAY_PUBKEY": owner.pk, "RELAY_NEW_KEY_HOURS": "24"})
	r := connect(t, ts)

	// una clave que el relé ve por primera vez no puede publicar notas todavía...
	err := publish(r, newbie.event(1, "mi primera nota", nil))
	if err == nil || !strings.Contains(err.Error(), "restricted:") || !strings.Contains(err.Error(), "24 hour") {
		t.Fatalf("la nota de una clave nueva se aplaza con un motivo claro: %v", err)
	}
	// ...pero sí darse de alta: perfil, reacciones, listas y borrados
	for _, ev := range []nostr.Event{
		newbie.event(0, `{"name":"nueva"}`, nil),
		newbie.event(7, "+", nostr.Tags{{"e", strings.Repeat("a", 64)}}),
		newbie.event(10002, "", nostr.Tags{{"r", "wss://relay.example.com"}}),
	} {
		if err := publish(r, ev); err != nil {
			t.Fatalf("kind %d de una clave nueva debe aceptarse: %v", ev.Kind, err)
		}
	}
	// seguir intentándolo no la «envejece» antes de tiempo
	if err := publish(r, newbie.event(1, "otra nota", nil)); err == nil {
		t.Fatal("sigue en periodo de prueba")
	}
	// el dueño nunca queda en prueba
	if err := publish(r, owner.event(1, "nota del dueño", nil)); err != nil {
		t.Fatalf("el dueño queda exento: %v", err)
	}

	// el panel marca como «nueva» la clave y no al dueño
	cookie := adminLogin(t, ts, owner)
	_, body := adminCall(t, ts, "GET", "/admin/api/stats", "", cookie)
	flags := map[string]bool{}
	for _, e := range body["events"].(map[string]any)["recent"].([]any) {
		ev := e.(map[string]any)
		flags[ev["pubkey"].(string)] = ev["newKey"].(bool)
	}
	if !flags[newbie.pk] || flags[owner.pk] {
		t.Fatalf("insignia «nueva»: %v", flags)
	}
	_, sb := adminCall(t, ts, "GET", "/admin/api/search?q="+newbie.pk, "", cookie)
	for _, e := range sb["events"].([]any) {
		if !e.(map[string]any)["newKey"].(bool) {
			t.Fatalf("en la búsqueda también: %v", e)
		}
	}
	if cfg := body["config"].(map[string]any); cfg["newKeyHours"] != float64(24) {
		t.Fatalf("la configuración muestra el periodo: %v", cfg["newKeyHours"])
	}

	// cumplido el plazo, publica; y deja de constar como nueva pasadas 24 h (el periodo de prueba es de 24 h)
	srv.Store.SetFirstSeen(newbie.pk, time.Now().Add(-25*time.Hour).Unix())
	if err := publish(r, newbie.event(1, "ya puedo", nil)); err != nil {
		t.Fatalf("pasadas 24 h puede publicar notas: %v", err)
	}
	_, body = adminCall(t, ts, "GET", "/admin/api/stats", "", cookie)
	for _, e := range body["events"].(map[string]any)["recent"].([]any) {
		if ev := e.(map[string]any); ev["pubkey"] == newbie.pk && ev["newKey"].(bool) {
			t.Fatalf("pasadas 25 h ya no es «nueva»: %v", ev)
		}
	}
}

func TestNewKeys_OffByDefault_StillRecordsStoredKeysOnly(t *testing.T) {
	newbie, flood := newKeys(), newKeys()
	srv, ts := start(t, nil) // sin RELAY_NEW_KEY_HOURS
	r := connect(t, ts)
	if err := publish(r, newbie.event(1, "hola", nil)); err != nil {
		t.Fatalf("por defecto no se aplaza nada: %v", err)
	}
	if err := publish(r, flood.event(20001, "efímero", nil)); err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.Store.FirstSeen(newbie.pk); !ok {
		t.Fatal("la clave que publica algo que se guarda consta, aunque la función esté apagada (alimenta la insignia)")
	}
	if _, ok := srv.Store.FirstSeen(flood.pk); ok {
		t.Fatal("las claves que solo mandan efímeros no se anotan")
	}
}

func TestNewKeys_BackfillOnStartup(t *testing.T) {
	old := newKeys()
	path := filepath.Join(t.TempDir(), "relay.sqlite")
	env := map[string]string{"RELAY_DB_PATH": path}
	srv, ts := start(t, env)
	r := connect(t, ts)
	ev := old.event(1, "de hace tiempo", nil)
	ev.CreatedAt = nostr.Now() - 40*3600
	ev.Sign(old.sk)
	if err := publish(r, ev); err != nil {
		t.Fatal(err)
	}
	// «borra» lo anotado para simular una base de datos de antes de la función, y reabre
	srv.Store.ForgetAllKeysForTest()
	srv.Close()
	ts.Close()
	srv2, _ := start(t, env)
	first, ok := srv2.Store.FirstSeen(old.pk)
	if !ok || time.Since(time.Unix(first, 0)) < 39*time.Hour {
		t.Fatalf("al arrancar se rellena con la fecha de su evento más antiguo: %d %v", first, ok)
	}
}
