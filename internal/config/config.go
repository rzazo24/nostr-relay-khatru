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
	PubKey      string // hex, opcional; también es el dueño que puede usar la API de gestión NIP-86
	Contact     string // opcional
	PublicURL   string // URL pública https del relé (para NIP-42/NIP-86); vacío = se deduce de la petición
	Icon        string // icono del relé (NIP-11): URL absoluta o ruta relativa a la URL pública (p. ej. /icon.png)

	// Límites por evento.
	MaxContentLength int           // caracteres (runas) del content
	MaxEventTags     int           // número de tags
	MaxTagValueBytes int           // bytes de cada valor de un tag
	MaxFutureSkew    time.Duration // cuánto puede adelantarse el created_at (0 = sin límite)
	AllowedKinds     []int         // vacío = todos los kinds

	// Límites por consulta (REQ). MaxLimit acota el `limit` de cada filtro.
	MaxLimit int
	// Tope de eventos que se ofrecen a una sesión de sincronización NIP-77.
	MaxNegentropyEvents int

	// Retención: los eventos regulares (notas, reacciones, mensajes...) más viejos que estos días se
	// borran; perfiles, listas y lo del dueño se conservan. 0 = se guarda todo para siempre.
	RetentionDays int

	// NIP-13: dificultad mínima de prueba de trabajo (bits a cero del id). 0 = desactivado.
	MinPoW int

	// NIP-42: AuthRequired exige autenticarse para leer y escribir. PrivateKinds son
	// los kinds cuyo contenido solo ven su autor y el destinatario (tag p), siempre
	// autenticado (por defecto los mensajes directos: 4 y 1059).
	AuthRequired bool
	PrivateKinds []int

	// Límites de velocidad por IP: tokens por minuto y ráfaga máxima.
	EventsPerMinute, EventsBurst int
	ReqsPerMinute, ReqsBurst     int
	ConnsPerMinute, ConnsBurst   int
}

// Load construye la configuración a partir de `get` (normalmente os.Getenv).
// Devuelve un error que nombra la variable si algún valor no es válido.
func Load(get func(string) string) (Config, error) {
	c := Config{
		ListenAddr:          str(get, "RELAY_LISTEN_ADDR", ":3334"),
		DBPath:              str(get, "RELAY_DB_PATH", "./data/relay.sqlite"),
		Name:                str(get, "RELAY_NAME", "nostr-relay-khatru"),
		Description:         str(get, "RELAY_DESCRIPTION", "A small general-purpose Nostr relay built with khatru"),
		PubKey:              get("RELAY_PUBKEY"),
		Contact:             get("RELAY_CONTACT"),
		PublicURL:           strings.TrimRight(get("RELAY_PUBLIC_URL"), "/"),
		Icon:                strings.TrimSpace(get("RELAY_ICON")),
		PrivateKinds:        []int{4, 1059},
		MaxContentLength:    65536,
		MaxEventTags:        2000,
		MaxTagValueBytes:    1024,
		MaxFutureSkew:       15 * time.Minute,
		MaxLimit:            500,
		MaxNegentropyEvents: 100000,
		EventsPerMinute:     30, EventsBurst: 60,
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
		{"RELAY_MAX_NEGENTROPY_EVENTS", &c.MaxNegentropyEvents, 1},
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

	if v := strings.TrimSpace(get("RELAY_RETENTION_DAYS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 36500 {
			return c, fmt.Errorf("RELAY_RETENTION_DAYS: %q no es un entero entre 0 y 36500 (0 = guardar todo)", v)
		}
		c.RetentionDays = n
	}

	if v := strings.TrimSpace(get("RELAY_MIN_POW")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 64 {
			return c, fmt.Errorf("RELAY_MIN_POW: %q no es un entero entre 0 y 64 (0 = desactivado)", v)
		}
		c.MinPoW = n
	}

	if v := strings.TrimSpace(get("RELAY_AUTH_REQUIRED")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("RELAY_AUTH_REQUIRED: %q no es true/false", v)
		}
		c.AuthRequired = b
	}

	if v := strings.TrimSpace(get("RELAY_PRIVATE_KINDS")); v != "" {
		c.PrivateKinds = nil
		if strings.ToLower(v) != "none" {
			for _, part := range strings.Split(v, ",") {
				n, err := strconv.Atoi(strings.TrimSpace(part))
				if err != nil || n < 0 {
					return c, fmt.Errorf("RELAY_PRIVATE_KINDS: %q no es una lista de kinds separados por comas (o \"none\")", v)
				}
				c.PrivateKinds = append(c.PrivateKinds, n)
			}
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
