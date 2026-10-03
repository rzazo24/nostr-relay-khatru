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
  if (n < 1073741824) return `${(n / 1048576).toFixed(1)} MB`
  return `${(n / 1073741824).toFixed(1)} GB`
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

// El evento que se firma para entrar (NIP-98): lo mismo con una extensión que con un firmador remoto.
const loginTemplate = () => ({ kind: 27235, created_at: Math.floor(Date.now() / 1000), tags: [['u', `${location.origin}/admin/api/login`], ['method', 'POST']], content: '' })

async function sendLogin(signed) {
  const res = await fetch('/admin/api/login', { method: 'POST', headers: { Authorization: 'Nostr ' + btoa(JSON.stringify(signed)) }, credentials: 'same-origin' })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `error ${res.status}`)
}

// ---------- entrar con un firmador remoto (NIP-46, p. ej. Clave en el iPhone) ----------

let remote = null // sesión NIP-46 en curso

function remoteReset() {
  if (remote) { remote.close(); remote = null }
  $('remote-box').hidden = true
  $('remote-btn').hidden = false
  $('remote-link').removeAttribute('href')
  $('remote-diag').textContent = ''
}

async function loginRemote() {
  const msg = $('remote-msg')
  const say = (t, err) => { msg.className = err ? 'msg err' : 'msg'; msg.textContent = t }
  remoteReset()
  $('remote-btn').hidden = true
  $('remote-box').hidden = false
  say('Preparando la conexión…')
  let session
  try {
    const { createSession } = await import('/admin/nip46.js')
    session = remote = createSession({
      onAuthUrl: (u) => say(`El firmador pide abrir esta dirección para continuar: ${u}`),
      onStatus: (t) => { if (remote === session || !session) $('remote-diag').textContent = `${new Date().toLocaleTimeString('es-ES')} · ${t}` },
    })
  } catch (err) {
    remoteReset()
    $('login-msg').className = 'msg err'
    $('login-msg').textContent = `No se pudo preparar la conexión: ${err.message || err}`
    return
  }
  $('remote-link').setAttribute('href', session.uri)
  say('Esperando a que apruebes la conexión en la app firmadora…')
  try {
    await session.waitForSigner()
    say('Conectado. Aprueba ahora la firma del inicio de sesión en la app…')
    const signed = JSON.parse(await session.request('sign_event', [JSON.stringify(loginTemplate())]))
    if (!signed || signed.kind !== 27235 || typeof signed.sig !== 'string') throw new Error('el firmador devolvió algo que no es la firma pedida')
    await sendLogin(signed)
    remoteReset()
    $('login-msg').textContent = ''
    start()
  } catch (err) {
    if (remote !== session) return // se canceló o se empezó de nuevo
    remoteReset()
    $('login-msg').className = 'msg err'
    $('login-msg').textContent = `No se pudo entrar: ${err.message || err}`
  }
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
    await sendLogin(await window.nostr.signEvent(loginTemplate()))
    msg.textContent = ''
    start()
  } catch (err) {
    msg.className = 'msg err'
    msg.textContent = `No se pudo entrar: ${err.message || err}`
  }
}

async function logout() {
  stop()
  modState = null
  searchState = null
  $('search-out').replaceChildren()
  await fetch('/admin/api/logout', { method: 'POST', credentials: 'same-origin' }).catch(() => {})
  show('login')
}

// ---------- datos ----------

