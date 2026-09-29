// Package config lee la configuración del relé desde variables de entorno.
// Es puro (recibe una función de lectura) para poder probarlo sin tocar el
// entorno real del proceso.
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Config reúne todo lo configurable. Los valores por defecto son los de un relé
// público pequeño: abierto para leer y escribir, con límites para que no lo
// tumbe un solo cliente.
type Config struct {
	ListenAddr string
	DBPath     string

	// Documento NIP-11 (lo que ven los clientes al consultar el relé).
	Name        string
	Description string
	PubKey      string // hex, opcional
	Contact     string // opcional

	// Límites por evento.
	MaxContentLength int           // caracteres (runas) del content
	MaxEventTags     int           // número de tags
	MaxTagValueBytes int           // bytes de cada valor de un tag
	MaxFutureSkew    time.Duration // cuánto puede adelantarse el created_at (0 = sin límite)
	AllowedKinds     []int         // vacío = todos los kinds

	// Límites por consulta (REQ). MaxLimit acota el `limit` de cada filtro.
	MaxLimit int

	// Límites de velocidad por IP: tokens por minuto y ráfaga máxima.
	EventsPerMinute, EventsBurst int
	ReqsPerMinute, ReqsBurst     int
	ConnsPerMinute, ConnsBurst   int
}

// Load construye la configuración a partir de `get` (normalmente os.Getenv).
// Devuelve un error que nombra la variable si algún valor no es válido.
func Load(get func(string) string) (Config, error) {
	c := Config{
		ListenAddr:       str(get, "RELAY_LISTEN_ADDR", ":3334"),
		DBPath:           str(get, "RELAY_DB_PATH", "./data/relay.sqlite"),
		Name:             str(get, "RELAY_NAME", "nostr-relay-khatru"),
		Description:      str(get, "RELAY_DESCRIPTION", "A small general-purpose Nostr relay built with khatru"),
		PubKey:           get("RELAY_PUBKEY"),
		Contact:          get("RELAY_CONTACT"),
		MaxContentLength: 65536,
		MaxEventTags:     2000,
		MaxTagValueBytes: 1024,
		MaxFutureSkew:    15 * time.Minute,
		MaxLimit:         500,
		EventsPerMinute:  30, EventsBurst: 60,
		ReqsPerMinute: 60, ReqsBurst: 180,
		ConnsPerMinute: 20, ConnsBurst: 60,
	}

	ints := []struct {
		name string
		dst  *int
		min  int
	}{
		{"RELAY_MAX_CONTENT_LENGTH", &c.MaxContentLength, 1},
		{"RELAY_MAX_EVENT_TAGS", &c.MaxEventTags, 1},
		{"RELAY_MAX_TAG_VALUE_BYTES", &c.MaxTagValueBytes, 1},
		{"RELAY_MAX_LIMIT", &c.MaxLimit, 1},
		{"RELAY_EVENTS_PER_MINUTE", &c.EventsPerMinute, 1},
		{"RELAY_EVENTS_BURST", &c.EventsBurst, 1},
		{"RELAY_REQS_PER_MINUTE", &c.ReqsPerMinute, 1},
		{"RELAY_REQS_BURST", &c.ReqsBurst, 1},
		{"RELAY_CONNS_PER_MINUTE", &c.ConnsPerMinute, 1},
		{"RELAY_CONNS_BURST", &c.ConnsBurst, 1},
	}
	for _, f := range ints {
		if v := strings.TrimSpace(get(f.name)); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < f.min {
				return c, fmt.Errorf("%s: %q no es un entero >= %d", f.name, v, f.min)
			}
			*f.dst = n
		}
	}

	if v := strings.TrimSpace(get("RELAY_MAX_FUTURE_SKEW_SECONDS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return c, fmt.Errorf("RELAY_MAX_FUTURE_SKEW_SECONDS: %q no es un entero >= 0 (0 = sin límite)", v)
		}
		c.MaxFutureSkew = time.Duration(n) * time.Second
	}

	if v := strings.TrimSpace(get("RELAY_ALLOWED_KINDS")); v != "" {
		for _, part := range strings.Split(v, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || n < 0 {
				return c, fmt.Errorf("RELAY_ALLOWED_KINDS: %q no es una lista de kinds separados por comas", v)
			}
			c.AllowedKinds = append(c.AllowedKinds, n)
		}
	}

	if c.PubKey != "" && len(c.PubKey) != 64 {
		return c, fmt.Errorf("RELAY_PUBKEY: debe ser una clave pública en hex de 64 caracteres")
	}
	return c, nil
}

func str(get func(string) string, key, fallback string) string {
	if v := get(key); v != "" {
		return v
	}
	return fallback
}
