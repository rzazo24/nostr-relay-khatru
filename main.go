// nostr-relay-khatru: un relé Nostr de propósito general, pequeño y con límites,
// construido sobre khatru (https://github.com/fiatjaf/khatru) con almacenamiento SQLite.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/rzazo24/nostr-relay-khatru/internal/config"
	"github.com/rzazo24/nostr-relay-khatru/internal/server"
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

	srv, err := server.New(cfg, version)
	if err != nil {
		log.Fatal(err)
	}
	defer srv.Close()

	fmt.Printf("%s (%s) escuchando en %s, db: %s\n", cfg.Name, version, cfg.ListenAddr, cfg.DBPath)
	if err := http.ListenAndServe(cfg.ListenAddr, srv.Relay); err != nil {
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