async function load() {
  const res = await fetch('/admin/api/stats', { credentials: 'same-origin', cache: 'no-store' })
  if (res.status === 401) { stop(); show('login'); return }
  if (!res.ok) throw new Error(`error ${res.status}`)
  render(await res.json())
  await loadModeration(false)
  await loadHistory()
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
  $('subtitle').textContent = `Versión ${d.version} · en marcha desde hace ${duration(now - d.startedAt)}`

  const rejectedTotal = Object.values(d.activity.reasons).reduce((a, b) => a + b, 0)
  const cards = [
    ['Eventos guardados', fmt(d.events.total)], ['Claves distintas', fmt(d.events.pubkeys)], ['Últimas 24 h', fmt(d.events.last24h)],
    ['Base de datos', bytes(d.dbBytes)], ['Conexiones abiertas', fmt(d.connections)], ['Rechazos desde el arranque', fmt(rejectedTotal)],
  ]
  // Estado del servidor: disco usado (rojo a partir del 85 %) y última copia de seguridad (rojo si hace más de 36 h o no hay).
  const sv = d.server || {}
  const extra = []
  if (sv.diskTotal > 0) {
    const used = Math.round((1 - sv.diskFree / sv.diskTotal) * 100)
    extra.push({ l: 'Disco usado', v: `${used} %`, s: `${bytes(sv.diskFree)} libres`, warn: used >= 85 })
  }
  if (sv.backup && sv.backup.configured) {
    const b = sv.backup
    const old = !b.last || now - b.last > 36 * 3600
    extra.push({ l: 'Última copia de seguridad', v: b.last ? ago(b.last, now) : 'ninguna', s: b.last ? `${bytes(b.lastBytes)} · ${b.count} guardadas` : 'no se encontró ninguna', warn: old })
  }
  $('cards').replaceChildren(...cards.map(([l, v]) => ({ l, v })).concat(extra).map((c) => el('div', { class: 'card' }, el('div', { class: c.warn ? 'v warn' : 'v', text: c.v }), el('div', { class: 'l', text: c.l }), c.s ? el('div', { class: 's', text: c.s }) : null)))

  liveMinutes = d.activity.minutes
  renderActivity()

  const maxDay = Math.max(1, ...d.events.perDay.map((x) => x.count))
  $('growth').replaceChildren(...(d.events.perDay.length ? d.events.perDay.map((x) => el('div', { class: 'row' }, el('span', { text: x.day }), el('div', { class: 'bar' }, el('i', { width: `${Math.round((x.count / maxDay) * 100)}%` })), el('span', { text: fmt(x.count) }))) : [el('span', { class: 'muted', text: 'Sin eventos en los últimos 14 días.' })]))

  const max = Math.max(1, ...d.events.byKind.map((k) => k.count))
  $('kinds').replaceChildren(...d.events.byKind.map((k) =>
    el('div', { class: 'row' }, el('span', { text: kindName(k.kind) }), el('div', { class: 'bar' }, el('i', { width: `${Math.round((k.count / max) * 100)}%` })), el('span', { text: fmt(k.count) }))))
  if (!d.events.byKind.length) $('kinds').textContent = 'Todavía no hay eventos.'

  $('reasons').replaceChildren(...Object.entries(d.activity.reasons).sort((a, b) => b[1] - a[1]).map(([r, n]) => el('span', { class: 'chip' }, el('b', { text: r }), ` ${fmt(n)}`)))
  if (!Object.keys(d.activity.reasons).length) $('reasons').textContent = 'Ningún rechazo desde el arranque.'
  const tbody = $('rejections').querySelector('tbody')
  tbody.replaceChildren(...d.activity.rejections.map((r) => el('tr', {},
    el('td', { text: clock(r.t) }), el('td', { text: r.what === 'event' ? 'evento' : 'consulta' }),
    el('td', { text: r.kind >= 0 ? String(r.kind) : '—' }), el('td', {}, r.pubkey ? searchButton(r.pubkey) : '—'), el('td', { class: 'reason', text: r.reason }))))

  const noisyBody = $('noisy').querySelector('tbody')
  noisyBody.replaceChildren(...d.activity.noisy.map((n) => noisyRow(n, now)))
  if (!d.activity.noisy.length) noisyBody.append(el('tr', {}, el('td', { colspan: '6', class: 'muted', text: 'Ningún evento rechazado en las últimas 24 h.' })))

  $('recent').replaceChildren(...d.events.recent.map((e) => eventItem(e, now, { source: 'eventos recientes' })))

  const list = (a) => (a && a.length ? a.join(', ') : 'ninguno')
  const c = d.config
  fillKv($('config'), [
    ['NIPs', (c.nips || []).join(', ')], ['Retención', c.retentionDays ? `${c.retentionDays} días` : 'sin límite'], ['Contenido máx.', `${fmt(c.maxContentLength)} caracteres`], ['Mensaje máx.', bytes(c.maxMessageBytes)], ['Tags por evento', fmt(c.maxEventTags)],
    ['Eventos por consulta', fmt(c.maxLimit)], ['Sync NIP-77 máx.', fmt(c.maxNegentropyEvents)], ['Fecha futura máx.', `${Math.round(c.maxFutureSkewSec / 60)} min`],
    ['Prueba de trabajo', c.minPoW ? `${c.minPoW} bits` : 'no'], ['Auth obligatoria', c.authRequired ? 'sí' : 'no'], ['Tipos privados', list(c.privateKinds)],
    ['Eventos/min por IP', `${c.eventsPerMinute} (ráfaga ${c.eventsBurst})`], ['Consultas/min por IP', `${c.reqsPerMinute} (ráfaga ${c.reqsBurst})`], ['Conexiones/min por IP', `${c.connsPerMinute} (ráfaga ${c.connsBurst})`],
  ])
}

