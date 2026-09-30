package server

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rzazo24/nostr-relay-khatru/internal/admin"
	"github.com/rzazo24/nostr-relay-khatru/internal/retention"
)

// LogOutput es dónde escribe el registro de actividad (docker compose logs lo recoge
// de la salida estándar). Las pruebas lo cambian para leerlo.
var LogOutput io.Writer = os.Stdout

// Cuántas líneas de rechazo se imprimen como máximo por motivo y por minuto: lo demás
// solo se cuenta en el resumen, para que un abuso no llene el disco de logs.
const maxRejectLinesPerReasonPerMinute = 5

// activityLog deja constancia de qué hace el relé sin registrar contenido ni IPs:
// solo el kind, un trozo corto del pubkey y el motivo del rechazo. Así se ve, por
// ejemplo, qué le rechaza a un cliente concreto (Damus, otro relé...) sin guardar
// nada sensible. Una vez por minuto, y solo si hubo actividad, imprime un resumen.
type activityLog struct {
	mu         sync.Mutex
	out        io.Writer
	now        func() time.Time
	windowFrom time.Time

	saved, ephemeral int
	rejected         map[string]int // motivo (prefijo) -> total en la ventana
	printed          map[string]int // motivo -> líneas ya impresas en la ventana
	authed           int

	// Para el panel de control: histórico por minuto (últimas 2 h), rechazos recientes y
	// totales por motivo desde que arrancó el relé. Solo en memoria, sin contenido ni IPs.
	minutes      map[int64]*admin.Minute
	recent       []admin.Rejection
	reasonTotals map[string]int
}

const (
	historyMinutes = 120
	recentKept     = 100
)

func newActivityLog(out io.Writer, now func() time.Time) *activityLog {
	return &activityLog{out: out, now: now, windowFrom: now(), rejected: map[string]int{}, printed: map[string]int{},
		minutes: map[int64]*admin.Minute{}, reasonTotals: map[string]int{}}
}

// bucketLocked devuelve el contador del minuto actual (y descarta los muy antiguos).
func (a *activityLog) bucketLocked() *admin.Minute {
	start := a.now().Unix() / 60 * 60
	b := a.minutes[start]
	if b == nil {
		b = &admin.Minute{T: start}
		a.minutes[start] = b
		for t := range a.minutes {
			if t < start-historyMinutes*60 {
				delete(a.minutes, t)
			}
		}
	}
	return b
}

// reasonKey reduce un motivo a su prefijo ("rate-limited", "blocked", "pow"...) para
// agrupar; lo que no tenga prefijo conocido va a "other".
func reasonKey(reason string) string {
	if i := strings.Index(reason, ":"); i > 0 && i < 24 {
		return reason[:i]
	}
	return "other"
}

func shortKey(pubkey string) string {
	if len(pubkey) < 8 {
		return pubkey
	}
	return pubkey[:8]
}

// Rejected anota un rechazo; imprime la línea salvo que ese motivo ya haya llegado al tope.
func (a *activityLog) Rejected(what string, kind int, pubkey, reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollLocked()
	key := reasonKey(reason)
	a.rejected[key]++
	a.reasonTotals[key]++
	a.bucketLocked().Rejected++
	a.recent = append(a.recent, admin.Rejection{T: a.now().Unix(), What: what, Kind: kind, Pubkey: shortKey(pubkey), Reason: reason})
	if len(a.recent) > recentKept {
		a.recent = a.recent[len(a.recent)-recentKept:]
	}
	if a.printed[key] >= maxRejectLinesPerReasonPerMinute {
		return
	}
	a.printed[key]++
	fmt.Fprintf(a.out, "reject %s kind=%d pubkey=%s reason=%q\n", what, kind, shortKey(pubkey), reason)
}

func (a *activityLog) Saved() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollLocked()
	a.saved++
	a.bucketLocked().Saved++
}

func (a *activityLog) Ephemeral() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollLocked()
	a.ephemeral++
	a.bucketLocked().Ephemeral++
}

func (a *activityLog) Authenticated() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollLocked()
	a.authed++
	a.bucketLocked().Authenticated++
}

// rollLocked cierra la ventana de un minuto imprimiendo el resumen si hubo actividad.
// Se evalúa al registrar algo (no hay temporizador): si no pasa nada, no se imprime nada.
func (a *activityLog) rollLocked() {
	now := a.now()
	if now.Sub(a.windowFrom) < time.Minute {
		return
	}
	total := 0
	for _, n := range a.rejected {
		total += n
	}
	if a.saved+a.ephemeral+a.authed+total > 0 {
		parts := make([]string, 0, len(a.rejected))
		for k, n := range a.rejected {
			parts = append(parts, fmt.Sprintf("%s=%d", k, n))
		}
		sort.Strings(parts)
		fmt.Fprintf(a.out, "stats last=%s saved=%d ephemeral=%d authenticated=%d rejected=%d [%s]\n",
			now.Sub(a.windowFrom).Round(time.Second), a.saved, a.ephemeral, a.authed, total, strings.Join(parts, " "))
	}
	a.windowFrom = now
	a.saved, a.ephemeral, a.authed = 0, 0, 0
	a.rejected = map[string]int{}
	a.printed = map[string]int{}
}

// --- lo que consulta el panel de control (implementa admin.Activity) ---

// Minutes devuelve los últimos n minutos (el actual incluido), con ceros en los sin actividad.
func (a *activityLog) Minutes(now time.Time, n int) []admin.Minute {
	a.mu.Lock()
	defer a.mu.Unlock()
	end := now.Unix() / 60 * 60
	out := make([]admin.Minute, 0, n)
	for i := n - 1; i >= 0; i-- {
		t := end - int64(i)*60
		if b, ok := a.minutes[t]; ok {
			out = append(out, *b)
		} else {
			out = append(out, admin.Minute{T: t})
		}
	}
	return out
}

// Rejections devuelve los últimos rechazos, el más reciente primero.
func (a *activityLog) Rejections(limit int) []admin.Rejection {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]admin.Rejection, 0, limit)
	for i := len(a.recent) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, a.recent[i])
	}
	return out
}

// ReasonTotals devuelve los rechazos por motivo desde que arrancó el relé.
func (a *activityLog) ReasonTotals() map[string]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]int, len(a.reasonTotals))
	for k, v := range a.reasonTotals {
		out[k] = v
	}
	return out
}

// Admin deja constancia de una acción de moderación hecha desde el panel de control: qué se hizo y,
// como mucho, los 8 primeros caracteres del pubkey o del id afectado (nunca contenido ni IPs).
func (a *activityLog) Admin(action, target string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if target == "" {
		fmt.Fprintf(a.out, "admin action=%s\n", action)
		return
	}
	fmt.Fprintf(a.out, "admin action=%s target=%s\n", action, target)
}

// Retention deja constancia de una pasada de retención, solo si borró algo o falló.
func (a *activityLog) Retention(res retention.Result, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		fmt.Fprintf(a.out, "retention error=%q deleted=%d\n", err.Error(), res.Deleted)
		return
	}
	if res.Deleted > 0 {
		fmt.Fprintf(a.out, "retention deleted=%d scanned=%d\n", res.Deleted, res.Scanned)
	}
}
