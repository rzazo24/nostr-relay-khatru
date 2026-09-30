package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/nbd-wtf/go-nostr/nip19"

	"github.com/rzazo24/nostr-relay-khatru/internal/moderation"
)

// Claves de los ajustes que se pueden cambiar en caliente (las lee también NIP-11).
const (
	SettingName        = "name"
	SettingDescription = "description"
	SettingIcon        = "icon"
)

// Effects son las acciones que tocan los eventos guardados (las implementa el servidor).
type Effects interface {
	// DeleteEventByID borra el evento si está guardado y dice si lo estaba.
	DeleteEventByID(ctx context.Context, id string) (bool, error)
	// DeleteEventsByAuthor borra como mucho `max` eventos de un pubkey y devuelve cuántos.
	DeleteEventsByAuthor(ctx context.Context, pubkey string, max int) (int, error)
}

// InfoView es lo que NIP-11 muestra del relé (nombre, descripción, icono).
type InfoView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Límite de eventos que se borran de golpe al banear a un pubkey con "borrar sus eventos".
const maxDeleteByAuthor = 5000

func (p *Panel) mountModeration(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/api/moderation", p.requireSession(p.moderationLists))
	for path, h := range map[string]func(map[string]any, *http.Request) (any, error){
		"ban-pubkey":     p.banPubKey,
		"unban-pubkey":   p.unbanPubKey,
		"allow-pubkey":   p.allowPubKey,
		"unallow-pubkey": p.unallowPubKey,
		"ban-event":      p.banEvent,
		"unban-event":    p.unbanEvent,
		"kind":           p.kindRule,
		"ip":             p.ipRule,
		"info":           p.setInfo,
	} {
		mux.HandleFunc("POST /admin/api/mod/"+path, p.mutation(path, h))
	}
}