// Fila de «Claves más ruidosas»: clave (copiable), rechazos, último tipo y motivo, y las acciones.
function noisyRow(n, now) {
  const pk = el('button', { class: 'pk', type: 'button', title: 'Copiar clave completa', text: n.pubkey.slice(0, 12) + '…' })
  pk.addEventListener('click', () => navigator.clipboard.writeText(n.pubkey).then(() => { pk.textContent = 'copiada ✓'; setTimeout(() => { pk.textContent = n.pubkey.slice(0, 12) + '…' }, 1200) }).catch(() => {}))
  const acts = el('td', {})
  const find = el('button', { type: 'button', class: 'act', text: 'Buscar', title: 'Buscar lo que el relé tiene guardado de esta clave (los eventos efímeros no se guardan)' })
  find.addEventListener('click', () => { searchFor(n.pubkey, ''); $('search-panel').scrollIntoView({ block: 'start' }) })
  acts.append(find)
  if (n.mine) acts.append(el('span', { class: 'badge', text: 'tuya' }))
  else {
    const ban = el('button', { type: 'button', class: 'act danger', text: 'Banear', title: 'Banear esta clave' })
    ban.addEventListener('click', () => {
      if (!confirm(`¿Banear la clave ${n.pubkey.slice(0, 12)}…?\n${fmt(n.count)} rechazos (${n.reason}). Dejará de poder publicar; puedes deshacerlo desde «Claves baneadas».`)) return
      act('ban-pubkey', { pubkey: n.pubkey, reason: 'desde claves más ruidosas', deleteEvents: false }, 'Clave baneada')
    })
    acts.append(ban)
  }
  return el('tr', {}, el('td', {}, pk), el('td', { text: fmt(n.count) }), el('td', { text: String(n.kind) }), el('td', { class: 'reason', text: n.reason }), el('td', { text: ago(n.last, now) }), acts)
}

// Fila de un evento (en «Eventos recientes" y en los resultados de la búsqueda). Las acciones de
// moderación no aparecen en los eventos del dueño.
function eventItem(e, now, { source, searchKey } = {}) {
  const pk = el('button', { class: 'pk', type: 'button', title: searchKey ? 'Buscar todo lo de esta clave' : 'Copiar clave completa', text: e.pubkey.slice(0, 12) + '…' })
  if (searchKey) pk.addEventListener('click', () => searchFor(e.pubkey, ''))
  else pk.addEventListener('click', () => navigator.clipboard.writeText(e.pubkey).then(() => { pk.textContent = 'copiada ✓'; setTimeout(() => { pk.textContent = e.pubkey.slice(0, 12) + '…' }, 1200) }).catch(() => {}))
  const meta = el('div', { class: 'meta' }, el('span', { text: ago(e.createdAt, now) }), el('span', { text: kindName(e.kind) }), pk)
  if (!searchKey) {
    const more = el('button', { class: 'act', type: 'button', text: 'Sus eventos', title: 'Buscar todo lo que ha publicado esta clave' })
    more.addEventListener('click', () => { searchFor(e.pubkey, ''); $('search-panel').scrollIntoView({ block: 'start' }) })
    meta.append(more)
  }
  if (e.mine) meta.append(el('span', { class: 'badge', text: 'tuyo' }))
  else {
    const acts = el('span', { class: 'actions2' })
    const ban = el('button', { type: 'button', class: 'act danger', text: 'Banear clave', title: 'Banear la clave de este evento' })
    ban.addEventListener('click', () => {
      if (!confirm(`¿Banear la clave ${e.pubkey.slice(0, 12)}…?\nDejará de poder publicar. Puedes deshacerlo desde «Claves baneadas».`)) return
      const del = confirm('¿Borrar también todos sus eventos guardados?\n(Aceptar = sí, Cancelar = no, solo banear)')
      act('ban-pubkey', { pubkey: e.pubkey, reason: `desde ${source || 'la búsqueda'}`, deleteEvents: del }, (r) => (r.deleted ? `Clave baneada y ${r.deleted} evento(s) borrados` : 'Clave baneada'))
    })
    const veto = el('button', { type: 'button', class: 'act', text: 'Vetar evento', title: 'Vetar y borrar este evento' })
    veto.addEventListener('click', () => {
      if (!confirm('¿Vetar y borrar este evento? No podrá volver a publicarse.')) return
      act('ban-event', { id: e.id, reason: `desde ${source || 'la búsqueda'}` }, () => 'Evento vetado y borrado')
    })
    acts.append(veto, ban)
    meta.append(acts)
  }
  return el('li', {}, meta, e.content ? el('div', { class: 'body', text: e.content }) : null)
}

function fillKv(dl, rows) {
  dl.replaceChildren(...rows.flatMap(([k, v]) => [el('dt', { text: k }), el('dd', { text: String(v) })]))
}

let liveMinutes = []
let range = '60m'
let longHistory = null // último histórico pedido (para el periodo elegido)

