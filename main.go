// nostr-relay-khatru: un relé Nostr de propósito general, pequeño y con límites,
// construido sobre khatru (https://github.com/fiatjaf/khatru) con almacenamiento SQLite.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/fiatjaf/eventstore/sqlite3"
	"github.com/fiatjaf/khatru"
	khatrupolicies "github.com/fiatjaf/khatru/policies"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip11"

	"github.com/rzazo24/nostr-relay-khatru/internal/config"
	"github.com/rzazo24/nostr-relay-khatru/internal/policies"
)

// version se sobrescribe al compilar (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatalf("configuración inválida: %v", err)
	}

	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		runHealthcheck(cfg.ListenAddr)
		return
	}

	if dir := filepath.Dir(cfg.DBPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("no se pudo crear el directorio de datos %q: %v", dir, err)
		}
	}

	relay := khatru.NewRelay()
	relay.Info.Name = cfg.Name
	relay.Info.Description = cfg.Description
	relay.Info.PubKey = cfg.PubKey
	relay.Info.Contact = cfg.Contact
	relay.Info.Software = "https://github.com/rzazo24/nostr-relay-khatru"
	relay.Info.Version = version
	relay.Info.Limitation = &nip11.RelayLimitationDocument{
		MaxContentLength: cfg.MaxContentLength,
		MaxEventTags:     cfg.MaxEventTags,
		MaxLimit:         cfg.MaxLimit,
	}

	// El backend de SQLite acota por defecto cada consulta a 100 eventos y a 10
	// valores por tag, y lo hace EN SILENCIO (un `limit: 500` devuelve 100). Se
	// alinea con el límite configurado y se sube el de valores de tag, que los
	// clientes Nostr usan mucho (#e / #p con listas largas).
	db := sqlite3.SQLite3Backend{DatabaseURL: cfg.DBPath, QueryLimit: cfg.MaxLimit, QueryTagsLimit: 500}
	if err := db.Init(); err != nil {
		log.Fatalf("no se pudo inicializar la base de datos sqlite en %q: %v", cfg.DBPath, err)
	}

	relay.StoreEvent = append(relay.StoreEvent, db.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents, func(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
		// sin `limit` (o con uno enorme) se devuelve el máximo configurado, no todo
		if filter.Limit < 1 || filter.Limit > cfg.MaxLimit {
			filter.Limit = cfg.MaxLimit
		}
		return db.QueryEvents(ctx, filter)
	})
	relay.CountEvents = append(relay.CountEvents, db.CountEvents)
	relay.DeleteEvent = append(relay.DeleteEvent, db.DeleteEvent)
	relay.ReplaceEvent = append(relay.ReplaceEvent, db.ReplaceEvent)

	// Orden importante: khatru corta en la primera política que rechaza, así que el
	// límite de velocidad va primero (barato) y las validaciones de contenido después.
	relay.RejectEvent = append(relay.RejectEvent,
		khatrupolicies.EventIPRateLimiter(cfg.EventsPerMinute, time.Minute, cfg.EventsBurst),
		policies.NewEventLimits(policies.EventLimits{
			MaxContentLength: cfg.MaxContentLength,
			MaxEventTags:     cfg.MaxEventTags,
			MaxTagValueBytes: cfg.MaxTagValueBytes,
			MaxFutureSkew:    cfg.MaxFutureSkew,
			AllowedKinds:     cfg.AllowedKinds,
		}, time.Now),
	)
	relay.RejectFilter = append(relay.RejectFilter,
		khatrupolicies.FilterIPRateLimiter(cfg.ReqsPerMinute, time.Minute, cfg.ReqsBurst),
	)
	relay.RejectConnection = append(relay.RejectConnection,
		khatrupolicies.ConnectionRateLimiter(cfg.ConnsPerMinute, time.Minute, cfg.ConnsBurst),
	)

	fmt.Printf("%s (%s) escuchando en %s, db: %s\n", cfg.Name, version, cfg.ListenAddr, cfg.DBPath)
	if err := http.ListenAndServe(cfg.ListenAddr, relay); err != nil {
		log.Fatal(err)
	}
}

// runHealthcheck se usa como HEALTHCHECK de Docker: el propio binario se
// autochequea pidiendo el documento NIP-11 a su instancia local (la imagen final
// no lleva curl ni wget). Sale con código 0 si responde 200.
func runHealthcheck(listenAddr string) {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: RELAY_LISTEN_ADDR inválido (%q): %v\n", listenAddr, err)
		os.Exit(1)
	}
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+port+"/", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Accept", "application/nostr+json")
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: estado %d\n", resp.StatusCode)
		os.Exit(1)
	}
}
