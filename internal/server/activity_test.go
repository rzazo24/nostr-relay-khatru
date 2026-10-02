package server

import (
	"bytes"
	"fmt"
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

func TestActivityLog_NoisyKeys(t *testing.T) {
	var buf bytes.Buffer
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	a := newActivityLog(&buf, func() time.Time { return clock })
	loud, quiet := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for i := 0; i < 5; i++ {
		a.Rejected("event", 20001, loud, "rate-limited: slow down, please")
	}
	a.Rejected("event", 1, loud, "blocked: kind 1 is not accepted by this relay")
	a.Rejected("event", 1, quiet, "invalid: bad id")
	a.Rejected("filter", 1059, loud, "auth-required: x") // las consultas no cuentan
	a.Rejected("event", 1, "corta", "invalid: x")        // sin clave válida no se cuenta
	top := a.Noisy(10)
	if len(top) != 2 || top[0].Pubkey != loud || top[0].Count != 6 || top[0].Reason != "rate-limited" || top[0].Kind != 1 || top[1].Pubkey != quiet {
		t.Fatalf("ranking: %+v", top)
	}
	if got := a.Noisy(1); len(got) != 1 || got[0].Pubkey != loud {
		t.Fatalf("limit: %+v", got)
	}
	if strings.Contains(buf.String(), loud) {
		t.Fatal("el log no debe imprimir la clave completa")
	}
	clock = clock.Add(25 * time.Hour)
	if len(a.Noisy(10)) != 0 {
		t.Fatal("a las 24 h una clave sin actividad deja de aparecer")
	}
}

func TestActivityLog_NoisyKeysAreBounded(t *testing.T) {
	var buf bytes.Buffer
	a := newActivityLog(&buf, time.Now)
	for i := 0; i < noisyKept*3; i++ {
		pk := fmt.Sprintf("%064x", i)
		for j := 0; j <= i%5; j++ {
			a.Rejected("event", 1, pk, "invalid: x")
		}
	}
	if len(a.noisy) > noisyKept {
		t.Fatalf("se recuerdan como mucho %d claves, hay %d", noisyKept, len(a.noisy))
	}
}