const dayFmt = (unix) => { const d = new Date(unix * 1000); return `${two(d.getDate())}/${two(d.getMonth() + 1)}` }
const dateTimeFmt = (unix) => { const d = new Date(unix * 1000); return `${two(d.getDate())}/${two(d.getMonth() + 1)} ${two(d.getHours())}:${two(d.getMinutes())}` }
const hourFmt = (unix) => { const d = new Date(unix * 1000); return `${two(d.getDate())}/${two(d.getMonth() + 1)} ${two(d.getHours())}:00` }

// Pinta la gráfica y el resumen del periodo elegido.
function renderActivity() {
  $('range-label').textContent = { '60m': '· últimos 60 minutos', '24h': '· últimas 24 horas', '7d': '· últimos 7 días', '30d': '· últimos 30 días', '90d': '· últimos 90 días' }[range]
  document.querySelectorAll('.tabs [data-range]').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.range === range)))
  if (range === '60m') {
    renderChart(liveMinutes, (m) => clock(m.t).slice(0, 5))
    $('axis-start').textContent = liveMinutes.length ? clock(liveMinutes[0].t).slice(0, 5) : ''
    $('axis-end').textContent = 'ahora'
    const sum = (k) => liveMinutes.reduce((a, m) => a + m[k], 0)
    fillKv($('hist-summary'), [['Guardados', fmt(sum('saved'))], ['Efímeros', fmt(sum('ephemeral'))], ['Rechazados', fmt(sum('rejected'))], ['Autenticaciones', fmt(sum('authenticated'))]])
    return
  }
  if (!longHistory || longHistory.range !== range) { $('chart').replaceChildren(el('span', { class: 'muted', text: 'Cargando…' })); $('hist-summary').replaceChildren(); return }
  const label = longHistory.step === 3600 ? hourFmt : dayFmt
  renderChart(longHistory.buckets, (b) => label(b.t))
  $('axis-start').textContent = label(longHistory.buckets[0].t)
  $('axis-end').textContent = 'ahora'
  const t = longHistory.totals
  const rows = [['Guardados', fmt(t.saved)], ['Efímeros', fmt(t.ephemeral)], ['Rechazados', fmt(t.rejected)], ['Autenticaciones', fmt(t.authenticated)], ['Conexiones máx. a la vez', t.maxConns ? fmt(t.maxConns) : '—']]
  const reasons = Object.entries(longHistory.reasons).sort((a, b) => b[1] - a[1]).map(([r, n]) => `${r}: ${fmt(n)}`).join(' · ')
  if (reasons) rows.push(['Rechazos por motivo', reasons])
  if (longHistory.dbStart && longHistory.dbEnd) {
    const diff = longHistory.dbEnd - longHistory.dbStart
    rows.push(['Base de datos', `${bytes(longHistory.dbStart)} → ${bytes(longHistory.dbEnd)} (${diff >= 0 ? '+' : '−'}${bytes(Math.abs(diff))})`])
  }
  if (longHistory.eventsStart && longHistory.eventsEnd) {
    const diff = longHistory.eventsEnd - longHistory.eventsStart
    rows.push(['Eventos guardados', `${fmt(longHistory.eventsStart)} → ${fmt(longHistory.eventsEnd)} (${diff >= 0 ? '+' : '−'}${fmt(Math.abs(diff))})`])
  }
  fillKv($('hist-summary'), rows)
}

async function loadHistory() {
  if (range === '60m') return
  const wanted = range
  const res = await fetch(`/admin/api/history?range=${wanted}`, { credentials: 'same-origin', cache: 'no-store' })
  if (res.status === 401) { stop(); show('login'); return }
  if (!res.ok) throw new Error(`error ${res.status}`)
  longHistory = await res.json()
  if (range === wanted) renderActivity()
}

function renderChart(minutes, labelOf) {
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
      t.textContent = `${labelOf(m)} · guardados ${m.saved}, efímeros ${m.ephemeral}, rechazados ${m.rejected}, autenticaciones ${m.authenticated}`
      r.append(t)
      svg.append(r)
    }
  })
  $('chart').replaceChildren(svg)
}

// ---------- moderación ----------

let toastTimer = null
function toast(msg, isErr) {
  const t = $('toast')
  t.textContent = msg
  t.className = 'toast show' + (isErr ? ' err' : '')
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { t.className = 'toast'; t.textContent = '' }, isErr ? 7000 : 3500)
}

