// Package server monta el relé khatru completo a partir de la configuración: el
// almacén SQLite, las políticas, la API de gestión NIP-86, la autenticación NIP-42,
// la sincronización NIP-77 y el documento NIP-11. Vive en su propio paquete (y no en
// main) para poder levantarlo en las pruebas de integración.
package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/sqlite3"
	"github.com/fiatjaf/khatru"
	khatrupolicies "github.com/fiatjaf/khatru/policies"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip11"
	"github.com/nbd-wtf/go-nostr/nip86"

	"github.com/rzazo24/nostr-relay-khatru/internal/admin"
	"github.com/rzazo24/nostr-relay-khatru/internal/config"
	"github.com/rzazo24/nostr-relay-khatru/internal/moderation"
	"github.com/rzazo24/nostr-relay-khatru/internal/policies"
	"github.com/rzazo24/nostr-relay-khatru/internal/retention"
	"github.com/rzazo24/nostr-relay-khatru/internal/stats"
)

// Ajustes que se pueden cambiar en caliente con NIP-86 (changerelayname, etc.).
const (
	settingName        = admin.SettingName
	settingDescription = admin.SettingDescription
	settingIcon        = admin.SettingIcon
	settingContact     = admin.SettingContact
	settingTags        = admin.SettingTags
	settingLanguages   = admin.SettingLanguages
	settingPolicy      = admin.SettingPolicy
)

// Server es el relé ya montado. Relay implementa http.Handler.
type Server struct {
	Relay *khatru.Relay
	Store *moderation.Store

	cfg     config.Config
	db      *sqlite3.SQLite3Backend
	private policies.PrivateKinds
	act     *activityLog
	panel   *admin.Panel
	stop    context.CancelFunc // detiene las tareas de fondo (retención, volcado de estadísticas)
	stats   *stats.Store
	done    sync.WaitGroup // espera a que acaben las tareas de fondo al cerrar
	conns   atomic.Int64   // conexiones WebSocket abiertas ahora
	live    sync.Map       // *khatru.WebSocket de las conexiones contadas (khatru llama a OnDisconnect dos veces por conexión)
}

