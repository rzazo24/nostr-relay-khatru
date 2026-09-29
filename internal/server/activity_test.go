package server

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestActivityLog_RejectionLinesAreCappedAndSummarised(t *testing.T) {
	var buf bytes.Buffer
	clock := time.Unix(1_800_000_000, 0)
	a := newActivityLog(&buf, func() time.Time { return clock })

	for i := 0; i < 20; i++ {
		a.Rejected("event", 1, "abcdef0123456789", "rate-limited: slow down, please")
	}
	a.Rejected("event", 7, "abcdef0123456789", "blocked: kind 7 is not accepted")
	a.Saved()
	a.Saved()

	if n := strings.Count(buf.String(), "reject event"); n != maxRejectLinesPerReasonPerMinute+1 {
		t.Fatalf("se esperaban %d líneas (tope por motivo + 1 de otro motivo), hubo %d:\n%s", maxRejectLinesPerReasonPerMinute+1, n, buf.String())
	}
	if !strings.Contains(buf.String(), "pubkey=abcdef01 ") || strings.Contains(buf.String(), "abcdef0123456789") {
		t.Fatal("solo se registra un trozo corto del pubkey")
	}

	clock = clock.Add(61 * time.Second)
	a.Saved() // al cambiar de ventana se imprime el resumen de la anterior
	out := buf.String()
	if !strings.Contains(out, "stats last=1m1s saved=2") || !strings.Contains(out, "rejected=21") || !strings.Contains(out, "rate-limited=20") || !strings.Contains(out, "blocked=1") {
		t.Fatalf("resumen inesperado:\n%s", out)
	}
}

func TestActivityLog_SilentWhenNothingHappens(t *testing.T) {
	var buf bytes.Buffer
	clock := time.Unix(1_800_000_000, 0)
	a := newActivityLog(&buf, func() time.Time { return clock })
	clock = clock.Add(5 * time.Minute)
	a.rollLocked()
	if buf.Len() != 0 {
		t.Fatalf("sin actividad no se imprime nada: %q", buf.String())
	}
}

func TestReasonKey(t *testing.T) {
	for in, want := range map[string]string{
		"rate-limited: slow down": "rate-limited",
		"auth-required: x":        "auth-required",
		"sin prefijo":             "other",
		"":                        "other",
	} {
		if got := reasonKey(in); got != want {
			t.Errorf("%q -> %q, se esperaba %q", in, got, want)
		}
	}
}
