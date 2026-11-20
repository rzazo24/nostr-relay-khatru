#!/usr/bin/env node
// Reenvía a los relés de una cuenta lo que esa cuenta publicó en las últimas horas y les falta, tal cual (ya firmado: no firma nada).
// Pensado para el dueño del relé: este relé guarda todo lo suyo, así que sirve de copia maestra. Lo lanza cron cada noche.
// Sin dependencias (Node 22+ trae WebSocket).
//
//   node rebroadcast-own.mjs <relé origen> <npub o clave hex> [--hours=48] [--skip-kinds=7] [--dry-run]
//   node rebroadcast-own.mjs wss://relay.hivescope.xyz npub1... --hours=48
//
// Los relés destino salen de la lista de relés publicada por la cuenta (NIP-65, kind 10002: los de escritura), leída del origen o, si no
// la tiene, de purplepag.es. A cada uno se le pregunta por id qué tiene y se le manda lo que no. Los eventos efímeros (20000-29999) no se
// tocan, ni las reacciones (kind 7) salvo que se pida otra cosa con --skip-kinds: una reacción va dirigida al autor de la nota, y los relés con
// web of trust (nostr.bitcoiner.social, offchain.pub) rechazan las que apuntan a cuentas de fuera, así que reenviarlas cada noche solo era ruido.
// Cada relé aplica sus propias reglas: un rechazo se apunta y ya está, mañana se vuelve a intentar.

const [src, who, ...flags] = process.argv.slice(2)
const dry = flags.includes('--dry-run')
const hours = Number(flags.find((f) => f.startsWith('--hours='))?.slice(8) || 48)
const skipFlag = flags.find((f) => f.startsWith('--skip-kinds='))
const skip = new Set((skipFlag ? skipFlag.slice(13) : '7').split(',').filter(Boolean).map(Number))
if (!src || !who || !(hours > 0)) { console.error('Uso: node rebroadcast-own.mjs <origen> <npub|hex> [--hours=48] [--skip-kinds=7] [--dry-run]'); process.exit(2) }
const WS = globalThis.WebSocket
if (!WS) { console.error('Esta versión de Node no trae WebSocket: hace falta Node 22+.'); process.exit(2) }

const CHARSET = 'qpzry9x8gf2tvdw0s3jn54khce6mua7l'
function npubToHex(s) {
  if (/^[0-9a-f]{64}$/i.test(s)) return s.toLowerCase()
  const data = [...s.slice(s.lastIndexOf('1') + 1, -6)].map((c) => CHARSET.indexOf(c))
  if (!s.startsWith('npub1') || data.includes(-1)) throw new Error('clave no válida (usa npub1… o 64 caracteres hex)')
  let acc = 0, bits = 0; const out = []
  for (const v of data) { acc = (acc << 5) | v; bits += 5; if (bits >= 8) { bits -= 8; out.push((acc >> bits) & 255); acc &= (1 << bits) - 1 } }
  const hex = Buffer.from(out).toString('hex')
  if (hex.length !== 64) throw new Error('npub con longitud inválida')
  return hex
}
const author = npubToHex(who)

/** Una consulta: devuelve los eventos hasta EOSE (o el tiempo). */
function query(url, filter, ms = 15000) {
  return new Promise((resolve) => {
    const got = new Map(); let ws
    const end = () => { clearTimeout(t); try { ws.close() } catch {} resolve([...got.values()]) }
    const t = setTimeout(end, ms)
    try { ws = new WS(url) } catch { return end() }
    ws.onopen = () => ws.send(JSON.stringify(['REQ', 'q', filter]))
    ws.onmessage = (m) => { let x; try { x = JSON.parse(m.data) } catch { return } if (x[0] === 'EVENT' && x[2]?.pubkey === author) got.set(x[2].id, x[2]); if (x[0] === 'EOSE' || x[0] === 'CLOSED') end() }
    ws.onerror = end
  })
}

/** Manda eventos de uno en uno por la misma conexión y recoge cada OK. */
function send(url, events, ms = 30000) {
  return new Promise((resolve) => {
    const res = new Map(); let ws
    const end = () => { clearTimeout(t); try { ws.close() } catch {} resolve(res) }
    const t = setTimeout(end, ms)
    try { ws = new WS(url) } catch { return end() }
    ws.onopen = () => { for (const e of events) ws.send(JSON.stringify(['EVENT', e])) }
    ws.onmessage = (m) => { let x; try { x = JSON.parse(m.data) } catch { return } if (x[0] === 'OK') { res.set(x[1], x[2] ? 'ok' : String(x[3] || 'rechazado').slice(0, 80)); if (res.size === events.length) end() } }
    ws.onerror = end
  })
}

const since = Math.floor(Date.now() / 1000) - hours * 3600
const stamp = new Date().toISOString().slice(0, 16)

// 1. Los relés de escritura de la cuenta
const lists = [...await query(src, { kinds: [10002], authors: [author] }), ...await query('wss://purplepag.es', { kinds: [10002], authors: [author] })]
const list = lists.sort((a, b) => b.created_at - a.created_at)[0]
const norm = (u) => u.trim().replace(/\/+$/, '')
const targets = [...new Set((list?.tags ?? []).filter((t) => t[0] === 'r' && t[1]?.startsWith('wss://') && (!t[2] || t[2] === 'write')).map((t) => norm(t[1])))].filter((u) => u !== norm(src))
if (!targets.length) { console.log(`${stamp} sin lista de relés publicada: nada que hacer`); process.exit(0) }

// 2. Lo publicado en el periodo, desde el origen (paginando hacia atrás)
const mine = new Map(); let until
for (let page = 0; page < 20; page++) {
  const batch = await query(src, { authors: [author], since, ...(until ? { until } : {}), limit: 500 })
  const fresh = batch.filter((e) => !mine.has(e.id))
  for (const e of fresh) if ((e.kind < 20000 || e.kind >= 30000) && !skip.has(e.kind)) mine.set(e.id, e)
  if (batch.length < 500 || !fresh.length) break
  until = Math.min(...batch.map((e) => e.created_at))
}
const events = [...mine.values()]
if (!events.length) { console.log(`${stamp} nada publicado en las últimas ${hours} h`); process.exit(0) }

// 3. Relé a relé: qué le falta, y mandarlo
const lines = []
for (const url of targets) {
  const have = new Set()
  for (let i = 0; i < events.length; i += 100) for (const e of await query(url, { ids: events.slice(i, i + 100).map((e) => e.id) })) have.add(e.id)
  const missing = events.filter((e) => !have.has(e.id))
  if (!missing.length || dry) { lines.push(`${url}: tenía ${have.size}/${events.length}${dry && missing.length ? `, faltan ${missing.length} (prueba: no se envía)` : ''}`); continue }
  const res = await send(url, missing)
  const ok = [...res.values()].filter((v) => v === 'ok').length
  const why = [...new Set([...res.values()].filter((v) => v !== 'ok'))].slice(0, 2)
  lines.push(`${url}: tenía ${have.size}/${events.length}, enviados ${missing.length}, aceptados ${ok}${missing.length - res.size ? `, sin respuesta ${missing.length - res.size}` : ''}${why.length ? ` (${why.join('; ')})` : ''}`)
}
console.log(`${stamp} ${events.length} eventos de las últimas ${hours} h · ${lines.join(' | ')}`)
