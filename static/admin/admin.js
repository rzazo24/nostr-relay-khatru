// Panel de control del relé (solo lectura). Habla con /admin/api/* del propio relé.
// Entrada: firma NIP-98 con la extensión NIP-07 (nos2x...), que se cambia por una sesión
// corta en una cookie HttpOnly. TODO lo que llega del servidor (contenido de notas, motivos,
// nombres) se inserta con textContent, nunca como HTML.
'use strict'

const $ = (id) => document.getElementById(id)
const REFRESH_MS = 15000
let timer = null
let lastOk = 0

const KIND_NAMES = {
  0: 'Perfil', 1: 'Nota', 3: 'Contactos', 4: 'Mensaje directo (antiguo)', 5: 'Borrado', 6: 'Repost', 7: 'Reacción',
  13: 'Seal (NIP-59)', 14: 'Mensaje directo (NIP-17)', 1059: 'Gift wrap (mensaje privado)', 1984: 'Denuncia',
  9735: 'Zap', 10000: 'Lista de silenciados', 10002: 'Lista de relés', 10050: 'Relés de mensajes directos',
  20001: 'Efímero 20001', 22242: 'Autenticación', 27235: 'Auth HTTP', 30023: 'Artículo', 30078: 'Datos de app',
}
const kindName = (k) => KIND_NAMES[k] ? `${k} · ${KIND_NAMES[k]}` : String(k)

function el(tag, attrs, ...children) {
  const n = document.createElement(tag)
  for (const [k, v] of Object.entries(attrs || {})) {
    if (k === 'class') n.className = v
    else if (k === 'text') n.textContent = v
    else if (k === 'width') n.style.width = v // por CSSOM: la política de contenido no admite el atributo style
    else n.setAttribute(k, v)
  }
  for (const c of children) if (c != null) n.append(c)
  return n
}
const fmt = (n) => Number(n).toLocaleString('es-ES')
const two = (n) => String(n).padStart(2, '0')
const clock = (unix) => { const d = new Date(unix * 1000); return `${two(d.getHours())}:${two(d.getMinutes())}:${two(d.getSeconds())}` }

