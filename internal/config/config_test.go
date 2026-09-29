package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoad_Defaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":3334" || c.MaxContentLength != 65536 || c.MaxLimit != 500 || c.MaxFutureSkew != 15*time.Minute {
		t.Fatalf("valores por defecto inesperados: %+v", c)
	}
	if len(c.AllowedKinds) != 0 {
		t.Fatal("por defecto se aceptan todos los kinds")
	}
	if c.MinPoW != 0 || c.AuthRequired || c.MaxNegentropyEvents != 100000 {
		t.Fatalf("NIP-13/42/77 con valores por defecto inesperados: %+v", c)
	}
	if len(c.PrivateKinds) != 2 || c.PrivateKinds[0] != 4 || c.PrivateKinds[1] != 1059 {
		t.Fatalf("los kinds privados por defecto son 4 y 1059: %v", c.PrivateKinds)
	}
}

func TestLoad_NIP13And42Options(t *testing.T) {
	c, err := Load(env(map[string]string{
		"RELAY_MIN_POW":               "20",
		"RELAY_AUTH_REQUIRED":         "true",
		"RELAY_PRIVATE_KINDS":         "4, 1059,14",
		"RELAY_PUBLIC_URL":            "https://relay.example.com/",
		"RELAY_ICON":                  " /icon.png ",
		"RELAY_MAX_NEGENTROPY_EVENTS": "5000",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.MinPoW != 20 || !c.AuthRequired || len(c.PrivateKinds) != 3 || c.PublicURL != "https://relay.example.com" || c.Icon != "/icon.png" || c.MaxNegentropyEvents != 5000 {
		t.Fatalf("opciones mal leídas: %+v", c)
	}
	none, err := Load(env(map[string]string{"RELAY_PRIVATE_KINDS": "none"}))
	if err != nil || len(none.PrivateKinds) != 0 {
		t.Fatalf("\"none\" desactiva los kinds privados: %v %v", none.PrivateKinds, err)
	}
}

func TestLoad_Overrides(t *testing.T) {
	c, err := Load(env(map[string]string{
		"RELAY_LISTEN_ADDR":             ":9999",
		"RELAY_MAX_CONTENT_LENGTH":      "1000",
		"RELAY_MAX_FUTURE_SKEW_SECONDS": "0",
		"RELAY_ALLOWED_KINDS":           "0, 1,3,7",
		"RELAY_EVENTS_PER_MINUTE":       "5",
		"RELAY_PUBKEY":                  strings.Repeat("a", 64),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":9999" || c.MaxContentLength != 1000 || c.MaxFutureSkew != 0 || c.EventsPerMinute != 5 {
		t.Fatalf("no se aplicaron los valores: %+v", c)
	}
	if len(c.AllowedKinds) != 4 || c.AllowedKinds[1] != 1 {
		t.Fatalf("kinds mal leídos: %v", c.AllowedKinds)
	}
}

func TestLoad_RejectsInvalidValuesNamingTheVariable(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"RELAY_MAX_LIMIT":               {"RELAY_MAX_LIMIT": "0"},
		"RELAY_EVENTS_BURST":            {"RELAY_EVENTS_BURST": "abc"},
		"RELAY_MAX_FUTURE_SKEW_SECONDS": {"RELAY_MAX_FUTURE_SKEW_SECONDS": "-1"},
		"RELAY_ALLOWED_KINDS":           {"RELAY_ALLOWED_KINDS": "1,x"},
		"RELAY_PUBKEY":                  {"RELAY_PUBKEY": "corta"},
		"RELAY_MIN_POW":                 {"RELAY_MIN_POW": "99"},
		"RELAY_AUTH_REQUIRED":           {"RELAY_AUTH_REQUIRED": "quizá"},
		"RELAY_PRIVATE_KINDS":           {"RELAY_PRIVATE_KINDS": "4,x"},
	} {
		_, err := Load(env(m))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: se esperaba un error que nombre la variable, hubo %v", name, err)
		}
	}
}