// Llama a una acción del panel. Devuelve la respuesta; si la sesión caducó, vuelve al login.
async function api(path, body) {
  const res = await fetch(`/admin/api/mod/${path}`, { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  if (res.status === 401) { stop(); show('login'); throw new Error('La sesión ha caducado: vuelve a entrar') }
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `error ${res.status}`)
  return data
}

// Hace una acción, avisa del resultado y refresca los datos.
async function act(path, body, okMsg, opts) {
  try {
    const r = await api(path, body)
    // Primero se refrescan las listas y luego se avisa: así, cuando ves «hecho», lo que hay en pantalla ya está al día.
    // (Un fallo al refrescar no convierte en error una acción que sí se hizo.)
    await Promise.all([load().catch(() => {}), loadModeration(!!(opts && opts.forceInfo)).catch(() => {}), refreshSearch().catch(() => {})])
    toast(typeof okMsg === 'function' ? okMsg(r) : okMsg, false)
    return true
  } catch (err) {
    toast(err.message || String(err), true)
    return false
  }
}

let modState = null
function shortKey(k) { return k.length > 16 ? `${k.slice(0, 10)}…${k.slice(-4)}` : k }

function fillList(id, rows, removeLabel, onRemove, describe) {
  const ul = $(id)
  if (!rows.length) { ul.replaceChildren(el('li', { class: 'none', text: 'Ninguno' })); return }
  ul.replaceChildren(...rows.map((r) => {
    const what = el('span', { class: 'what' }, describe(r), r.reason ? el('span', { class: 'why', text: ` · ${r.reason}` }) : null)
    const b = el('button', { type: 'button', text: removeLabel })
    b.addEventListener('click', () => onRemove(r))
    return el('li', {}, what, b)
  }))
}

async function loadModeration(forceInfo) {
  const res = await fetch('/admin/api/moderation', { credentials: 'same-origin', cache: 'no-store' })
  if (res.status === 401) { stop(); show('login'); return }
  if (!res.ok) throw new Error(`error ${res.status}`)
  const m = await res.json()
  const first = modState === null
  modState = m

  fillList('l-banned', m.bannedPubkeys, 'Quitar', (r) => act('unban-pubkey', { pubkey: r.key }, 'Baneo quitado'), (r) => el('code', { text: shortKey(r.key), title: r.key }))
  fillList('l-allowed', m.allowedPubkeys, 'Quitar', (r) => {
    const last = m.allowedPubkeys.length === 1
    if (last && !confirm('Es la última clave de la lista blanca: al quitarla el relé vuelve a ser abierto para todos. ¿Continuar?')) return
    act('unallow-pubkey', { pubkey: r.key }, 'Clave quitada de la lista blanca')
  }, (r) => el('code', { text: shortKey(r.key), title: r.key }))
  fillList('l-events', m.bannedEvents, 'Quitar', (r) => act('unban-event', { id: r.key }, 'Veto quitado'), (r) => el('code', { text: shortKey(r.key), title: r.key }))
  fillList('l-ips', m.blockedIPs, 'Desbloquear', (r) => act('ip', { ip: r.key, action: 'unblock' }, 'IP desbloqueada'), (r) => el('code', { text: r.key }))
  const kinds = [...(m.disallowedKinds || []).map((k) => ({ key: String(k), kind: k, reason: '', rule: 'prohibido' })), ...(m.allowedKinds || []).map((k) => ({ key: String(k), kind: k, reason: '', rule: 'permitido' }))]
  fillList('l-kinds', kinds, 'Quitar', (r) => act('kind', { kind: r.kind, rule: 'clear' }, 'Regla quitada'), (r) => el('span', { text: `${kindName(r.kind)} — ${r.rule}` }))

  renderHistory(m.history || [])

  // El formulario de información solo se rellena al principio y tras guardar/restaurar:
  // si se rellenara en cada refresco borraría lo que estés escribiendo.
  if (first || forceInfo) {
    const f = $('f-info')
    f.elements.name.value = m.info.name
    f.elements.description.value = m.info.description
    for (const k of INFO_FIELDS) f.elements[k].value = infoText(m.info, k)
  }
  for (const k of INFO_FIELDS) $(`o-${k}`).hidden = !m.infoOverrides[k]
}

// Historial de acciones (lo registra el relé; el origen es el panel o un cliente NIP-86).
const ACTION_LABELS = {
  'ban-pubkey': 'Clave baneada', 'unban-pubkey': 'Baneo quitado', 'allow-pubkey': 'Clave permitida', 'unallow-pubkey': 'Quitada de la lista blanca',
  'ban-event': 'Evento vetado', 'unban-event': 'Veto quitado', 'kind-allow': 'Tipo permitido', 'kind-disallow': 'Tipo prohibido', 'kind-clear': 'Regla de tipo quitada',
  'ip-block': 'IP bloqueada', 'ip-unblock': 'IP desbloqueada', info: 'Información del relé', backup: 'Copia de seguridad descargada', login: 'Inicio de sesión', logout: 'Cierre de sesión',
}
function renderHistory(items) {
  const body = $('audit').querySelector('tbody')
  body.replaceChildren(...items.map((a) => {
    let target = el('td', { text: '' })
    if (a.target) {
      const txt = /^[0-9a-f]{64}$/.test(a.target) ? a.target.slice(0, 12) + '…' : a.target
      target = el('td', {}, el('code', { text: txt, title: a.target }))
    }
    return el('tr', {}, el('td', { text: dateTimeFmt(a.t) }), el('td', { text: ACTION_LABELS[a.action] || a.action }), target,
      el('td', { class: 'reason', text: a.detail || '' }), el('td', { text: a.source === 'nip86' ? 'NIP-86' : 'panel' }))
  }))
  if (!items.length) body.append(el('tr', {}, el('td', { colspan: '5', class: 'muted', text: 'Todavía no hay acciones anotadas.' })))
}

const INFO_FIELDS = ['name', 'description', 'icon', 'contact', 'tags', 'languages', 'postingPolicy']
// Las listas (etiquetas, idiomas) se muestran como «a, b, c».
const infoText = (info, k) => (Array.isArray(info[k]) ? info[k].join(', ') : info[k] || '')

function onSubmit(id, handler) {
  $(id).addEventListener('submit', async (ev) => {
    ev.preventDefault()
    const f = ev.currentTarget
    const ok = await handler(f)
    if (ok) f.reset()
  })
}

onSubmit('f-ban', (f) => {
  const pubkey = f.elements.pubkey.value.trim()
  if (!confirm(`¿Banear esta clave?\n${pubkey}`)) return false
  return act('ban-pubkey', { pubkey, reason: f.elements.reason.value, deleteEvents: f.elements.deleteEvents.checked }, (r) => (r.deleted ? `Clave baneada y ${r.deleted} evento(s) borrados` : 'Clave baneada'))
})
onSubmit('f-allow', (f) => {
  if (modState && modState.allowedPubkeys.length === 0 && !confirm('Al permitir la primera clave, el relé pasa a ser de escritura restringida: solo podrán publicar las claves permitidas (y tú). ¿Continuar?')) return false
  return act('allow-pubkey', { pubkey: f.elements.pubkey.value.trim(), reason: f.elements.reason.value }, 'Clave permitida')
})
onSubmit('f-event', (f) => {
  if (!confirm('¿Vetar este evento? Se borra si está guardado y no podrá volver a publicarse.')) return false
  return act('ban-event', { id: f.elements.id.value.trim(), reason: f.elements.reason.value }, (r) => (r.deleted ? 'Evento vetado y borrado' : 'Evento vetado (no estaba guardado)'))
})
onSubmit('f-kind', (f) => {
  const rule = f.elements.rule.value
  if (rule === 'allow' && modState && modState.allowedKinds.length === 0 && !confirm('Al permitir el primer tipo, SOLO pasarán los tipos permitidos (el resto se rechaza). ¿Continuar?')) return false
  return act('kind', { kind: Number(f.elements.kind.value), rule }, 'Regla aplicada')
})
onSubmit('f-ip', (f) => act('ip', { ip: f.elements.ip.value.trim(), action: 'block', reason: f.elements.reason.value }, 'IP bloqueada'))
$('f-info').addEventListener('submit', async (ev) => {
  ev.preventDefault()
  const f = ev.currentTarget
  // Solo se envía lo que has cambiado (así un icono vacío que no tocas no da error).
  const body = {}
  for (const k of INFO_FIELDS) if (modState && f.elements[k].value.trim() !== infoText(modState.info, k)) body[k] = f.elements[k].value
  if (!Object.keys(body).length) { toast('No hay nada que guardar', false); return }
  await act('info', body, 'Información guardada', { forceInfo: true })
})
$('info-reset').addEventListener('click', async () => {
  if (!confirm('¿Restaurar toda la información del relé a los valores de la configuración?')) return
  await act('info', { reset: INFO_FIELDS }, 'Restaurados los de la configuración', { forceInfo: true })
})

document.querySelectorAll('.tabs [data-range]').forEach((b) => b.addEventListener('click', () => {
  range = b.dataset.range
  renderActivity()
  loadHistory().catch((e) => { $('updated').textContent = `Error al cargar el histórico: ${e.message}` })
}))

// ---------- copia de seguridad ----------

$('backup-btn').addEventListener('click', async () => {
  const btn = $('backup-btn')
  if (btn.disabled) return
  btn.disabled = true
  const label = btn.textContent
  btn.textContent = 'Preparando la copia…'
  try {
    const res = await fetch('/admin/api/backup', { credentials: 'same-origin', cache: 'no-store' })
    if (res.status === 401) { stop(); show('login'); return }
    if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || `error ${res.status}`)
    const name = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') || '')?.[1] || 'nostr-relay-khatru.sqlite.gz'
    const url = URL.createObjectURL(await res.blob())
    const a = el('a', { href: url, download: name })
    document.body.append(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 10000)
    toast(`Copia descargada: ${name}`, false)
    loadModeration(false).catch(() => {}) // para que salga en el historial
  } catch (err) {
    toast(`No se pudo descargar la copia: ${err.message}`, true)
  } finally {
    btn.disabled = false
    btn.textContent = label
  }
})