function ago(unix, now) {
  const s = Math.max(0, Math.round(now - unix))
  if (s < 60) return `hace ${s} s`
  if (s < 3600) return `hace ${Math.floor(s / 60)} min`
  if (s < 86400) return `hace ${Math.floor(s / 3600)} h`
  return `hace ${Math.floor(s / 86400)} d`
}
function bytes(n) {
  if (n < 1024) return `${n} B`
  if (n < 1048576) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1048576).toFixed(1)} MB`
}
function duration(s) {
  if (s < 3600) return `${Math.floor(s / 60)} min`
  if (s < 86400) return `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} min`
  return `${Math.floor(s / 86400)} d ${Math.floor((s % 86400) / 3600)} h`
}

// ---------- sesión ----------

function show(view) {
  $('login').hidden = view !== 'login'
  $('dash').hidden = view !== 'dash'
  $('logout').hidden = view !== 'dash'
  $('refresh').hidden = view !== 'dash'
}

async function login() {
  const msg = $('login-msg')
  msg.className = 'msg'
  if (!window.nostr || typeof window.nostr.signEvent !== 'function') {
    msg.className = 'msg err'
    msg.textContent = 'No se detecta ninguna extensión de Nostr. Instala nos2x (o similar) y recarga la página.'
    return
  }
  msg.textContent = 'Firma la petición en la extensión…'
  try {
    const url = `${location.origin}/admin/api/login`
    const signed = await window.nostr.signEvent({ kind: 27235, created_at: Math.floor(Date.now() / 1000), tags: [['u', url], ['method', 'POST']], content: '' })
    const res = await fetch('/admin/api/login', { method: 'POST', headers: { Authorization: 'Nostr ' + btoa(JSON.stringify(signed)) }, credentials: 'same-origin' })
    const body = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(body.error || `error ${res.status}`)
    msg.textContent = ''
    start()
  } catch (err) {
    msg.className = 'msg err'
    msg.textContent = `No se pudo entrar: ${err.message || err}`
  }
}

async function logout() {
  stop()
  await fetch('/admin/api/logout', { method: 'POST', credentials: 'same-origin' }).catch(() => {})
  show('login')
}

// ---------- datos ----------

async function load() {
  const res = await fetch('/admin/api/stats', { credentials: 'same-origin', cache: 'no-store' })
  if (res.status === 401) { stop(); show('login'); return }
  if (!res.ok) throw new Error(`error ${res.status}`)
  render(await res.json())
  lastOk = Date.now()
  $('updated').textContent = `Actualizado a las ${clock(Math.floor(lastOk / 1000))}`
}

function start() {
  show('dash')
  load().catch((e) => { $('updated').textContent = `Error al actualizar: ${e.message}` })
  stop()
  timer = setInterval(() => { if (!document.hidden) load().catch((e) => { $('updated').textContent = `Error al actualizar: ${e.message}` }) }, REFRESH_MS)
}
function stop() { if (timer) { clearInterval(timer); timer = null } }

// ---------- pintado ----------

function render(d) {
  const now = d.now
  $('subtitle').textContent = `Solo lectura · versión ${d.version} · en marcha desde hace ${duration(now - d.startedAt)}`

  const rejectedTotal = Object.values(d.activity.reasons).reduce((a, b) => a + b, 0)
  const cards = [
    ['Eventos guardados', fmt(d.events.total)], ['Claves distintas', fmt(d.events.pubkeys)], ['Últimas 24 h', fmt(d.events.last24h)],
    ['Base de datos', bytes(d.dbBytes)], ['Conexiones abiertas', fmt(d.connections)], ['Rechazos desde el arranque', fmt(rejectedTotal)],
  ]
  $('cards').replaceChildren(...cards.map(([l, v]) => el('div', { class: 'card' }, el('div', { class: 'v', text: v }), el('div', { class: 'l', text: l }))))

  renderChart(d.activity.minutes)

  const max = Math.max(1, ...d.events.byKind.map((k) => k.count))
  $('kinds').replaceChildren(...d.events.byKind.map((k) =>
    el('div', { class: 'row' }, el('span', { text: kindName(k.kind) }), el('div', { class: 'bar' }, el('i', { width: `${Math.round((k.count / max) * 100)}%` })), el('span', { text: fmt(k.count) }))))
  if (!d.events.byKind.length) $('kinds').textContent = 'Todavía no hay eventos.'

  $('reasons').replaceChildren(...Object.entries(d.activity.reasons).sort((a, b) => b[1] - a[1]).map(([r, n]) => el('span', { class: 'chip' }, el('b', { text: r }), ` ${fmt(n)}`)))
  if (!Object.keys(d.activity.reasons).length) $('reasons').textContent = 'Ningún rechazo desde el arranque.'
  const tbody = $('rejections').querySelector('tbody')
  tbody.replaceChildren(...d.activity.rejections.map((r) => el('tr', {},
    el('td', { text: clock(r.t) }), el('td', { text: r.what === 'event' ? 'evento' : 'consulta' }),
    el('td', { text: r.kind >= 0 ? String(r.kind) : '—' }), el('td', { text: r.pubkey || '—' }), el('td', { class: 'reason', text: r.reason }))))

  $('recent').replaceChildren(...d.events.recent.map((e) => {
    const pk = el('button', { class: 'pk', type: 'button', title: 'Copiar clave completa', text: e.pubkey.slice(0, 12) + '…' })
    pk.addEventListener('click', () => navigator.clipboard.writeText(e.pubkey).then(() => { pk.textContent = 'copiada ✓'; setTimeout(() => { pk.textContent = e.pubkey.slice(0, 12) + '…' }, 1200) }).catch(() => {}))
    const meta = el('div', { class: 'meta' }, el('span', { text: ago(e.createdAt, now) }), el('span', { text: kindName(e.kind) }), pk, e.mine ? el('span', { class: 'badge', text: 'tuyo' }) : null)
    return el('li', {}, meta, e.content ? el('div', { class: 'body', text: e.content }) : null)
  }))

  const m = d.moderation
  const list = (a) => (a && a.length ? a.join(', ') : 'ninguno')
  fillKv($('moderation'), [['Claves baneadas', fmt(m.bannedPubkeys)], ['Claves permitidas (lista blanca)', fmt(m.allowedPubkeys)], ['Eventos vetados', fmt(m.bannedEvents)], ['IPs bloqueadas', fmt(m.blockedIPs)], ['Tipos permitidos', list(m.allowedKinds)], ['Tipos prohibidos', list(m.disallowedKinds)]])
  const c = d.config
  fillKv($('config'), [
    ['NIPs', (c.nips || []).join(', ')], ['Contenido máx.', `${fmt(c.maxContentLength)} caracteres`], ['Mensaje máx.', bytes(c.maxMessageBytes)], ['Tags por evento', fmt(c.maxEventTags)],
    ['Eventos por consulta', fmt(c.maxLimit)], ['Sync NIP-77 máx.', fmt(c.maxNegentropyEvents)], ['Fecha futura máx.', `${Math.round(c.maxFutureSkewSec / 60)} min`],
    ['Prueba de trabajo', c.minPoW ? `${c.minPoW} bits` : 'no'], ['Auth obligatoria', c.authRequired ? 'sí' : 'no'], ['Tipos privados', list(c.privateKinds)],
    ['Eventos/min por IP', `${c.eventsPerMinute} (ráfaga ${c.eventsBurst})`], ['Consultas/min por IP', `${c.reqsPerMinute} (ráfaga ${c.reqsBurst})`], ['Conexiones/min por IP', `${c.connsPerMinute} (ráfaga ${c.connsBurst})`],
  ])
}

function fillKv(dl, rows) {
  dl.replaceChildren(...rows.flatMap(([k, v]) => [el('dt', { text: k }), el('dd', { text: String(v) })]))
}

function renderChart(minutes) {
  const NS = 'http://www.w3.org/2000/svg'
  const W = 600, H = 140, pad = 2
  const max = Math.max(3, ...minutes.map((m) => m.saved + m.ephemeral + m.rejected))
  const bw = W / minutes.length
  const svg = document.createElementNS(NS, 'svg')
  svg.setAttribute('viewBox', `0 0 ${W} ${H}`)
  svg.setAttribute('preserveAspectRatio', 'none')
  minutes.forEach((m, i) => {
    let y = H - pad
    for (const [key, color] of [['saved', '#2dd4bf'], ['ephemeral', '#60a5fa'], ['rejected', '#e31337']]) {
      const h = (m[key] / max) * (H - 2 * pad)
      if (h <= 0) continue
      y -= h
      const r = document.createElementNS(NS, 'rect')
      r.setAttribute('x', String(i * bw + 0.5)); r.setAttribute('y', String(y)); r.setAttribute('width', String(Math.max(1, bw - 1))); r.setAttribute('height', String(h)); r.setAttribute('fill', color)
      const t = document.createElementNS(NS, 'title')
      t.textContent = `${clock(m.t).slice(0, 5)} · guardados ${m.saved}, efímeros ${m.ephemeral}, rechazados ${m.rejected}, autenticaciones ${m.authenticated}`
      r.append(t)
      svg.append(r)
    }
  })
  $('chart').replaceChildren(svg)
}

// ---------- arranque ----------

$('login-btn').addEventListener('click', login)
$('logout').addEventListener('click', logout)
$('refresh').addEventListener('click', () => load().catch((e) => { $('updated').textContent = `Error al actualizar: ${e.message}` }))
document.addEventListener('visibilitychange', () => { if (!document.hidden && timer && Date.now() - lastOk > REFRESH_MS) load().catch(() => {}) })

fetch('/admin/api/session', { credentials: 'same-origin', cache: 'no-store' })
  .then((r) => { if (r.ok) start(); else show('login') })
  .catch(() => show('login'))
