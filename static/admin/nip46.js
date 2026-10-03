// Inicio de sesión con un firmador remoto (NIP-46), por ejemplo Clave en el iPhone. Se carga solo cuando eliges ese modo.
//
// Flujo "nostrconnect": el panel crea una clave temporal y un enlace nostrconnect://; el firmador lo abre, pide tu
// aprobación y contesta por el relé con un mensaje cifrado (NIP-44, kind 24133). Después el panel le pide firmar el
// mismo evento de inicio de sesión (NIP-98) que firmaría una extensión. La clave del dueño nunca sale del firmador.
//
// Relés: siempre ESTE relé (sin barra final) y, si la página lo indica (data-extra-relays), los relés extra que el
// firmador necesita. Clave, por ejemplo, solo recibe en segundo plano lo que pasa por relay.powr.build. El panel
// escucha y escribe en todos a la vez y se queda con lo primero que llegue.
// En el móvil la página se suspende mientras apruebas en la otra app: la conexión se retoma sola al volver, y el relé
// guarda unos minutos los mensajes 24133 para entregárselos entonces.
import { generateSecretKey, getPublicKey, finalizeEvent, verifyEvent, nip44, bytesToHex } from './vendor/nostr.js'

const t = (es, vars) => window.I18N.t(es, vars) // traduce un texto del panel (ver i18n.js)
const KIND = 24133
const randomHex = (n) => bytesToHex(crypto.getRandomValues(new Uint8Array(n)))
const noSlash = (u) => u.replace(/\/+$/, '')