// ---------- búsqueda ----------

let searchState = null // { q, kind, next } de la búsqueda activa (null = ninguna)

function searchButton(text) {
  const b = el('button', { class: 'pk', type: 'button', title: 'Buscar eventos de esta clave', text })
  b.addEventListener('click', () => { searchFor(text, ''); $('search-panel').scrollIntoView({ block: 'start' }) })
  return b
}

// Una búsqueda nueva desde un botón (una clave, un tipo…) parte sin fechas.
async function searchFor(q, kind) {
  const f = $('f-search').elements
  f.q.value = q
  f.kind.value = kind
  f.from.value = ''
  f.to.value = ''
  await runSearch({ q, kind, from: '', to: '' }, false)
}

// 'AAAA-MM-DD' (la fecha del <input type=date>) a segundos unix: el principio o el final de ese día en la hora local.
const dayStart = (v) => Math.floor(new Date(`${v}T00:00:00`).getTime() / 1000)
const dayEnd = (v) => Math.floor(new Date(`${v}T23:59:59`).getTime() / 1000)
const niceDay = (v) => v.split('-').reverse().join('/')
const isoDay = (d) => `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}`

async function runSearch(st, append) {
  const out = $('search-out')
  const params = new URLSearchParams({ q: st.q || '' })
  if (st.kind !== '' && st.kind != null) params.set('kind', String(st.kind))
  if (st.from) params.set('since', String(dayStart(st.from)))
  if (st.to) params.set('until', String(dayEnd(st.to)))
  if (append && st.next) params.set('next', st.next)
  let res
  try {
    res = await fetch(`/admin/api/search?${params}`, { credentials: 'same-origin', cache: 'no-store' })
  } catch (err) { toast(`No se pudo buscar: ${err.message}`, true); return }
  if (res.status === 401) { stop(); show('login'); return }
  const data = await res.json().catch(() => ({}))
  if (!res.ok) { toast(data.error || `error ${res.status}`, true); return }
  searchState = { q: st.q, kind: st.kind, from: st.from || '', to: st.to || '', next: data.next || '' }
  const now = Math.floor(Date.now() / 1000)
  if (!append) out.replaceChildren()

  if (!append) {
    out.append(el('p', { class: 'resultinfo', text: `${data.totalExact ? fmt(data.total) : `más de ${fmt(data.total)}`} resultado(s): ${data.what}${rangeText(st)}` }))
    if (data.key) out.append(keyCard(data.key))
    out.append(el('ul', { class: 'recent', id: 'search-list' }))
    if (!data.events.length) out.append(el('p', { class: 'muted', text: 'No hay eventos que coincidan.' }))
  }
  const list = $('search-list')
  data.events.forEach((e) => list.append(eventItem(e, now, { source: 'la búsqueda', searchKey: true })))
  $('search-more')?.remove()
  if (data.next) {
    const more = el('button', { type: 'button', id: 'search-more', text: 'Cargar más' })
    more.addEventListener('click', () => runSearch(searchState, true))
    out.append(more)
  }
}