// New construye el relé. `version` aparece en el documento NIP-11.
func New(cfg config.Config, version string) (*Server, error) {
	if dir := filepath.Dir(cfg.DBPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("no se pudo crear el directorio de datos %q: %w", dir, err)
		}
	}

	// El backend de SQLite acota por defecto cada consulta a 100 eventos y a 10
	// valores por tag, y lo hace EN SILENCIO (un `limit: 500` devuelve 100). Su tope
	// interno se sube a lo máximo que servimos (una consulta normal o una sesión de
	// sincronización NIP-77) y los topes reales se aplican en Server.query.
	backendLimit := max(cfg.MaxLimit, cfg.MaxNegentropyEvents)
	db := &sqlite3.SQLite3Backend{DatabaseURL: cfg.DBPath, QueryLimit: backendLimit, QueryTagsLimit: 500}
	if err := db.Init(); err != nil {
		return nil, fmt.Errorf("no se pudo inicializar la base de datos sqlite en %q: %w", cfg.DBPath, err)
	}
	store, err := moderation.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir la moderación en %q: %w", cfg.DBPath, err)
	}

	statsStore, err := stats.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("no se pudo preparar el histórico de estadísticas: %w", err)
	}
	s := &Server{cfg: cfg, db: db, Store: store, stats: statsStore, private: policies.PrivateKinds(cfg.PrivateKinds), act: newActivityLog(LogOutput, time.Now)}
	relay := khatru.NewRelay()
	s.Relay = relay
	relay.ServiceURL = cfg.PublicURL
	relay.Negentropy = true // NIP-77

	s.setupInfo(version)

	// Ofrece AUTH (NIP-42) nada más conectar: así un cliente de mensajería puede
	// autenticarse sin esperar a que se le rechace una consulta. NIP-70 (eventos
	// protegidos con el tag "-") ya lo implementa khatru por su cuenta.
	relay.OnConnect = append(relay.OnConnect, khatru.RequestAuth)
	relay.OnConnect = append(relay.OnConnect, func(ctx context.Context) {
		// cuenta las autenticaciones satisfechas (para el resumen, sin identificar a nadie)
		go func() {
			conn := khatru.GetConnection(ctx)
			if conn == nil {
				return
			}
			select {
			case <-ctx.Done():
			case <-conn.Authed:
				s.act.Authenticated()
			}
		}()
	})

	relay.StoreEvent = append(relay.StoreEvent, db.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents, s.query)
	relay.CountEvents = append(relay.CountEvents, db.CountEvents)
	relay.DeleteEvent = append(relay.DeleteEvent, db.DeleteEvent)
	relay.ReplaceEvent = append(relay.ReplaceEvent, db.ReplaceEvent)

	// Orden importante: khatru corta en la primera política que rechaza, así que lo
	// más barato va primero: velocidad por IP, autenticación, moderación, tamaños y,
	// por último, la prueba de trabajo.
	relay.RejectEvent = append(relay.RejectEvent,
		s.logEvent(khatrupolicies.EventIPRateLimiter(cfg.EventsPerMinute, time.Minute, cfg.EventsBurst)),
		s.logEvent(policies.NewAuthRequiredEvent(cfg.AuthRequired, khatru.GetAuthed)),
		s.logEvent(policies.NewModeration(store, cfg.PubKey)),
		s.logEvent(policies.NewEventLimits(policies.EventLimits{
			MaxContentLength: cfg.MaxContentLength,
			MaxEventTags:     cfg.MaxEventTags,
			MaxTagValueBytes: cfg.MaxTagValueBytes,
			MaxFutureSkew:    cfg.MaxFutureSkew,
			AllowedKinds:     cfg.AllowedKinds,
		}, time.Now)),
		s.logEvent(policies.NewPoW(cfg.MinPoW)),
	)
	relay.RejectFilter = append(relay.RejectFilter,
		s.logFilter(khatrupolicies.FilterIPRateLimiter(cfg.ReqsPerMinute, time.Minute, cfg.ReqsBurst)),
		s.logFilter(policies.NewAuthRequiredFilter(cfg.AuthRequired, khatru.GetAuthed)),
		s.logFilter(s.private.NewPrivateFilter(khatru.GetAuthed)),
	)
	relay.RejectCountFilter = append(relay.RejectCountFilter,
		s.logFilter(policies.NewAuthRequiredFilter(cfg.AuthRequired, khatru.GetAuthed)),
		s.logFilter(s.private.NewPrivateFilter(khatru.GetAuthed)),
	)
	relay.OnEventSaved = append(relay.OnEventSaved, func(ctx context.Context, event *nostr.Event) { s.act.Saved() })
	relay.OnEphemeralEvent = append(relay.OnEphemeralEvent, func(ctx context.Context, event *nostr.Event) { s.act.Ephemeral() })
	relay.RejectConnection = append(relay.RejectConnection,
		khatrupolicies.ConnectionRateLimiter(cfg.ConnsPerMinute, time.Minute, cfg.ConnsBurst),
		func(r *http.Request) bool { return store.IsIPBlocked(khatru.GetIPFromRequest(r)) },
	)

	// Eventos en vivo: un evento privado nunca se reparte a quien no puede verlo,
	// aunque su suscripción sea genérica (por ejemplo sin `kinds`).
	if len(s.private) > 0 {
		relay.PreventBroadcast = append(relay.PreventBroadcast, func(ws *khatru.WebSocket, event *nostr.Event) bool {
			return !s.private.Visible(event, ws.AuthedPublicKey)
		})
	}

	s.setupManagementAPI()

	// Conexiones abiertas (para el panel de control).
	relay.OnConnect = append(relay.OnConnect, func(ctx context.Context) {
		if _, dup := s.live.LoadOrStore(khatru.GetConnection(ctx), struct{}{}); !dup {
			s.conns.Add(1)
		}
	})
	relay.OnDisconnect = append(relay.OnDisconnect, func(ctx context.Context) {
		if _, ok := s.live.LoadAndDelete(khatru.GetConnection(ctx)); ok {
			s.conns.Add(-1)
		}
	})

	// Panel de control (solo lectura, solo para el dueño): /admin/api/*
	panel, err := admin.New(admin.Options{
		Owner:        cfg.PubKey,
		PublicURL:    cfg.PublicURL,
		DBPath:       cfg.DBPath,
		BackupDir:    cfg.BackupDir,
		Version:      version,
		StartedAt:    time.Now(),
		Activity:     s.act,
		Moderation:   store,
		Store:        store,
		Effects:      s,
		Info:         s.effectiveInfo,
		InfoDefaults: infoFromConfig(cfg),
		Log:          s.act.Admin,
		Stats:        statsStore,
		Connections:  s.conns.Load,
		Config:       s.panelConfig(),
	})
	if err != nil {
		return nil, fmt.Errorf("no se pudo preparar el panel de control: %w", err)
	}
	s.panel = panel
	panel.Mount(relay.Router())

	// Retención: borra de vez en cuando los eventos regulares más viejos que RELAY_RETENTION_DAYS.
	ctx, cancel := context.WithCancel(context.Background())
	s.stop = cancel
	go retention.Start(ctx, s.db.QueryEvents, s.db.DeleteEvent, cfg.RetentionDays, cfg.PubKey, time.Hour, s.act.Retention)
	s.done.Add(1)
	go s.statsLoop(ctx)
	return s, nil
}

