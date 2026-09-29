// Package moderation guarda las listas que se administran en caliente con la API
// de gestión NIP-86 (pubkeys bloqueados o permitidos, eventos vetados, IPs
// bloqueadas, kinds permitidos o prohibidos, y el nombre/descripción/icono del
// relé). Todo se persiste en el mismo archivo SQLite que los eventos y se mantiene
// además en memoria, porque las comprobaciones se hacen en cada evento y consulta.
package moderation

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// Nombres de lista (columna `list` de la tabla entries).
const (
	listBannedPubKey   = "banned_pubkey"
	listAllowedPubKey  = "allowed_pubkey"
	listBannedEvent    = "banned_event"
	listBlockedIP      = "blocked_ip"
	listAllowedKind    = "allowed_kind"
	listDisallowedKind = "disallowed_kind"
)

// Entry es un elemento de una lista con el motivo que se dio al añadirlo.
type Entry struct {
	Key    string
	Reason string
}

// Store es seguro para uso concurrente.
type Store struct {
	db       *sql.DB
	mu       sync.RWMutex
	lists    map[string]map[string]string // list -> key -> reason
	settings map[string]string
}

// Open abre (o crea) las tablas de moderación en el archivo SQLite `path` y carga
// todo en memoria. El archivo puede ser el mismo que usa el almacén de eventos.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS moderation_entries (list TEXT NOT NULL, key TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', PRIMARY KEY (list, key))`,
		`CREATE TABLE IF NOT EXISTS moderation_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("moderation: creando tablas: %w", err)
		}
	}

	s := &Store{db: db, lists: map[string]map[string]string{}, settings: map[string]string{}}
	rows, err := db.Query(`SELECT list, key, reason FROM moderation_entries`)
	if err != nil {
		db.Close()
		return nil, err
	}
	for rows.Next() {
		var list, key, reason string
		if err := rows.Scan(&list, &key, &reason); err != nil {
			rows.Close()
			db.Close()
			return nil, err
		}
		s.put(list, key, reason)
	}
	rows.Close()
	rows, err = db.Query(`SELECT key, value FROM moderation_settings`)
	if err != nil {
		db.Close()
		return nil, err
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			db.Close()
			return nil, err
		}
		s.settings[k] = v
	}
	rows.Close()
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) put(list, key, reason string) {
	if s.lists[list] == nil {
		s.lists[list] = map[string]string{}
	}
	s.lists[list][key] = reason
}

// add / remove tocan a la vez la memoria y el disco; si el disco falla no se cambia la memoria.
func (s *Store) add(list, key, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`INSERT INTO moderation_entries (list, key, reason) VALUES (?, ?, ?) ON CONFLICT(list, key) DO UPDATE SET reason = excluded.reason`, list, key, reason); err != nil {
		return err
	}
	s.put(list, key, reason)
	return nil
}

func (s *Store) remove(list, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`DELETE FROM moderation_entries WHERE list = ? AND key = ?`, list, key); err != nil {
		return err
	}
	delete(s.lists[list], key)
	return nil
}

func (s *Store) entries(list string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, 0, len(s.lists[list]))
	for k, r := range s.lists[list] {
		out = append(out, Entry{Key: k, Reason: r})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (s *Store) has(list, key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.lists[list][key]
	return ok
}

// --- pubkeys: banear quita de la lista de permitidos y al revés ---

func (s *Store) BanPubKey(pubkey, reason string) error {
	if err := s.remove(listAllowedPubKey, pubkey); err != nil {
		return err
	}
	return s.add(listBannedPubKey, pubkey, reason)
}
func (s *Store) AllowPubKey(pubkey, reason string) error {
	if err := s.remove(listBannedPubKey, pubkey); err != nil {
		return err
	}
	return s.add(listAllowedPubKey, pubkey, reason)
}
func (s *Store) BannedPubKeys() []Entry            { return s.entries(listBannedPubKey) }
func (s *Store) AllowedPubKeys() []Entry           { return s.entries(listAllowedPubKey) }
func (s *Store) IsPubKeyBanned(pubkey string) bool { return s.has(listBannedPubKey, pubkey) }

// HasAllowlist dice si hay lista blanca de autores: con al menos un pubkey
// permitido, SOLO esos pueden publicar (los baneados siguen sin poder).
func (s *Store) HasAllowlist() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.lists[listAllowedPubKey]) > 0
}
func (s *Store) IsPubKeyAllowed(pubkey string) bool { return s.has(listAllowedPubKey, pubkey) }

// --- eventos ---

func (s *Store) BanEvent(id, reason string) error { return s.add(listBannedEvent, id, reason) }
func (s *Store) UnbanEvent(id string) error       { return s.remove(listBannedEvent, id) }
func (s *Store) BannedEvents() []Entry            { return s.entries(listBannedEvent) }
func (s *Store) IsEventBanned(id string) bool     { return s.has(listBannedEvent, id) }

// --- IPs ---

func (s *Store) BlockIP(ip, reason string) error { return s.add(listBlockedIP, ip, reason) }
func (s *Store) UnblockIP(ip string) error       { return s.remove(listBlockedIP, ip) }
func (s *Store) BlockedIPs() []Entry             { return s.entries(listBlockedIP) }
func (s *Store) IsIPBlocked(ip string) bool      { return s.has(listBlockedIP, ip) }

// --- kinds: permitir uno lo saca de los prohibidos y al revés ---

func (s *Store) AllowKind(kind int) error {
	if err := s.remove(listDisallowedKind, strconv.Itoa(kind)); err != nil {
		return err
	}
	return s.add(listAllowedKind, strconv.Itoa(kind), "")
}
func (s *Store) DisallowKind(kind int) error {
	if err := s.remove(listAllowedKind, strconv.Itoa(kind)); err != nil {
		return err
	}
	return s.add(listDisallowedKind, strconv.Itoa(kind), "")
}
func (s *Store) AllowedKinds() []int    { return s.kinds(listAllowedKind) }
func (s *Store) DisallowedKinds() []int { return s.kinds(listDisallowedKind) }

func (s *Store) kinds(list string) []int {
	var out []int
	for _, e := range s.entries(list) {
		if n, err := strconv.Atoi(e.Key); err == nil {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

// KindBlocked decide si un kind está vetado por las listas de kinds: lo está si se
// prohibió expresamente, o si hay lista de permitidos y no está en ella.
func (s *Store) KindBlocked(kind int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k := strconv.Itoa(kind)
	if _, no := s.lists[listDisallowedKind][k]; no {
		return true
	}
	if len(s.lists[listAllowedKind]) > 0 {
		_, ok := s.lists[listAllowedKind][k]
		return !ok
	}
	return false
}

// --- ajustes (nombre, descripción, icono) ---

func (s *Store) SetSetting(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`INSERT INTO moderation_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
		return err
	}
	s.settings[key] = value
	return nil
}

func (s *Store) Setting(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.settings[key]
	return v, ok
}