function rangeText(st) {
  if (st.from && st.to) return st.from === st.to ? ` · el ${niceDay(st.from)}` : ` · del ${niceDay(st.from)} al ${niceDay(st.to)}`
  if (st.from) return ` · desde el ${niceDay(st.from)}`
  if (st.to) return ` · hasta el ${niceDay(st.to)}`
  return ''
}

// Repite la búsqueda activa (desde la primera página), por ejemplo tras vetar o banear.
async function refreshSearch() {
  if (searchState) await runSearch({ q: searchState.q, kind: searchState.kind, from: searchState.from, to: searchState.to }, false)
}

function keyCard(k) {
  const card = el('div', { class: 'keycard' })
  const name = el('span', { class: 'name', text: k.name || (k.isOwner ? 'Tú (dueño del relé)' : 'Clave sin perfil guardado') })
  const top = el('div', { class: 'top' }, name)
  const badges = el('span', { class: 'actions2' })
  if (k.isOwner) badges.append(el('span', { class: 'badge', text: 'dueño' }))
  if (k.banned) badges.append(el('span', { class: 'badge warn', text: 'baneada' }))
  if (k.allowed) badges.append(el('span', { class: 'badge', text: 'en la lista blanca' }))
  if (!k.isOwner) {
    const b = el('button', { type: 'button', class: k.banned ? 'act' : 'act danger', text: k.banned ? 'Quitar baneo' : 'Banear clave' })
    b.addEventListener('click', () => {
      if (k.banned) act('unban-pubkey', { pubkey: k.pubkey }, 'Baneo quitado')
      else {
        if (!confirm(`¿Banear esta clave?\n${k.npub}`)) return
        const del = confirm('¿Borrar también todos sus eventos guardados?\n(Aceptar = sí, Cancelar = no, solo banear)')
        act('ban-pubkey', { pubkey: k.pubkey, reason: 'desde la búsqueda', deleteEvents: del }, (r) => (r.deleted ? `Clave baneada y ${r.deleted} evento(s) borrados` : 'Clave baneada'))
      }
    })
    badges.append(b)
  }
  top.append(badges)
  const npub = el('button', { class: 'pk', type: 'button', title: 'Copiar el npub', text: k.npub })
  npub.addEventListener('click', () => navigator.clipboard.writeText(k.npub).then(() => { npub.textContent = 'copiado ✓'; setTimeout(() => { npub.textContent = k.npub }, 1200) }).catch(() => {}))
  const when = k.events ? `${fmt(k.events)} evento(s) guardados · el primero ${dateTimeFmt(k.first)}, el último ${dateTimeFmt(k.last)}` : 'Sin eventos guardados'
  card.append(top, el('div', { class: 'small' }, npub), el('div', { class: 'muted small', text: when }))
  if (k.byKind.length) {
    const kinds = el('div', { class: 'kinds' })
    k.byKind.forEach((x) => {
      const c = el('button', { type: 'button', class: 'chipbtn', title: 'Ver solo este tipo', text: `${kindName(x.kind)} · ${fmt(x.count)}` })
      c.addEventListener('click', () => searchFor(k.pubkey, String(x.kind)))
      kinds.append(c)
    })
    card.append(kinds)
  }
  return card
}

