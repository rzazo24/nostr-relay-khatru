// Prueba de humo contra un relé real (por defecto ws://localhost:3334):
//   RELAY_URL=ws://localhost:3334 npm test
// Para las pruebas de volumen conviene arrancar el relé con límites de velocidad altos:
//   RELAY_EVENTS_PER_MINUTE=1000 RELAY_EVENTS_BURST=1000 (ver el workflow de CI).
import assert from 'node:assert/strict'
import WebSocket from 'ws'
import { Relay, useWebSocketImplementation } from 'nostr-tools/relay'
import { finalizeEvent, generateSecretKey, getPublicKey } from 'nostr-tools/pure'

useWebSocketImplementation(WebSocket)

const RELAY_URL = process.env.RELAY_URL ?? 'ws://localhost:3334'
const HTTP_URL = RELAY_URL.replace(/^ws/, 'http')
const sk = generateSecretKey()
const pk = getPublicKey(sk)
const now = () => Math.floor(Date.now() / 1000)
const sign = (kind, content, tags = [], created_at = now()) => finalizeEvent({ kind, content, tags, created_at }, sk)

let passed = 0
async function test(name, fn) {
  try {
    await fn()
    passed++
    console.log(`ok   ${name}`)
  } catch (err) {
    console.error(`FAIL ${name}\n     ${err.message}`)
    process.exitCode = 1
  }
}

async function query(relay, filter) {
  return new Promise((resolve) => {
    const events = []
    const sub = relay.subscribe([filter], {
      onevent: (e) => events.push(e),
      oneose: () => {
        sub.close()
        resolve(events)
      },
    })
  })
}

async function rejection(relay, event) {
  try {
    await relay.publish(event)
    return null
  } catch (err) {
    return String(err.message ?? err)
  }
}

const relay = await Relay.connect(RELAY_URL)

await test('NIP-11: el documento de información incluye nombre y límites', async () => {
  const res = await fetch(HTTP_URL, { headers: { Accept: 'application/nostr+json' } })
  assert.equal(res.status, 200)
  const doc = await res.json()
  assert.ok(doc.name)
  assert.ok(doc.limitation.max_content_length > 0)
  assert.ok(doc.limitation.max_limit > 0)
})

const note = sign(1, 'hola desde la prueba de humo')

await test('publica una nota y la recupera por id y por autor', async () => {
  await relay.publish(note)
  const byId = await query(relay, { ids: [note.id] })
  assert.equal(byId.length, 1)
  assert.equal(byId[0].content, note.content)
  const byAuthor = await query(relay, { authors: [pk], kinds: [1] })
  assert.ok(byAuthor.some((e) => e.id === note.id))
})

await test('NIP-45: COUNT devuelve el número de eventos', async () => {
  const n = await relay.count([{ authors: [pk], kinds: [1] }], { id: 'count-test' })
  assert.ok(n >= 1)
})

await test('acepta kinds arbitrarios (relé de propósito general)', async () => {
  await relay.publish(sign(30023, '# artículo largo', [['d', 'prueba']]))
  await relay.publish(sign(7, '+', [['e', note.id], ['p', pk]]))
})

await test('rechaza contenido demasiado largo', async () => {
  const reason = await rejection(relay, sign(1, 'a'.repeat(70000)))
  assert.ok(reason?.includes('too long'), `motivo: ${reason}`)
})

await test('rechaza un created_at muy en el futuro', async () => {
  const reason = await rejection(relay, sign(1, 'del futuro', [], now() + 24 * 3600))
  assert.ok(reason?.includes('future'), `motivo: ${reason}`)
})

await test('rechaza un evento ya caducado (NIP-40)', async () => {
  const reason = await rejection(relay, sign(1, 'caducado', [['expiration', String(now() - 60)]]))
  assert.ok(reason?.includes('expired'), `motivo: ${reason}`)
})

await test('rechaza una firma inválida', async () => {
  const bad = { ...sign(1, 'firma rota'), sig: '0'.repeat(128) }
  assert.ok(await rejection(relay, bad))
})

await test('el limit de una consulta se respeta por encima de 100', async () => {
  const author = generateSecretKey()
  for (let i = 0; i < 130; i++) {
    await relay.publish(finalizeEvent({ kind: 1, content: `n${i}`, tags: [], created_at: now() - i }, author))
  }
  const events = await query(relay, { authors: [getPublicKey(author)], kinds: [1], limit: 500 })
  assert.equal(events.length, 130)
})

await test('NIP-09: un kind 5 borra la nota propia', async () => {
  await relay.publish(sign(5, '', [['e', note.id]]))
  const after = await query(relay, { ids: [note.id] })
  assert.equal(after.length, 0)
})

relay.close()
console.log(`\n${passed} pruebas pasadas`)
