package moderation

import (
	"path/filepath"
	"reflect"
	"testing"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mod.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestPubKeysBanAndAllowAreExclusive(t *testing.T) {
	s, _ := open(t)
	if s.HasAllowlist() {
		t.Fatal("sin nadie permitido no hay lista blanca")
	}
	s.AllowPubKey("aaa", "amigo")
	if !s.HasAllowlist() || !s.IsPubKeyAllowed("aaa") {
		t.Fatal("aaa debería estar permitido")
	}
	s.BanPubKey("aaa", "spam")
	if s.IsPubKeyAllowed("aaa") || !s.IsPubKeyBanned("aaa") || s.HasAllowlist() {
		t.Fatal("banear lo saca de los permitidos")
	}
	s.AllowPubKey("aaa", "perdonado")
	if s.IsPubKeyBanned("aaa") || !s.IsPubKeyAllowed("aaa") {
		t.Fatal("permitir lo saca de los baneados")
	}
	if got := s.AllowedPubKeys(); len(got) != 1 || got[0].Reason != "perdonado" {
		t.Fatalf("lista de permitidos: %+v", got)
	}
}

func TestKindRules(t *testing.T) {
	s, _ := open(t)
	if s.KindBlocked(1) {
		t.Fatal("sin reglas no se bloquea nada")
	}
	s.DisallowKind(1984)
	if !s.KindBlocked(1984) || s.KindBlocked(1) {
		t.Fatal("solo el kind prohibido")
	}
	s.AllowKind(1)
	if s.KindBlocked(1) || !s.KindBlocked(7) {
		t.Fatal("con lista de permitidos solo pasan esos")
	}
	s.AllowKind(1984)
	if s.KindBlocked(1984) {
		t.Fatal("permitir un kind lo saca de los prohibidos")
	}
	if !reflect.DeepEqual(s.AllowedKinds(), []int{1, 1984}) {
		t.Fatalf("permitidos: %v", s.AllowedKinds())
	}
}

func TestEventsIPsAndPersistence(t *testing.T) {
	s, path := open(t)
	s.BanEvent("e1", "ilegal")
	s.BlockIP("1.2.3.4", "abuso")
	s.BanPubKey("pk", "spam")
	s.DisallowKind(9)
	s.SetSetting("name", "Mi relé")
	s.UnblockIP("nunca-estuvo") // no falla

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if !s2.IsEventBanned("e1") || !s2.IsIPBlocked("1.2.3.4") || !s2.IsPubKeyBanned("pk") || !s2.KindBlocked(9) {
		t.Fatal("las listas deberían sobrevivir a reabrir")
	}
	if v, ok := s2.Setting("name"); !ok || v != "Mi relé" {
		t.Fatalf("ajuste perdido: %q %v", v, ok)
	}
	s2.UnbanEvent("e1")
	if s2.IsEventBanned("e1") || len(s2.BannedEvents()) != 0 {
		t.Fatal("desbanear un evento")
	}
	if got := s2.BlockedIPs(); len(got) != 1 || got[0].Reason != "abuso" {
		t.Fatalf("ips: %+v", got)
	}
}

func TestRemovalsDoNotSideEffectOtherLists(t *testing.T) {
	s, _ := open(t)
	s.BanPubKey("aaa", "spam")
	s.UnbanPubKey("aaa")
	if s.IsPubKeyBanned("aaa") || s.HasAllowlist() {
		t.Fatal("quitar un baneo no debe meterlo en la lista blanca")
	}
	s.AllowPubKey("bbb", "")
	s.RemoveAllowedPubKey("bbb")
	if s.HasAllowlist() || s.IsPubKeyBanned("bbb") {
		t.Fatal("quitar de la lista blanca la deja vacía sin banear")
	}
	s.DisallowKind(7)
	s.AllowKind(1)
	s.ClearKindRule(7)
	s.ClearKindRule(1)
	if s.KindBlocked(7) || s.KindBlocked(1) || len(s.AllowedKinds()) != 0 || len(s.DisallowedKinds()) != 0 {
		t.Fatal("ClearKindRule quita la regla, sea cual sea")
	}
	s.SetSetting("name", "X")
	s.DeleteSetting("name")
	if _, ok := s.Setting("name"); ok {
		t.Fatal("el ajuste debería haberse borrado")
	}
}