$('f-search').addEventListener('submit', (ev) => {
  ev.preventDefault()
  const f = ev.currentTarget
  if (f.elements.from.value && f.elements.to.value && f.elements.from.value > f.elements.to.value) { toast('«Desde» no puede ser posterior a «Hasta»', true); return }
  runSearch({ q: f.elements.q.value.trim(), kind: f.elements.kind.value.trim(), from: f.elements.from.value, to: f.elements.to.value }, false)
})
document.querySelectorAll('#f-search .presets [data-days]').forEach((b) => b.addEventListener('click', () => {
  const f = $('f-search').elements
  const today = new Date()
  const start = new Date(today)
  start.setDate(start.getDate() - (Number(b.dataset.days) - 1))
  f.from.value = isoDay(start)
  f.to.value = isoDay(today)
}))
$('search-clear').addEventListener('click', () => { $('f-search').reset(); $('search-out').replaceChildren(); searchState = null })

// ---------- ayuda ----------

const helpDialog = $('help')
function openHelp(anchor) {
  if (!helpDialog.open) helpDialog.showModal()
  const target = anchor && document.getElementById(anchor)
  // el contenido se desplaza dentro del diálogo; sin ancla, se vuelve arriba
  if (target) target.scrollIntoView({ block: 'start' })
  else $('help-body').scrollTop = 0
}
$('help-btn').addEventListener('click', () => openHelp())
$('help-close').addEventListener('click', () => helpDialog.close())
// clic fuera del cuadro (en el fondo oscuro) cierra
helpDialog.addEventListener('click', (e) => { if (e.target === helpDialog) helpDialog.close() })
// los enlaces del índice se desplazan dentro del diálogo sin cambiar la URL
document.querySelectorAll('.toc a').forEach((a) => a.addEventListener('click', (e) => { e.preventDefault(); document.getElementById(a.getAttribute('href').slice(1)).scrollIntoView({ block: 'start' }) }))
document.querySelectorAll('[data-help]').forEach((b) => b.addEventListener('click', () => openHelp(b.dataset.help)))

// ---------- arranque ----------

if (!window.nostr) $('remote-login').open = true // sin extensión (un móvil), lo normal es entrar con un firmador remoto
$('login-btn').addEventListener('click', login)
$('remote-btn').addEventListener('click', loginRemote)
$('remote-cancel').addEventListener('click', () => { remoteReset(); $('login-msg').textContent = '' })
$('remote-copy').addEventListener('click', () => {
  const href = $('remote-link').getAttribute('href')
  if (!href) return
  navigator.clipboard.writeText(href).then(() => toast('Enlace copiado', false)).catch(() => toast('No se pudo copiar el enlace', true))
})
$('logout').addEventListener('click', logout)
$('refresh').addEventListener('click', () => load().catch((e) => { $('updated').textContent = `Error al actualizar: ${e.message}` }))
document.addEventListener('visibilitychange', () => { if (!document.hidden && timer && Date.now() - lastOk > REFRESH_MS) load().catch(() => {}) })

fetch('/admin/api/session', { credentials: 'same-origin', cache: 'no-store' })
  .then((r) => { if (r.ok) start(); else show('login') })
  .catch(() => show('login'))