// panelConfig es lo que el panel muestra de la configuración (solo lectura, sin secretos).
func (s *Server) panelConfig() map[string]any {
	c := s.cfg
	return map[string]any{
		"name":                c.Name,
		"nips":                s.Relay.Info.SupportedNIPs,
		"maxContentLength":    c.MaxContentLength,
		"maxMessageBytes":     s.Relay.MaxMessageSize,
		"maxEventTags":        c.MaxEventTags,
		"maxTagValueBytes":    c.MaxTagValueBytes,
		"maxLimit":            c.MaxLimit,
		"maxNegentropyEvents": c.MaxNegentropyEvents,
		"maxFutureSkewSec":    int(c.MaxFutureSkew.Seconds()),
		"minPoW":              c.MinPoW,
		"authRequired":        c.AuthRequired,
		"privateKinds":        c.PrivateKinds,
		"allowedKinds":        c.AllowedKinds,
		"retentionDays":       c.RetentionDays,
		"eventsPerMinute":     c.EventsPerMinute,
		"eventsBurst":         c.EventsBurst,
		"reqsPerMinute":       c.ReqsPerMinute,
		"reqsBurst":           c.ReqsBurst,
		"connsPerMinute":      c.ConnsPerMinute,
		"connsBurst":          c.ConnsBurst,
	}
}

// Close libera la moderación y el almacén.
func (s *Server) Close() {
	s.stop()
	s.done.Wait() // deja que el volcado final de estadísticas termine antes de cerrar la base
	s.stats.Close()
	s.panel.Close()
	s.Store.Close()
	s.db.Close()
}

