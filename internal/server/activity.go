package server

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
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
}

func newActivityLog(out io.Writer, now func() time.Time) *activityLog {
	return &activityLog{out: out, now: now, windowFrom: now(), rejected: map[string]int{}, printed: map[string]int{}}
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
}

func (a *activityLog) Ephemeral() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollLocked()
	a.ephemeral++
}

func (a *activityLog) Authenticated() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollLocked()
	a.authed++
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
