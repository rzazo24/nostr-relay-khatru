package admin

import (
	"encoding/json"
	"net/http"
	"time"
)

// GET /stats.json: cifras generales y PÚBLICAS para la página de inicio (y para quien quiera mirarlas). No pide sesión, así que
// SOLO lleva agregados: ni claves, ni IPs, ni contenido, ni motivos de rechazo, y los tipos de mensajes privados no salen en el
// desglose. Se cachea 30 s (en el servidor y en el navegador/Caddy) para que no suponga carga.
const publicStatsTTL = 30 * time.Second

type publicHour struct {
	T         int64 `json:"t"`
	Saved     int64 `json:"saved"`
	Ephemeral int64 `json:"ephemeral"`
}

type publicStatsDoc struct {
	Now         int64 `json:"now"`
	StartedAt   int64 `json:"startedAt"`
	Connections int64 `json:"connections"`
	Events      struct {
		Total   int         `json:"total"`
		Authors int         `json:"authors"`
		Oldest  int64       `json:"oldest"`
		Last24h int         `json:"last24h"`
		ByKind  []kindCount `json:"byKind"`
	} `json:"events"`
	Last24h struct {
		Saved     int64        `json:"saved"`
		Ephemeral int64        `json:"ephemeral"`
		Rejected  int64        `json:"rejected"`
		Hours     []publicHour `json:"hours"`
	} `json:"last24h"`
}

func (p *Panel) publicStats(w http.ResponseWriter, r *http.Request) {
	now := p.o.Now()
	p.pubMu.Lock()
	defer p.pubMu.Unlock()
	if p.pubBody == nil || now.Sub(p.pubAt) >= publicStatsTTL {
		doc, err := p.buildPublicStats(r, now)
		if err != nil {
			http.Error(w, `{"error":"could not read the statistics"}`, http.StatusInternalServerError)
			return
		}
		b, _ := json.Marshal(doc)
		p.pubBody, p.pubAt = b, now
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "public, max-age=30")
	h.Set("Access-Control-Allow-Origin", "*")
	w.Write(p.pubBody)
}

func (p *Panel) buildPublicStats(r *http.Request, now time.Time) (*publicStatsDoc, error) {
	ev, err := p.eventStats(r.Context(), now)
	if err != nil {
		return nil, err
	}
	doc := &publicStatsDoc{Now: now.Unix(), StartedAt: p.o.StartedAt.Unix()}
	if p.o.Connections != nil {
		doc.Connections = p.o.Connections()
	}
	doc.Events.Total, doc.Events.Authors, doc.Events.Oldest, doc.Events.Last24h = ev.Total, ev.PubKeys, ev.Oldest, ev.Last24h
	doc.Events.ByKind = []kindCount{}
	for _, k := range ev.ByKind {
		if privateKinds[k.Kind] || len(doc.Events.ByKind) >= 8 {
			continue // los mensajes privados no salen ni como recuento
		}
		doc.Events.ByKind = append(doc.Events.ByKind, k)
	}
	doc.Last24h.Hours = []publicHour{}
	if p.o.Stats != nil {
		if res, err := p.o.Stats.History("24h", now); err == nil {
			doc.Last24h.Saved, doc.Last24h.Ephemeral, doc.Last24h.Rejected = res.Totals.Saved, res.Totals.Ephemeral, res.Totals.Rejected
			for _, b := range res.Buckets {
				doc.Last24h.Hours = append(doc.Last24h.Hours, publicHour{T: b.T, Saved: b.Saved, Ephemeral: b.Ephemeral})
			}
		}
	}
	if len(doc.Last24h.Hours) == 0 { // sin histórico persistido: 24 horas a cero, para que el cliente no tenga que adivinar
		end := now.Unix() / 3600 * 3600
		for i := int64(23); i >= 0; i-- {
			doc.Last24h.Hours = append(doc.Last24h.Hours, publicHour{T: end - i*3600})
		}
	}
	return doc, nil
}
