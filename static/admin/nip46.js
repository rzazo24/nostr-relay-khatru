// Inicio de sesión con un firmador remoto (NIP-46), por ejemplo Clave en el iPhone. Se carga solo cuando eliges ese modo.
//
// Flujo "nostrconnect": el panel crea una clave temporal y un enlace nostrconnect://; el firmador lo abre, pide tu
// aprobación y contesta por el relé con un mensaje cifrado (NIP-44, kind 24133). Después el panel le pide firmar el
// mismo evento de inicio de sesión (NIP-98) que firmaría una extensión. La clave del dueño nunca sale del firmador.
//
// El intercambio pasa por ESTE relé. En el móvil la página se suspende mientras apruebas en la otra app, así que la
// conexión se retoma sola al volver (el relé guarda unos minutos los mensajes 24133 para entregárselos entonces).
import { generateSecretKey, getPublicKey, finalizeEvent, verifyEvent, nip44, bytesToHex } from './vendor/nostr.js'

const KIND = 24133
const randomHex = (n) => bytesToHex(crypto.getRandomValues(new Uint8Array(n)))

export function createSession({ name = 'Panel de control del relé', permissions = 'sign_event:27235', onAuthUrl = () => {}, onStatus = () => {} } = {}) {
  const clientSk = generateSecretKey()
  const clientPk = getPublicKey(clientSk)
  const secret = randomHex(16)
  const relayUrl = `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/`
  const startedAt = Math.floor(Date.now() / 1000)

  const params = new URLSearchParams()
  params.append('relay', relayUrl)
  params.set('secret', secret)
  params.set('name', name)
  params.set('url', location.origin)
  params.set('callback', `${location.origin}/admin/`)
  params.set('perms', permissions)
  const uri = `nostrconnect://${clientPk}?${params.toString()}`

  let ws = null
  let closed = false
  let retry = 0
  let timer = null
  let signerPk = null
  const seen = new Set()
  const pending = new Map() // id de la petición -> { resolve, reject, timeout }
  const outbox = [] // eventos aún sin enviar (el socket no estaba abierto)
  let ready // promesa de «el firmador ha aceptado la conexión»
  let resolveReady, rejectReady
  ready = new Promise((res, rej) => { resolveReady = res; rejectReady = rej })
  ready.catch(() => {}) // que un rechazo sin nadie esperando no salte como error

  const send = (msg) => ws.send(JSON.stringify(msg))

  function subscribe() {
    // `since` con margen: tras una suspensión se piden de nuevo los mensajes recientes (el relé los guarda unos minutos).
    send(['REQ', 'n46', { kinds: [KIND], '#p': [clientPk], since: startedAt - 30 }])
  }

  function connect() {
    if (closed || (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING))) return
    clearTimeout(timer)
    onStatus('Conectando con el relé…')
    try { ws = new WebSocket(relayUrl) } catch (err) { onStatus(`No se pudo abrir la conexión con el relé: ${err.message}`); timer = setTimeout(connect, 3000); return }
    const mine = ws
    ws.addEventListener('open', () => {
      if (mine !== ws) return
      onStatus('Relé conectado ✓')
      retry = 0
      subscribe()
      while (outbox.length) send(['EVENT', outbox.shift()])
    })
    ws.addEventListener('message', (m) => { if (mine === ws) onMessage(m.data) })
    ws.addEventListener('close', () => {
      if (mine !== ws || closed) return
      onStatus('Conexión con el relé perdida; reintentando…')
      timer = setTimeout(connect, Math.min(1000 * 2 ** retry++, 5000))
    })
    ws.addEventListener('error', () => {}) // el 'close' que sigue reintenta
  }

  function onMessage(data) {
    let msg
    try { msg = JSON.parse(data) } catch { return }
    if (msg[0] === 'OK' && msg[2] === true) onStatus('Mensaje enviado al relé ✓')
    if (msg[0] === 'OK' && msg[2] === false) { // el relé rechazó lo que enviamos (límite de velocidad, etc.)
      for (const p of pending.values()) p.reject(new Error(`el relé rechazó el mensaje: ${msg[3] || 'sin motivo'}`))
      pending.clear()
      rejectReady(new Error(`el relé rechazó el mensaje: ${msg[3] || 'sin motivo'}`))
      return
    }
    if (msg[0] !== 'EVENT' || !msg[2]) return
    const ev = msg[2]
    if (seen.has(ev.id) || ev.kind !== KIND || !verifyEvent(ev)) return
    seen.add(ev.id)
    let body
    try {
      body = JSON.parse(nip44.decrypt(ev.content, nip44.getConversationKey(clientSk, ev.pubkey)))
    } catch { onStatus('Ha llegado un mensaje al panel que no se ha podido descifrar'); return } // no era para nosotros o está mal cifrado
    if (!signerPk) {
      // La primera respuesta válida es la aceptación de la conexión: debe devolver el secreto que pusimos en el enlace.
      if (body.result === secret) { signerPk = ev.pubkey; onStatus('Firmador emparejado ✓'); resolveReady(signerPk) }
      else onStatus('Ha llegado una respuesta del firmador, pero no con el secreto esperado')
      return
    }
    if (ev.pubkey !== signerPk) return // después de emparejar, solo se hace caso a ese firmador
    const p = pending.get(body.id)
    if (!p) return
    if (body.result === 'auth_url' && body.error) { onAuthUrl(body.error); return } // sigue esperando la aprobación
    pending.delete(body.id)
    clearTimeout(p.timeout)
    if (body.error) p.reject(new Error(body.error))
    else p.resolve(body.result)
  }

  // Al volver a la pestaña o recuperar la red, se reconecta sin esperar.
  const wake = () => { if (!closed && (!ws || ws.readyState !== WebSocket.OPEN)) { retry = 0; connect() } }
  const onVisible = () => { if (document.visibilityState === 'visible') wake() }
  const onViolation = (e) => onStatus(`El navegador ha bloqueado algo por la política de seguridad: ${e.violatedDirective} (${e.blockedURI || 'sin dirección'})`)
  document.addEventListener('securitypolicyviolation', onViolation)
  document.addEventListener('visibilitychange', onVisible)
  window.addEventListener('online', wake)
  window.addEventListener('pageshow', wake)

  connect()

  return {
    uri,
    relayUrl,
    /** Se resuelve con la clave del firmador cuando acepta la conexión. */
    waitForSigner(ms = 180000) {
      return Promise.race([ready, new Promise((_, rej) => setTimeout(() => rej(new Error('no se ha aprobado la conexión a tiempo')), ms))])
    },
    /** Pide algo al firmador y espera su respuesta (el usuario puede tardar en aprobar). */
    request(method, args = [], ms = 120000) {
      if (!signerPk) return Promise.reject(new Error('el firmador aún no se ha conectado'))
      const id = randomHex(8)
      const content = nip44.encrypt(JSON.stringify({ id, method, params: args }), nip44.getConversationKey(clientSk, signerPk))
      const ev = finalizeEvent({ kind: KIND, created_at: Math.floor(Date.now() / 1000), tags: [['p', signerPk]], content }, clientSk)
      return new Promise((resolve, reject) => {
        const timeout = setTimeout(() => { pending.delete(id); reject(new Error('el firmador no ha contestado a tiempo')) }, ms)
        pending.set(id, { resolve, reject, timeout })
        if (ws && ws.readyState === WebSocket.OPEN) send(['EVENT', ev])
        else { outbox.push(ev); connect() }
      })
    },
    close() {
      closed = true
      clearTimeout(timer)
      document.removeEventListener('securitypolicyviolation', onViolation)
      document.removeEventListener('visibilitychange', onVisible)
      window.removeEventListener('online', wake)
      window.removeEventListener('pageshow', wake)
      for (const p of pending.values()) { clearTimeout(p.timeout); p.reject(new Error('cancelado')) }
      pending.clear()
      rejectReady(new Error('cancelado'))
      try { ws && ws.close() } catch { /* ya cerrado */ }
    },
  }
}