// query aplica el tope de eventos por filtro y oculta los eventos privados a quien
// no puede verlos. Las llamadas internas de khatru (para borrar o caducar eventos)
// ven todo: si no, borrar un mensaje directo propio no encontraría el evento.
func (s *Server) query(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	limit := s.cfg.MaxLimit
	if eventstore.IsNegentropySession(ctx) {
		limit = s.cfg.MaxNegentropyEvents
	}
	if filter.Limit < 1 || filter.Limit > limit {
		filter.Limit = limit
	}
	ch, err := s.db.QueryEvents(ctx, filter)
	if err != nil || ch == nil || len(s.private) == 0 || khatru.IsInternalCall(ctx) {
		return ch, err
	}

	viewer := khatru.GetAuthed(ctx)
	out := make(chan *nostr.Event)
	go func() {
		defer close(out)
		for ev := range ch {
			if !s.private.Visible(ev, viewer) {
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// setupInfo prepara el documento NIP-11: lo fijo aquí y lo que cambia en caliente
// (nombre, restricciones...) en OverwriteRelayInformation.
func (s *Server) setupInfo(version string) {
	cfg := s.cfg
	info := s.Relay.Info
	info.Name = cfg.Name
	info.Description = cfg.Description
	info.PubKey = cfg.PubKey
	info.Contact = cfg.Contact
	info.Tags, info.LanguageTags, info.PostingPolicy = cfg.Tags, cfg.Languages, cfg.PostingPolicy
	info.Icon = cfg.Icon // si es una ruta relativa, khatru la resuelve contra la URL pública
	info.Software = "https://github.com/rzazo24/nostr-relay-khatru"
	info.Version = version

	// Solo lo que de verdad hace este relé (khatru anuncia por defecto otros, como
	// el 70 sin implementarlo). khatru añade solos el 9, 45 y 77 según lo configurado.
	info.SupportedNIPs = []any{1, 9, 11, 40, 42, 45, 70, 77}
	if cfg.MinPoW > 0 {
		info.SupportedNIPs = append(info.SupportedNIPs, 13)
	}
	if cfg.PubKey != "" {
		info.SupportedNIPs = append(info.SupportedNIPs, 86)
	}

	info.Limitation = &nip11.RelayLimitationDocument{
		MaxMessageLength: int(s.Relay.MaxMessageSize),
		MaxContentLength: cfg.MaxContentLength,
		MaxEventTags:     cfg.MaxEventTags,
		MaxLimit:         cfg.MaxLimit,
		MinPowDifficulty: cfg.MinPoW,
		AuthRequired:     cfg.AuthRequired,
		// NIP-11: segundos que puede adelantarse created_at (0 = sin límite anunciado)
		CreatedAtUpperLimit: int64(cfg.MaxFutureSkew.Seconds()),
	}

	s.Relay.OverwriteRelayInformation = append(s.Relay.OverwriteRelayInformation,
		func(ctx context.Context, r *http.Request, in nip11.RelayInformationDocument) nip11.RelayInformationDocument {
			v := s.effectiveInfo()
			in.Name, in.Description, in.Contact = v.Name, v.Description, v.Contact
			if _, changed := s.Store.Setting(settingIcon); changed { // si no, se deja el que khatru ya resolvió contra la URL pública
				in.Icon = v.Icon
			}
			in.Tags, in.LanguageTags, in.PostingPolicy = v.Tags, v.Languages, v.PostingPolicy
			// Copia: no se toca el documento compartido, que es de todas las peticiones.
			lim := *in.Limitation
			lim.RestrictedWrites = s.Store.HasAllowlist()
			in.Limitation = &lim
			return in
		},
	)
}

// setupManagementAPI conecta NIP-86 con las listas de moderación. Solo puede usarla
// el dueño (RELAY_PUBKEY), autenticado con NIP-98; sin RELAY_PUBKEY queda desactivada.
func (s *Server) setupManagementAPI() {
	api := &s.Relay.ManagementAPI
	owner := s.cfg.PubKey

	api.RejectAPICall = append(api.RejectAPICall, func(ctx context.Context, mp nip86.MethodParams) (bool, string) {
		if owner == "" {
			return true, "the management API is disabled on this relay (RELAY_PUBKEY is not set)"
		}
		if khatru.GetAuthed(ctx) != owner {
			return true, "unauthorized: only the relay owner can use the management API"
		}
		return false, ""
	})

	st := s.Store
	toPubKeys := func(es []moderation.Entry) []nip86.PubKeyReason {
		out := make([]nip86.PubKeyReason, len(es))
		for i, e := range es {
			out[i] = nip86.PubKeyReason{PubKey: e.Key, Reason: e.Reason}
		}
		return out
	}

	// Lo que se hace por NIP-86 también queda en el historial del panel (con source "nip86").
	audited := func(action, target, detail string, err error) error {
		if err == nil {
			st.LogAction("nip86", action, target, detail)
		}
		return err
	}
	api.BanPubKey = func(ctx context.Context, pubkey, reason string) error {
		return audited("ban-pubkey", pubkey, reason, st.BanPubKey(pubkey, reason))
	}
	api.AllowPubKey = func(ctx context.Context, pubkey, reason string) error {
		return audited("allow-pubkey", pubkey, reason, st.AllowPubKey(pubkey, reason))
	}
	api.ListBannedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) { return toPubKeys(st.BannedPubKeys()), nil }
	api.ListAllowedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) { return toPubKeys(st.AllowedPubKeys()), nil }

	api.BanEvent = func(ctx context.Context, id, reason string) error {
		if err := st.BanEvent(id, reason); err != nil {
			return err
		}
		// además de impedir que vuelva, se borra si ya estaba guardado
		_, err := s.DeleteEventByID(ctx, id)
		return audited("ban-event", id, reason, err)
	}
	api.ListBannedEvents = func(ctx context.Context) ([]nip86.IDReason, error) {
		es := st.BannedEvents()
		out := make([]nip86.IDReason, len(es))
		for i, e := range es {
			out[i] = nip86.IDReason{ID: e.Key, Reason: e.Reason}
		}
		return out, nil
	}

	api.BlockIP = func(ctx context.Context, ip net.IP, reason string) error {
		return audited("ip-block", ip.String(), reason, st.BlockIP(ip.String(), reason))
	}
	api.UnblockIP = func(ctx context.Context, ip net.IP, reason string) error {
		return audited("ip-unblock", ip.String(), "", st.UnblockIP(ip.String()))
	}
	api.ListBlockedIPs = func(ctx context.Context) ([]nip86.IPReason, error) {
		es := st.BlockedIPs()
		out := make([]nip86.IPReason, len(es))
		for i, e := range es {
			out[i] = nip86.IPReason{IP: e.Key, Reason: e.Reason}
		}
		return out, nil
	}

	api.AllowKind = func(ctx context.Context, kind int) error {
		return audited("kind-allow", strconv.Itoa(kind), "", st.AllowKind(kind))
	}
	api.DisallowKind = func(ctx context.Context, kind int) error {
		return audited("kind-disallow", strconv.Itoa(kind), "", st.DisallowKind(kind))
	}
	api.ListAllowedKinds = func(ctx context.Context) ([]int, error) { return st.AllowedKinds(), nil }
	api.ListDisAllowedKinds = func(ctx context.Context) ([]int, error) { return st.DisallowedKinds(), nil }

	api.ChangeRelayName = func(ctx context.Context, v string) error {
		return audited("info", "", "cambiados: nombre", st.SetSetting(settingName, v))
	}
	api.ChangeRelayDescription = func(ctx context.Context, v string) error {
		return audited("info", "", "cambiados: descripción", st.SetSetting(settingDescription, v))
	}
	api.ChangeRelayIcon = func(ctx context.Context, v string) error {
		return audited("info", "", "cambiados: icono", st.SetSetting(settingIcon, v))
	}
}

// logEvent / logFilter envuelven una política para dejar constancia (sin contenido ni
// IPs) de lo que rechaza. No cambian su decisión.
func (s *Server) logEvent(p func(ctx context.Context, event *nostr.Event) (bool, string)) func(ctx context.Context, event *nostr.Event) (bool, string) {
	return func(ctx context.Context, event *nostr.Event) (bool, string) {
		reject, msg := p(ctx, event)
		if reject {
			s.act.Rejected("event", event.Kind, event.PubKey, msg)
		}
		return reject, msg
	}
}

func (s *Server) logFilter(p func(ctx context.Context, filter nostr.Filter) (bool, string)) func(ctx context.Context, filter nostr.Filter) (bool, string) {
	return func(ctx context.Context, filter nostr.Filter) (bool, string) {
		reject, msg := p(ctx, filter)
		if reject {
			kind := -1
			if len(filter.Kinds) == 1 {
				kind = filter.Kinds[0]
			}
			s.act.Rejected("filter", kind, khatru.GetAuthed(ctx), msg)
		}
		return reject, msg
	}
}

// DeleteEventByID borra un evento guardado (si existe). Consulta el almacén directamente: el
// filtro de eventos privados no debe esconderle el evento a quien modera. Implementa admin.Effects.
func (s *Server) DeleteEventByID(ctx context.Context, id string) (bool, error) {
	ch, err := s.db.QueryEvents(ctx, nostr.Filter{IDs: []string{id}, Limit: 1})
	if err != nil {
		return false, err
	}
	found := false
	for ev := range ch {
		if ev.ID != id {
			continue
		}
		if err := s.db.DeleteEvent(ctx, ev); err != nil {
			return found, err
		}
		found = true
	}
	return found, nil
}

// DeleteEventsByAuthor borra como mucho `max` eventos de un pubkey. Implementa admin.Effects.
func (s *Server) DeleteEventsByAuthor(ctx context.Context, pubkey string, max int) (int, error) {
	ch, err := s.db.QueryEvents(ctx, nostr.Filter{Authors: []string{pubkey}, Limit: max})
	if err != nil {
		return 0, err
	}
	var doomed []*nostr.Event
	for ev := range ch {
		if ev.PubKey == pubkey {
			doomed = append(doomed, ev)
		}
	}
	n := 0
	for _, ev := range doomed {
		if err := s.db.DeleteEvent(ctx, ev); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// infoFromConfig es la información del relé tal como la fija la configuración (RELAY_*).
func infoFromConfig(cfg config.Config) admin.InfoView {
	return admin.InfoView{Name: cfg.Name, Description: cfg.Description, Icon: cfg.Icon, Contact: cfg.Contact,
		Tags: cfg.Tags, Languages: cfg.Languages, PostingPolicy: cfg.PostingPolicy}
}

// splitList separa una lista guardada como "a,b,c" (vacía = ninguna).
func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// effectiveInfo es la información que anuncia NIP-11 ahora mismo (el cambio en caliente de NIP-86 o del
// panel, o si no lo hay, el de la configuración).
func (s *Server) effectiveInfo() admin.InfoView {
	v := infoFromConfig(s.cfg)
	if x, ok := s.Store.Setting(settingName); ok {
		v.Name = x
	}
	if x, ok := s.Store.Setting(settingDescription); ok {
		v.Description = x
	}
	if x, ok := s.Store.Setting(settingIcon); ok {
		v.Icon = x
	}
	if x, ok := s.Store.Setting(settingContact); ok {
		v.Contact = x
	}
	if x, ok := s.Store.Setting(settingTags); ok {
		v.Tags = splitList(x)
	}
	if x, ok := s.Store.Setting(settingLanguages); ok {
		v.Languages = splitList(x)
	}
	if x, ok := s.Store.Setting(settingPolicy); ok {
		v.PostingPolicy = x
	}
	return v
}

// RunRetentionOnce hace ahora una pasada de retención (la misma que corre cada hora) y devuelve lo
// que borró. Lo usan las pruebas.
func (s *Server) RunRetentionOnce(ctx context.Context) (retention.Result, error) {
	return retention.SweepOnce(ctx, s.db.QueryEvents, s.db.DeleteEvent, time.Now(), s.cfg.RetentionDays, s.cfg.PubKey)
}

// statsLoop vuelca cada minuto la actividad contada a la base de datos (y una vez más al cerrar).
func (s *Server) statsLoop(ctx context.Context) {
	defer s.done.Done()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	var lastEvents, lastPrune time.Time
	for {
		select {
		case <-ctx.Done():
			s.FlushStats(false)
			return
		case now := <-t.C:
			sample := now.Sub(lastEvents) >= 15*time.Minute
			s.FlushStats(sample)
			if sample {
				lastEvents = now
			}
			if now.Sub(lastPrune) >= 24*time.Hour {
				// el histórico horario se conserva un año
				s.stats.Prune(stats.Hour(now.Add(-365 * 24 * time.Hour)))
				lastPrune = now
			}
		}
	}
}

// FlushStats guarda ahora lo contado desde la última vez y las medidas actuales (conexiones, tamaño de
// la base de datos y, si sampleEvents, el número de eventos). Lo usan el bucle de fondo y las pruebas.
func (s *Server) FlushStats(sampleEvents bool) {
	if err := s.stats.Add(s.act.TakeDeltas()); err != nil {
		fmt.Fprintf(LogOutput, "stats error=%q\n", err.Error())
	}
	gauges := map[string]int64{stats.MaxConns: s.conns.Load(), stats.MaxDBBytes: s.dbBytes()}
	if sampleEvents {
		if n, err := s.stats.EventCount(); err == nil {
			gauges[stats.MaxEvents] = n
		}
	}
	if err := s.stats.SetMax(stats.Hour(time.Now()), gauges); err != nil {
		fmt.Fprintf(LogOutput, "stats error=%q\n", err.Error())
	}
}

func (s *Server) dbBytes() int64 {
	var total int64
	for _, suffix := range []string{"", "-wal"} {
		if fi, err := os.Stat(s.cfg.DBPath + suffix); err == nil {
			total += fi.Size()
		}
	}
	return total
}