export function createSession({ name = 'Panel de control del relé', permissions = 'sign_event:27235', extraRelays = [], onAuthUrl = () => {}, onStatus = () => {} } = {}) {
  const clientSk = generateSecretKey()
  const clientPk = getPublicKey(clientSk)
  const secret = randomHex(16)
  const ownRelay = noSlash(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}`)
  const relays = [...new Set([ownRelay, ...extraRelays.map(noSlash)])]
  const startedAt = Math.floor(Date.now() / 1000)

  const params = new URLSearchParams()
  for (const r of relays) params.append('relay', r)
  params.set('secret', secret)
  params.set('name', name)
  params.set('url', location.origin)
  params.set('callback', `${location.origin}/admin/`)
  params.set('perms', permissions)
  const uri = `nostrconnect://${clientPk}?${params.toString()}`
  // Enlace que abre la app Clave en iOS: el enlace universal de Clave (si la app está instalada, iOS la abre; si no, lleva a
  // su página). El `uri` va codificado dentro. (Su esquema propio `clave://connect?uri=` es la alternativa, no se usa.)
  const claveLink = `https://clave.casa/connect/?uri=${encodeURIComponent(uri)}`

  let closed = false
  let signerPk = null
  const seen = new Set()
  const pending = new Map() // id de la petición -> { resolve, reject, timeout }
  let resolveReady, rejectReady
  const ready = new Promise((res, rej) => { resolveReady = res; rejectReady = rej })
  ready.catch(() => {}) // que un rechazo sin nadie esperando no salte como error

  // ---- una conexión por relé, cada una con su propia reconexión ----
  const conns = relays.map((url) => ({ url, ws: null, retry: 0, timer: null, outbox: [], label: url.replace(/^wss?:\/\//, '') }))

  function connect(c) {
    if (closed || (c.ws && (c.ws.readyState === WebSocket.OPEN || c.ws.readyState === WebSocket.CONNECTING))) return
    clearTimeout(c.timer)
    onStatus(t('Conectando con {r}…', { r: c.label }))
    let ws
    try { ws = new WebSocket(c.url) } catch (err) {
      onStatus(t('No se pudo abrir la conexión con {r}: {e}', { r: c.label, e: err.message }))
      c.timer = setTimeout(() => connect(c), 3000)
      return
    }
    c.ws = ws
    ws.addEventListener('open', () => {
      if (c.ws !== ws) return
      onStatus(t('{r} conectado ✓', { r: c.label }))
      c.retry = 0
      // `since` con margen: tras una suspensión se piden de nuevo los mensajes recientes.
      ws.send(JSON.stringify(['REQ', 'n46', { kinds: [KIND], '#p': [clientPk], since: startedAt - 30 }]))
      while (c.outbox.length) ws.send(JSON.stringify(['EVENT', c.outbox.shift()]))
    })
    ws.addEventListener('message', (m) => { if (c.ws === ws) onMessage(c, m.data) })
    ws.addEventListener('close', () => {
      if (c.ws !== ws || closed) return
      onStatus(t('Conexión con {r} perdida; reintentando…', { r: c.label }))
      c.timer = setTimeout(() => connect(c), Math.min(1000 * 2 ** c.retry++, 5000))
    })
    ws.addEventListener('error', () => {}) // el 'close' que sigue reintenta
  }

  function onMessage(c, data) {
    let msg
    try { msg = JSON.parse(data) } catch { return }
    if (msg[0] === 'OK' && msg[2] === true) onStatus(t('Mensaje enviado a {r} ✓', { r: c.label }))
    if (msg[0] === 'OK' && msg[2] === false) {
      // Un relé rechazó lo que enviamos (límite de velocidad, etc.). Solo es grave si todos fallan, así que se avisa y se sigue.
      onStatus(t('{r} rechazó el mensaje: {m}', { r: c.label, m: msg[3] || t('sin motivo') }))
      return
    }
    if (msg[0] !== 'EVENT' || !msg[2]) return
    const ev = msg[2]
    if (seen.has(ev.id) || ev.kind !== KIND || !verifyEvent(ev)) return
    seen.add(ev.id)
    let body
    try {
      body = JSON.parse(nip44.decrypt(ev.content, nip44.getConversationKey(clientSk, ev.pubkey)))
    } catch { onStatus(t('Ha llegado un mensaje al panel que no se ha podido descifrar')); return } // no era para nosotros o está mal cifrado
    if (!signerPk) {
      // La primera respuesta válida es la aceptación de la conexión: debe devolver el secreto que pusimos en el enlace.
      if (body.result === secret) { signerPk = ev.pubkey; onStatus(t('Firmador emparejado ✓ (por {r})', { r: c.label })); resolveReady(signerPk) }
      else onStatus(t('Ha llegado una respuesta del firmador, pero no con el secreto esperado'))
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
  const wake = () => { if (!closed) for (const c of conns) if (!c.ws || c.ws.readyState !== WebSocket.OPEN) { c.retry = 0; connect(c) } }
  const onVisible = () => { if (document.visibilityState === 'visible') wake() }
  const onViolation = (e) => onStatus(t('El navegador ha bloqueado algo por la política de seguridad: {d} ({u})', { d: e.violatedDirective, u: e.blockedURI || t('sin dirección') }))
  document.addEventListener('securitypolicyviolation', onViolation)
  document.addEventListener('visibilitychange', onVisible)
  window.addEventListener('online', wake)
  window.addEventListener('pageshow', wake)

  conns.forEach(connect)

  return {
    uri,
    claveLink,
    relays,
    /** Se resuelve con la clave del firmador cuando acepta la conexión. */
    waitForSigner(ms = 180000) {
      return Promise.race([ready, new Promise((_, rej) => setTimeout(() => rej(new Error(t('no se ha aprobado la conexión a tiempo'))), ms))])
    },
    /** Pide algo al firmador y espera su respuesta (el usuario puede tardar en aprobar). Se envía por todos los relés. */
    request(method, args = [], ms = 120000) {
      if (!signerPk) return Promise.reject(new Error(t('el firmador aún no se ha conectado')))
      const id = randomHex(8)
      const content = nip44.encrypt(JSON.stringify({ id, method, params: args }), nip44.getConversationKey(clientSk, signerPk))
      const ev = finalizeEvent({ kind: KIND, created_at: Math.floor(Date.now() / 1000), tags: [['p', signerPk]], content }, clientSk)
      return new Promise((resolve, reject) => {
        const timeout = setTimeout(() => { pending.delete(id); reject(new Error(t('el firmador no ha contestado a tiempo'))) }, ms)
        pending.set(id, { resolve, reject, timeout })
        for (const c of conns) {
          if (c.ws && c.ws.readyState === WebSocket.OPEN) c.ws.send(JSON.stringify(['EVENT', ev]))
          else { c.outbox.push(ev); connect(c) }
        }
      })
    },
    close() {
      closed = true
      document.removeEventListener('securitypolicyviolation', onViolation)
      document.removeEventListener('visibilitychange', onVisible)
      window.removeEventListener('online', wake)
      window.removeEventListener('pageshow', wake)
      for (const p of pending.values()) { clearTimeout(p.timeout); p.reject(new Error(t('cancelado'))) }
      pending.clear()
      rejectReady(new Error(t('cancelado')))
      for (const c of conns) { clearTimeout(c.timer); try { c.ws && c.ws.close() } catch { /* ya cerrado */ } }
    },
  }
}