// mutation envuelve una acción que cambia algo: exige sesión, que la petición venga de la propia
// página (Origin / Sec-Fetch-Site) y JSON. Además de la cookie SameSite=Strict, que ya impide que
// otro sitio use la sesión, esto evita que un fallo en otra parte lo permita.
func (p *Panel) mutation(name string, h func(map[string]any, *http.Request) (any, error)) http.HandlerFunc {
	return p.requireSession(func(w http.ResponseWriter, r *http.Request) {
		if p.o.Store == nil {
			fail(w, http.StatusServiceUnavailable, "moderation is not available")
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			fail(w, http.StatusForbidden, "cross-site request refused")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && normalizeURL(origin) != normalizeURL(p.originOf(r)) {
			fail(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			fail(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}
		var body map[string]any
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
			fail(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		out, err := h(body, r)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		if out == nil {
			out = map[string]any{}
		}
		if m, ok := out.(map[string]any); ok {
			m["ok"] = true
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func (p *Panel) originOf(r *http.Request) string {
	e := p.expectedURL(r)
	if i := strings.Index(e, "://"); i >= 0 {
		if j := strings.Index(e[i+3:], "/"); j >= 0 {
			return e[:i+3+j]
		}
	}
	return e
}

// ---------- lectura ----------

type entryOut struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

func entries(es []moderation.Entry) []entryOut {
	out := make([]entryOut, len(es))
	for i, e := range es {
		out[i] = entryOut{e.Key, e.Reason}
	}
	return out
}

func (p *Panel) moderationLists(w http.ResponseWriter, r *http.Request) {
	if p.o.Store == nil {
		fail(w, http.StatusServiceUnavailable, "moderation is not available")
		return
	}
	st := p.o.Store
	overrides := map[string]bool{}
	for _, k := range []string{SettingName, SettingDescription, SettingIcon} {
		_, overrides[k] = st.Setting(k)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bannedPubkeys":   entries(st.BannedPubKeys()),
		"allowedPubkeys":  entries(st.AllowedPubKeys()),
		"bannedEvents":    entries(st.BannedEvents()),
		"blockedIPs":      entries(st.BlockedIPs()),
		"allowedKinds":    nonNil(st.AllowedKinds()),
		"disallowedKinds": nonNil(st.DisallowedKinds()),
		"info":            p.o.Info(),
		"infoDefaults":    p.o.InfoDefaults,
		"infoOverrides":   overrides,
		"owner":           p.o.Owner,
	})
}

// ---------- entradas ----------

func str(body map[string]any, key string, max int) (string, error) {
	v, ok := body[key]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%q must be a string", key)
	}
	s = strings.TrimSpace(s)
	if len([]rune(s)) > max {
		return "", fmt.Errorf("%q is too long (maximum %d characters)", key, max)
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' {
			return "", fmt.Errorf("%q contains control characters", key)
		}
	}
	return s, nil
}

// decodePubkey admite hex (64) o npub / nprofile.
func decodePubkey(s string) (string, error) {
	s = strings.TrimSpace(s)
	if hex64.MatchString(strings.ToLower(s)) {
		return strings.ToLower(s), nil
	}
	if prefix, v, err := nip19.Decode(s); err == nil {
		switch prefix {
		case "npub":
			if h, ok := v.(string); ok {
				return h, nil
			}
		case "nprofile":
			if pp, ok := v.(interface{ GetPublicKey() string }); ok {
				return pp.GetPublicKey(), nil
			}
		}
	}
	return "", fmt.Errorf("not a valid public key (use 64-character hex or an npub)")
}

func decodeEventID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if hex64.MatchString(strings.ToLower(s)) {
		return strings.ToLower(s), nil
	}
	if prefix, v, err := nip19.Decode(s); err == nil {
		switch prefix {
		case "note":
			if h, ok := v.(string); ok {
				return h, nil
			}
		case "nevent":
			if ep, ok := v.(interface{ GetID() string }); ok { // por si la versión lo expone como método
				return ep.GetID(), nil
			}
			if b, err := json.Marshal(v); err == nil {
				var probe struct{ ID string }
				if json.Unmarshal(b, &probe) == nil && hex64.MatchString(probe.ID) {
					return probe.ID, nil
				}
			}
		}
	}
	return "", fmt.Errorf("not a valid event id (use 64-character hex, note1… or nevent1…)")
}

func (p *Panel) log(action, target string) {
	if p.o.Log != nil {
		p.o.Log(action, target)
	}
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// ---------- acciones ----------

func (p *Panel) banPubKey(body map[string]any, r *http.Request) (any, error) {
	raw, _ := str(body, "pubkey", 200)
	pk, err := decodePubkey(raw)
	if err != nil {
		return nil, err
	}
	if pk == p.o.Owner {
		return nil, fmt.Errorf("you can't ban the relay owner")
	}
	reason, err := str(body, "reason", 200)
	if err != nil {
		return nil, err
	}
	if err := p.o.Store.BanPubKey(pk, reason); err != nil {
		return nil, fmt.Errorf("could not save the ban")
	}
	deleted := 0
	if b, _ := body["deleteEvents"].(bool); b && p.o.Effects != nil {
		if deleted, err = p.o.Effects.DeleteEventsByAuthor(r.Context(), pk, maxDeleteByAuthor); err != nil {
			return nil, fmt.Errorf("banned, but deleting their events failed: %v", err)
		}
	}
	p.log("ban-pubkey", short(pk))
	return map[string]any{"deleted": deleted}, nil
}

func (p *Panel) unbanPubKey(body map[string]any, r *http.Request) (any, error) {
	raw, _ := str(body, "pubkey", 200)
	pk, err := decodePubkey(raw)
	if err != nil {
		return nil, err
	}
	if err := p.o.Store.UnbanPubKey(pk); err != nil {
		return nil, fmt.Errorf("could not save the change")
	}
	p.log("unban-pubkey", short(pk))
	return nil, nil
}

func (p *Panel) allowPubKey(body map[string]any, r *http.Request) (any, error) {
	raw, _ := str(body, "pubkey", 200)
	pk, err := decodePubkey(raw)
	if err != nil {
		return nil, err
	}
	reason, err := str(body, "reason", 200)
	if err != nil {
		return nil, err
	}
	if err := p.o.Store.AllowPubKey(pk, reason); err != nil {
		return nil, fmt.Errorf("could not save the change")
	}
	p.log("allow-pubkey", short(pk))
	return nil, nil
}

func (p *Panel) unallowPubKey(body map[string]any, r *http.Request) (any, error) {
	raw, _ := str(body, "pubkey", 200)
	pk, err := decodePubkey(raw)
	if err != nil {
		return nil, err
	}
	if err := p.o.Store.RemoveAllowedPubKey(pk); err != nil {
		return nil, fmt.Errorf("could not save the change")
	}
	p.log("unallow-pubkey", short(pk))
	return nil, nil
}

func (p *Panel) banEvent(body map[string]any, r *http.Request) (any, error) {
	raw, _ := str(body, "id", 200)
	id, err := decodeEventID(raw)
	if err != nil {
		return nil, err
	}
	reason, err := str(body, "reason", 200)
	if err != nil {
		return nil, err
	}
	if err := p.o.Store.BanEvent(id, reason); err != nil {
		return nil, fmt.Errorf("could not save the ban")
	}
	deleted := false
	if p.o.Effects != nil {
		if deleted, err = p.o.Effects.DeleteEventByID(r.Context(), id); err != nil {
			return nil, fmt.Errorf("banned, but deleting the stored event failed: %v", err)
		}
	}
	p.log("ban-event", short(id))
	return map[string]any{"deleted": deleted}, nil
}

func (p *Panel) unbanEvent(body map[string]any, r *http.Request) (any, error) {
	raw, _ := str(body, "id", 200)
	id, err := decodeEventID(raw)
	if err != nil {
		return nil, err
	}
	if err := p.o.Store.UnbanEvent(id); err != nil {
		return nil, fmt.Errorf("could not save the change")
	}
	p.log("unban-event", short(id))
	return nil, nil
}

func (p *Panel) kindRule(body map[string]any, r *http.Request) (any, error) {
	kf, ok := body["kind"].(float64)
	if !ok || kf != float64(int(kf)) || kf < 0 || kf > 65535 {
		return nil, fmt.Errorf("kind must be an integer between 0 and 65535")
	}
	kind := int(kf)
	rule, _ := body["rule"].(string)
	var err error
	switch rule {
	case "allow":
		err = p.o.Store.AllowKind(kind)
	case "disallow":
		err = p.o.Store.DisallowKind(kind)
	case "clear":
		err = p.o.Store.ClearKindRule(kind)
	default:
		return nil, fmt.Errorf(`rule must be "allow", "disallow" or "clear"`)
	}
	if err != nil {
		return nil, fmt.Errorf("could not save the change")
	}
	p.log("kind-"+rule, strconv.Itoa(kind))
	return nil, nil
}

func (p *Panel) ipRule(body map[string]any, r *http.Request) (any, error) {
	raw, _ := str(body, "ip", 60)
	ip := net.ParseIP(raw)
	if ip == nil {
		return nil, fmt.Errorf("not a valid IP address")
	}
	action, _ := body["action"].(string)
	var err error
	switch action {
	case "block":
		reason, rerr := str(body, "reason", 200)
		if rerr != nil {
			return nil, rerr
		}
		err = p.o.Store.BlockIP(ip.String(), reason)
	case "unblock":
		err = p.o.Store.UnblockIP(ip.String())
	default:
		return nil, fmt.Errorf(`action must be "block" or "unblock"`)
	}
	if err != nil {
		return nil, fmt.Errorf("could not save the change")
	}
	p.log("ip-"+action, "")
	return nil, nil
}

// setInfo cambia el nombre, la descripción o el icono (los que vengan en la petición) y
// restaura los que se listen en "reset" a su valor de la configuración.
func (p *Panel) setInfo(body map[string]any, r *http.Request) (any, error) {
	st := p.o.Store
	set := func(key, field string, max int, allowEmpty bool, validate func(string) error) error {
		if _, present := body[field]; !present {
			return nil
		}
		v, err := str(body, field, max)
		if err != nil {
			return err
		}
		if v == "" && !allowEmpty {
			return fmt.Errorf("%q can't be empty (use reset to restore the default)", field)
		}
		if validate != nil && v != "" {
			if err := validate(v); err != nil {
				return err
			}
		}
		return st.SetSetting(key, v)
	}
	if err := set(SettingName, "name", 80, false, nil); err != nil {
		return nil, err
	}
	if err := set(SettingDescription, "description", 600, false, nil); err != nil {
		return nil, err
	}
	if err := set(SettingIcon, "icon", 300, true, func(v string) error { // vacío = sin icono
		if strings.HasPrefix(v, "https://") || (strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//")) {
			return nil
		}
		return fmt.Errorf("the icon must be an https:// URL or a path starting with /")
	}); err != nil {
		return nil, err
	}
	if list, ok := body["reset"].([]any); ok {
		keys := map[string]string{"name": SettingName, "description": SettingDescription, "icon": SettingIcon}
		for _, f := range list {
			if name, _ := f.(string); keys[name] != "" {
				if err := st.DeleteSetting(keys[name]); err != nil {
					return nil, fmt.Errorf("could not save the change")
				}
			}
		}
	}
	p.log("info", "")
	return map[string]any{"info": p.o.Info()}, nil
}

// nonNil evita que una lista vacía se serialice como null (el panel espera siempre un array).
func nonNil(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}
