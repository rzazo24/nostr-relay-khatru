// Página de presentación del relé. No depende de nada externo: lee el documento NIP-11
// del propio relé (mismo origen) y pinta lo que trae. TODO lo que viene del documento se
// inserta con textContent (nunca como HTML), porque el nombre o la descripción pueden
// cambiarse en caliente con la API NIP-86.
'use strict'

const T = {
  en: {
    checking: 'Checking…', online: 'Online', offline: 'Could not read the relay information',
    address: 'Relay address', copy: 'Copy', copied: 'Copied to the clipboard', copyFailed: 'Select the address and copy it manually',
    howto: 'Add it to the relay list of your Nostr client (in most apps: Settings → Relays → add) and you can read and publish through it.',
    supports: 'What it supports', limits: 'Limits', rules: 'Rules and privacy',
    'rule-open': 'Open for reading and writing, no sign-up. Rate limits apply per IP; if it gets abused it will be tightened.',
    'rule-dm': 'Direct messages are only delivered to their sender and recipient after they authenticate (NIP-42).',
    'rule-log': 'The relay does not log message content or IP addresses.',
    'rule-public': 'Notes are public: anyone can read what you publish here, and other relays may hold copies.',
    source: 'Source code', operator: 'Operator', contact: 'Contact',
    l_content: 'Max content length', l_content_v: (n) => `${n} characters`,
    l_limit: 'Max events per query', l_limit_v: (n) => `${n}`,
    l_tags: 'Max tags per event', l_msg: 'Max message size', l_msg_v: (n) => `${n} KB`,
    l_pow: 'Proof of work (NIP-13)', l_pow_v: (n) => (n > 0 ? `${n} bits` : 'not required'),
    l_future: 'Events dated in the future', l_future_v: (n) => (n > 0 ? `up to ${Math.round(n / 60)} minutes ahead` : 'no limit'),
    l_auth: 'Authentication required', l_writes: 'Writes restricted to allowed pubkeys', yes: 'Yes', no: 'No',
    nip: {
      1: 'Basic protocol', 9: 'Event deletion', 11: 'Relay information', 13: 'Proof of work', 40: 'Expiration',
      42: 'Authentication', 45: 'Counting', 70: 'Protected events', 77: 'Negentropy sync', 86: 'Management API',
    },
  },
  es: {
    checking: 'Comprobando…', online: 'En línea', offline: 'No se pudo leer la información del relé',
    address: 'Dirección del relé', copy: 'Copiar', copied: 'Copiada al portapapeles', copyFailed: 'Selecciona la dirección y cópiala a mano',
    howto: 'Añádela a la lista de relés de tu cliente de Nostr (en la mayoría de apps: Ajustes → Relés → añadir) y podrás leer y publicar a través de él.',
    supports: 'Qué soporta', limits: 'Límites', rules: 'Reglas y privacidad',
    'rule-open': 'Abierto para leer y escribir, sin registro. Hay límites de velocidad por IP; si se abusa de él, se endurecerán.',
    'rule-dm': 'Los mensajes directos solo se entregan a quien los envía y a quien los recibe, tras autenticarse (NIP-42).',
    'rule-log': 'El relé no registra el contenido de los mensajes ni las direcciones IP.',
    'rule-public': 'Las notas son públicas: cualquiera puede leer lo que publiques aquí, y otros relés pueden tener copias.',
    source: 'Código fuente', operator: 'Responsable', contact: 'Contacto',
    l_content: 'Longitud máxima del contenido', l_content_v: (n) => `${n} caracteres`,
    l_limit: 'Máximo de eventos por consulta', l_limit_v: (n) => `${n}`,
    l_tags: 'Máximo de tags por evento', l_msg: 'Tamaño máximo de mensaje', l_msg_v: (n) => `${n} KB`,
    l_pow: 'Prueba de trabajo (NIP-13)', l_pow_v: (n) => (n > 0 ? `${n} bits` : 'no se exige'),
    l_future: 'Eventos con fecha futura', l_future_v: (n) => (n > 0 ? `hasta ${Math.round(n / 60)} minutos por delante` : 'sin límite'),
    l_auth: 'Autenticación obligatoria', l_writes: 'Escritura restringida a pubkeys permitidos', yes: 'Sí', no: 'No',
    nip: {
      1: 'Protocolo básico', 9: 'Borrado de eventos', 11: 'Información del relé', 13: 'Prueba de trabajo', 40: 'Caducidad',
      42: 'Autenticación', 45: 'Conteo', 70: 'Eventos protegidos', 77: 'Sincronización Negentropy', 86: 'API de gestión',
    },
  },
}

let lang = (navigator.language || 'en').toLowerCase().startsWith('es') ? 'es' : 'en'
let info = null
let status = 'checking'

const $ = (id) => document.getElementById(id)
const t = (key) => T[lang][key]

// --- npub (bech32) para mostrar la clave del responsable de forma legible ---
const CHARSET = 'qpzry9x8gf2tvdw0s3jn54khce6mua7l'
function polymod(values) {
  const G = [0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3]
  let chk = 1
  for (const v of values) {
    const top = chk >>> 25
    chk = ((chk & 0x1ffffff) << 5) ^ v
    for (let i = 0; i < 5; i++) if ((top >>> i) & 1) chk ^= G[i]
  }
  return chk >>> 0
}
function toNpub(hex) {
  if (!/^[0-9a-f]{64}$/i.test(hex)) return null
  const bytes = hex.match(/../g).map((h) => parseInt(h, 16))
  const data = []
  let acc = 0, bits = 0
  for (const b of bytes) {
    acc = (acc << 8) | b
    bits += 8
    while (bits >= 5) { bits -= 5; data.push((acc >> bits) & 31) }
  }
  if (bits > 0) data.push((acc << (5 - bits)) & 31)
  const hrp = 'npub'
  const hrpExp = [...hrp].map((c) => c.charCodeAt(0) >> 5).concat([0], [...hrp].map((c) => c.charCodeAt(0) & 31))
  const mod = polymod(hrpExp.concat(data, [0, 0, 0, 0, 0, 0])) ^ 1
  const checksum = [0, 1, 2, 3, 4, 5].map((i) => (mod >>> (5 * (5 - i))) & 31)
  return hrp + '1' + data.concat(checksum).map((d) => CHARSET[d]).join('')
}

