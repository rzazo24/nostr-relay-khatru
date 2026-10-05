package moderation

import (
	"time"
)

// Primera vez que el relé ve cada clave (pubkey). Nostr no tiene «edad de cuenta»: una clave recién generada y una de hace años son
// iguales y cualquier evento se puede fechar a mano. Lo único que un relé sabe de verdad es CUÁNDO LA VIO ÉL POR PRIMERA VEZ, que es lo
// que se guarda aquí (con la hora de recepción, no con el `created_at` del evento, que lo pone quien lo firma).
//
// Sirve para dos cosas: señalar en el panel las claves «nuevas» y, si el dueño lo activa (RELAY_NEW_KEY_HOURS), aplazar las notas de
// una clave hasta que lleve un tiempo conocida. Solo se anotan claves que publican eventos que se guardan (los efímeros no cuentan:
// una inundación de claves de usar y tirar llenaría la tabla), y solo guarda una fila por clave.

// FirstSeen devuelve cuándo (unix) se vio por primera vez la clave, y false si nunca se ha visto.
func (s *Store) FirstSeen(pubkey string) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.firstSeen[pubkey]
	return t, ok
}

// SeeKey anota que la clave publica ahora si es la primera vez, y devuelve su primera vez (la de ahora o la anterior).
func (s *Store) SeeKey(pubkey string, now time.Time) (first int64, wasNew bool) {
	if t, ok := s.FirstSeen(pubkey); ok {
		return t, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.firstSeen[pubkey]; ok { // otro la anotó mientras tanto
		return t, false
	}
	t := now.Unix()
	// Un fallo al guardar no debe impedir publicar: se recuerda en memoria igualmente.
	_, _ = s.db.Exec(`INSERT OR IGNORE INTO key_first_seen (pubkey, first_seen) VALUES (?, ?)`, pubkey, t)
	s.firstSeen[pubkey] = t
	return t, true
}

// BackfillFirstSeen da una primera vez aproximada a las claves que ya tienen eventos guardados y no constan: la fecha de su evento más
// antiguo (como mucho, la de ahora: un evento puede venir fechado en el futuro). Se llama una vez al arrancar, cuando ya existe la tabla de
// eventos; si todavía no existe, no pasa nada. Devuelve cuántas claves ha añadido.
func (s *Store) BackfillFirstSeen(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`INSERT OR IGNORE INTO key_first_seen (pubkey, first_seen) SELECT pubkey, MIN(MIN(created_at), ?) FROM event GROUP BY pubkey`, now.Unix())
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		s.loadFirstSeenLocked()
	}
	return int(n)
}

func (s *Store) loadFirstSeenLocked() {
	rows, err := s.db.Query(`SELECT pubkey, first_seen FROM key_first_seen`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var pk string
		var t int64
		if rows.Scan(&pk, &t) == nil {
			s.firstSeen[pk] = t
		}
	}
}

// SetFirstSeen fija a mano cuándo se vio por primera vez una clave (las pruebas lo usan para «envejecerla»; también serviría para dar
// por conocida a una clave de confianza sin esperar el plazo).
func (s *Store) SetFirstSeen(pubkey string, t int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(`INSERT INTO key_first_seen (pubkey, first_seen) VALUES (?, ?) ON CONFLICT(pubkey) DO UPDATE SET first_seen = excluded.first_seen`, pubkey, t)
	s.firstSeen[pubkey] = t
}

// CountKnownKeys es cuántas claves se conocen (para las pruebas y el panel).
func (s *Store) CountKnownKeys() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.firstSeen)
}

// ForgetAllKeysForTest vacía el registro (solo para las pruebas: simula una base de datos anterior a la función).
func (s *Store) ForgetAllKeysForTest() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(`DELETE FROM key_first_seen`)
	s.firstSeen = map[string]int64{}
}
