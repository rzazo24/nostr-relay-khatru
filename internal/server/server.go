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
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/sqlite3"
	"github.com/fiatjaf/khatru"
	khatrupolicies "github.com/fiatjaf/khatru/policies"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip11"
	"github.com/nbd-wtf/go-nostr/nip86"

	"github.com/rzazo24/nostr-relay-khatru/internal/config"
	"github.com/rzazo24/nostr-relay-khatru/internal/moderation"
	"github.com/rzazo24/nostr-relay-khatru/internal/policies"
)

// Ajustes que se pueden cambiar en caliente con NIP-86 (changerelayname, etc.).
const (
	settingName        = "name"
	settingDescription = "description"
	settingIcon        = "icon"
)

// Server es el relé ya montado. Relay implementa http.Handler.
type Server struct {
	Relay *khatru.Relay
	Store *moderation.Store

	cfg     config.Config
	db      *sqlite3.SQLite3Backend
	private policies.PrivateKinds
	act     *activityLog
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

	s := &Server{cfg: cfg, db: db, Store: store, private: policies.PrivateKinds(cfg.PrivateKinds), act: newActivityLog(LogOutput, time.Now)}
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
		s.logEvent(policies.NewModeration(store)),
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
	return s, nil
}

// Close libera la moderación y el almacén.
func (s *Server) Close() {
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
			if v, ok := s.Store.Setting(settingName); ok {
				in.Name = v
			}
			if v, ok := s.Store.Setting(settingDescription); ok {
				in.Description = v
			}
			if v, ok := s.Store.Setting(settingIcon); ok {
				in.Icon = v
			}
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

	api.BanPubKey = func(ctx context.Context, pubkey, reason string) error { return st.BanPubKey(pubkey, reason) }
	api.AllowPubKey = func(ctx context.Context, pubkey, reason string) error { return st.AllowPubKey(pubkey, reason) }
	api.ListBannedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) { return toPubKeys(st.BannedPubKeys()), nil }
	api.ListAllowedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) { return toPubKeys(st.AllowedPubKeys()), nil }

	api.BanEvent = func(ctx context.Context, id, reason string) error {
		if err := st.BanEvent(id, reason); err != nil {
			return err
		}
		// además de impedir que vuelva, se borra si ya estaba guardado (se consulta el
		// almacén directamente: el filtro de eventos privados no debe esconderlo)
		ch, err := s.db.QueryEvents(ctx, nostr.Filter{IDs: []string{id}, Limit: 1})
		if err != nil {
			return err
		}
		for ev := range ch {
			if err := s.db.DeleteEvent(ctx, ev); err != nil {
				return err
			}
		}
		return nil
	}
	api.ListBannedEvents = func(ctx context.Context) ([]nip86.IDReason, error) {
		es := st.BannedEvents()
		out := make([]nip86.IDReason, len(es))
		for i, e := range es {
			out[i] = nip86.IDReason{ID: e.Key, Reason: e.Reason}
		}
		return out, nil
	}

	api.BlockIP = func(ctx context.Context, ip net.IP, reason string) error { return st.BlockIP(ip.String(), reason) }
	api.UnblockIP = func(ctx context.Context, ip net.IP, reason string) error { return st.UnblockIP(ip.String()) }
	api.ListBlockedIPs = func(ctx context.Context) ([]nip86.IPReason, error) {
		es := st.BlockedIPs()
		out := make([]nip86.IPReason, len(es))
		for i, e := range es {
			out[i] = nip86.IPReason{IP: e.Key, Reason: e.Reason}
		}
		return out, nil
	}

	api.AllowKind = func(ctx context.Context, kind int) error { return st.AllowKind(kind) }
	api.DisallowKind = func(ctx context.Context, kind int) error { return st.DisallowKind(kind) }
	api.ListAllowedKinds = func(ctx context.Context) ([]int, error) { return st.AllowedKinds(), nil }
	api.ListDisAllowedKinds = func(ctx context.Context) ([]int, error) { return st.DisallowedKinds(), nil }

	api.ChangeRelayName = func(ctx context.Context, v string) error { return st.SetSetting(settingName, v) }
	api.ChangeRelayDescription = func(ctx context.Context, v string) error { return st.SetSetting(settingDescription, v) }
	api.ChangeRelayIcon = func(ctx context.Context, v string) error { return st.SetSetting(settingIcon, v) }
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