function safeImageUrl(value) {
  if (typeof value !== 'string') return null
  if (value.startsWith('/') && !value.startsWith('//')) return value
  try {
    const u = new URL(value)
    return u.protocol === 'https:' ? u.href : null
  } catch { return null }
}

// La descripción de NIP-11 es un solo texto. Para que sea bilingüe en TODOS los clientes se
// escribe "texto en inglés | texto en español": los clientes muestran ambos, y esta página
// enseña solo el del idioma elegido. Si no tiene ese formato, se muestra tal cual.
function pickLanguage(text) {
  const parts = String(text).split(' | ')
  return parts.length === 2 ? parts[lang === 'es' ? 1 : 0].trim() : text
}

function fmt(n) { return Number(n).toLocaleString(lang) }

function addRow(dl, label, value) {
  const dt = document.createElement('dt')
  dt.textContent = label
  const dd = document.createElement('dd')
  dd.textContent = value
  dl.append(dt, dd)
}

function render() {
  document.documentElement.lang = lang
  document.querySelectorAll('[data-i18n]').forEach((el) => {
    const v = t(el.dataset.i18n)
    if (typeof v === 'string') el.textContent = v
  })
  document.querySelectorAll('[data-lang]').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.lang === lang)))

  $('address').textContent = `wss://${location.host}`
  const statusEl = $('status')
  statusEl.className = 'status' + (status === 'ok' ? ' ok' : status === 'down' ? ' down' : '')
  $('status-text').textContent = status === 'ok' ? t('online') : status === 'down' ? t('offline') : t('checking')

  const nips = $('nips')
  const limits = $('limits')
  nips.replaceChildren()
  limits.replaceChildren()
  $('operator').textContent = ''
  $('version').textContent = ''
  if (!info) return

  document.title = info.name || 'Nostr relay'
  $('name').textContent = info.name || location.host
  $('description').textContent = pickLanguage(info.description || '')
  const icon = safeImageUrl(info.icon)
  if (icon) $('icon').src = icon

  const numbers = (info.supported_nips || []).filter((n) => Number.isInteger(n)).sort((a, b) => a - b)
  for (const n of numbers) {
    const li = document.createElement('li')
    const a = document.createElement('a')
    a.href = `https://nips.nostr.com/${n}`
    a.rel = 'noopener noreferrer'
    const b = document.createElement('b')
    b.textContent = `NIP-${n}`
    a.append(b, document.createTextNode(T[lang].nip[n] ? ` · ${T[lang].nip[n]}` : ''))
    li.append(a)
    nips.append(li)
  }

  const lim = info.limitation || {}
  if (lim.max_content_length) addRow(limits, t('l_content'), t('l_content_v')(fmt(lim.max_content_length)))
  if (lim.max_limit) addRow(limits, t('l_limit'), t('l_limit_v')(fmt(lim.max_limit)))
  if (lim.max_event_tags) addRow(limits, t('l_tags'), fmt(lim.max_event_tags))
  if (lim.max_message_length) addRow(limits, t('l_msg'), t('l_msg_v')(fmt(Math.round(lim.max_message_length / 1000))))
  addRow(limits, t('l_pow'), t('l_pow_v')(lim.min_pow_difficulty || 0))
  if ('created_at_upper_limit' in lim) addRow(limits, t('l_future'), t('l_future_v')(lim.created_at_upper_limit || 0))
  addRow(limits, t('l_auth'), lim.auth_required ? t('yes') : t('no'))
  addRow(limits, t('l_writes'), lim.restricted_writes ? t('yes') : t('no'))

  const parts = []
  const npub = info.pubkey ? toNpub(info.pubkey) : null
  if (npub) parts.push(`${t('operator')}: ${npub.slice(0, 12)}…${npub.slice(-6)}`)
  if (info.contact) parts.push(`${t('contact')}: ${info.contact}`)
  $('operator').textContent = parts.join(' · ')
  // "dev" es la versión de una compilación sin etiquetar: no aporta nada al visitante
  if (info.version && info.version !== 'dev') $('version').textContent = ' · ' + info.version
}

async function copyAddress() {
  const text = $('address').textContent
  let ok = false
  try {
    await navigator.clipboard.writeText(text)
    ok = true
  } catch {
    try {
      const range = document.createRange()
      range.selectNodeContents($('address'))
      const sel = getSelection()
      sel.removeAllRanges()
      sel.addRange(range)
      ok = document.execCommand('copy')
    } catch { ok = false }
  }
  $('copied').textContent = ok ? t('copied') : t('copyFailed')
  setTimeout(() => { $('copied').textContent = '' }, 2500)
}

document.querySelectorAll('[data-lang]').forEach((b) => b.addEventListener('click', () => { lang = b.dataset.lang; render() }))
$('copy').addEventListener('click', copyAddress)
render()

fetch('/', { headers: { Accept: 'application/nostr+json' } })
  .then((r) => { if (!r.ok) throw new Error(String(r.status)); return r.json() })
  .then((doc) => { info = doc; status = 'ok'; render() })
  .catch(() => { status = 'down'; render() })
