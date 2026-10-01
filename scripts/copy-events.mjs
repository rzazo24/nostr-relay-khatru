#!/usr/bin/env node
// Copia todos los eventos de UNA cuenta de un relé a otro, tal cual (ya firmados: no firma nada).
// Sin dependencias en Node 22 o superior (trae WebSocket). Con una versión anterior: `npm install ws` en la misma carpeta.
//
//   node copy-events.mjs <relé origen> <relé destino> <npub o clave hex> [--dry-run]
//   node copy-events.mjs ws://umbrel.tailb59349.ts.net:4848 wss://relay.hivescope.xyz npub1... --dry-run
//
// Lee el origen paginando hacia atrás en el tiempo, se salta lo que el destino ya tiene y publica el resto.
// El destino comprueba las firmas y aplica sus propias reglas; nada se borra ni se modifica en el origen.

const [src, dst, who, ...flags] = process.argv.slice(2)
const dry = flags.includes('--dry-run')
if (!src || !dst || !who) {
  console.error('Uso: node copy-events.mjs <origen> <destino> <npub|hex> [--dry-run]')
  process.exit(2)
}
let WS = globalThis.WebSocket
if (!WS) {
  try { WS = (await import('ws')).default } catch {
    console.error('Esta versión de Node no trae WebSocket. Actualiza a Node 22+ o ejecuta `npm install ws` en esta carpeta.')
    process.exit(2)
  }
}

const CHARSET = 'qpzry9x8gf2tvdw0s3jn54khce6mua7l'
function npubToHex(s) { // decodificación bech32 mínima (sin dependencias)
  if (/^[0-9a-f]{64}$/i.test(s)) return s.toLowerCase()
  const data = [...s.slice(s.lastIndexOf('1') + 1, -6)].map((c) => CHARSET.indexOf(c))
  if (!s.startsWith('npub1') || data.includes(-1)) throw new Error('clave no válida (usa npub1… o 64 caracteres hex)')
  let acc = 0, bits = 0; const out = []
  for (const v of data) { acc = (acc << 5) | v; bits += 5; if (bits >= 8) { bits -= 8; out.push((acc >> bits) & 255); acc &= (1 << bits) - 1 } }
  const hex = Buffer.from(out).toString('hex')
  if (hex.length !== 64) throw new Error('npub con longitud inválida')
  return hex
}

// Abre un socket, manda mensajes y recoge respuestas hasta que `done` diga basta (o pasa el tiempo).
function talk(url, messages, done, ms = 30000) {
  return new Promise((resolve, reject) => {
    const ws = new WS(url), got = []
    const t = setTimeout(() => { ws.close(); reject(new Error(`${url}: tiempo agotado`)) }, ms)
    ws.addEventListener('open', () => messages.forEach((m) => ws.send(JSON.stringify(m))))
    ws.addEventListener('error', (ev) => { clearTimeout(t); reject(new Error(`${url}: no se pudo conectar (${ev.message || ev.error?.message || ev.error?.code || 'sin detalle'})`)) })
    ws.addEventListener('close', (ev) => { if (ev.code && ev.code !== 1000 && ev.code !== 1005 && !got.length) { clearTimeout(t); reject(new Error(`${url}: conexión cerrada (código ${ev.code}${ev.reason ? ': ' + ev.reason : ''})`)) } })
    ws.addEventListener('message', (ev) => { const d = JSON.parse(String(ev.data)); got.push(d); if (done(d)) { clearTimeout(t); ws.close(); resolve(got) } })
  })
}

process.on('uncaughtException', (e) => { console.error(`\nError: ${e.message}`); process.exit(1) })
process.on('unhandledRejection', (e) => { console.error(`\nError: ${e.message || e}`); process.exit(1) })

const pk = npubToHex(who)
console.log(`Origen: ${src}\nDestino: ${dst}\nCuenta: ${pk}${dry ? '\n(modo prueba: no se publica nada)' : ''}\n`)

// 1) leer todo del origen, hacia atrás con `until`
const events = new Map()
let until = Math.floor(Date.now() / 1000) + 60, rounds = 0
for (;;) {
  const r = await talk(src, [['REQ', 'c', { authors: [pk], until, limit: 500 }]], (d) => d[0] === 'EOSE' || d[0] === 'CLOSED')
  if (r.at(-1)[0] === 'CLOSED') throw new Error(`el origen rechazó la consulta: ${r.at(-1)[2]}`)
  const batch = r.filter((d) => d[0] === 'EVENT').map((d) => d[2]).filter((e) => e.pubkey === pk)
  const before = events.size
  batch.forEach((e) => events.set(e.id, e))
  process.stdout.write(`\rleídos: ${events.size}`)
  if (!batch.length || events.size === before || ++rounds > 200) break
  until = Math.min(...batch.map((e) => e.created_at)) // el siguiente tramo empieza donde acabó éste (se repite 1 segundo; los duplicados no cuentan)
}
console.log(`\nEventos de la cuenta en el origen: ${events.size}`)
if (!events.size) process.exit(0)

// 2) qué tiene ya el destino
const ids = [...events.keys()], have = new Set()
for (let i = 0; i < ids.length; i += 200) {
  const r = await talk(dst, [['REQ', 'h', { ids: ids.slice(i, i + 200) }]], (d) => d[0] === 'EOSE' || d[0] === 'CLOSED')
  r.filter((d) => d[0] === 'EVENT').forEach((d) => have.add(d[2].id))
}
const todo = [...events.values()].filter((e) => !have.has(e.id)).sort((a, b) => a.created_at - b.created_at)
const kinds = {}; todo.forEach((e) => (kinds[e.kind] = (kinds[e.kind] || 0) + 1))
console.log(`Ya estaban en el destino: ${have.size}. Por copiar: ${todo.length} ${JSON.stringify(kinds)}`)
if (dry || !todo.length) process.exit(0)

// 3) publicar (una a una, con una pausa corta para respetar los límites de velocidad)
let ok = 0; const failed = {}
for (const [i, e] of todo.entries()) {
  try {
    const r = await talk(dst, [['EVENT', e]], (d) => d[0] === 'OK', 10000)
    const o = r.find((d) => d[0] === 'OK')
    if (o[2]) ok++; else failed[o[3]] = (failed[o[3]] || 0) + 1
  } catch (err) { failed[err.message] = (failed[err.message] || 0) + 1 }
  process.stdout.write(`\rpublicados: ${ok}/${todo.length}`)
  if (i % 20 === 19) await new Promise((r) => setTimeout(r, 500))
}
console.log(`\nCopiados: ${ok}. Rechazados: ${todo.length - ok}`)
for (const [why, n] of Object.entries(failed)) console.log(`  ${n} × ${why}`)
