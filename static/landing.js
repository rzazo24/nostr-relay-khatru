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
    activity: 'Activity', s_events: 'Events stored', s_authors: 'Different authors', s_live: 'Connected now',
    s_accepted: 'Accepted, last 24 h', s_blocked: 'Blocked, last 24 h', ago24: '24 h ago', now: 'now',
    k_saved: 'stored', k_eph: 'ephemeral (relayed, not stored)',
    chart_label: 'Events per hour over the last 24 hours',
    chart_tip: (h, s, e) => `${h}: ${s} stored, ${e} ephemeral`,
    note: (up, since) => `Running for ${up}${since ? ` · oldest event: ${since}` : ''}. Only totals are shown: no keys, no addresses, no content.`,
    d: 'd', h: 'h', min: 'min', kindWord: 'kind',
    kinds: { 0: 'Profiles', 1: 'Notes', 3: 'Follow lists', 4: 'Old direct messages', 5: 'Deletions', 6: 'Reposts', 7: 'Reactions', 16: 'Generic reposts', 1984: 'Reports', 9735: 'Zaps', 10002: 'Relay lists', 30023: 'Articles', 30078: 'App data' },
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
    activity: 'Actividad', s_events: 'Eventos guardados', s_authors: 'Autores distintos', s_live: 'Conectados ahora',
    s_accepted: 'Aceptados, últimas 24 h', s_blocked: 'Bloqueados, últimas 24 h', ago24: 'hace 24 h', now: 'ahora',
    k_saved: 'guardados', k_eph: 'efímeros (reenviados, no se guardan)',
    chart_label: 'Eventos por hora en las últimas 24 horas',
    chart_tip: (h, s, e) => `${h}: ${s} guardados, ${e} efímeros`,
    note: (up, since) => `En marcha desde hace ${up}${since ? ` · evento más antiguo: ${since}` : ''}. Solo se enseñan totales: ni claves, ni direcciones, ni contenido.`,
    d: 'd', h: 'h', min: 'min', kindWord: 'tipo',
    kinds: { 0: 'Perfiles', 1: 'Notas', 3: 'Listas de seguidos', 4: 'Mensajes directos antiguos', 5: 'Borrados', 6: 'Reposts', 7: 'Reacciones', 16: 'Reposts genéricos', 1984: 'Denuncias', 9735: 'Zaps', 10002: 'Listas de relés', 30023: 'Artículos', 30078: 'Datos de apps' },
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

// El idioma elegido con los botones EN/ES se recuerda en este navegador (localStorage); sin elección, manda el idioma del navegador.
// Todo el acceso al almacenamiento va en try/catch: en modo privado o con el almacenamiento bloqueado la página funciona igual.
const LANG_KEY = 'landing-lang'
function storedLang() {
  try { const v = localStorage.getItem(LANG_KEY); return v === 'es' || v === 'en' ? v : null } catch { return null }
}
function rememberLang(l) {
  try { localStorage.setItem(LANG_KEY, l) } catch { /* no se puede recordar: no pasa nada */ }
}
let lang = storedLang() || ((navigator.language || 'en').toLowerCase().startsWith('es') ? 'es' : 'en')
let info = null
let status = 'checking'
let stats = null

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

  // una sola etiqueta «Contacto» con el npub del relé y lo que haya en el campo contact (aquí, la dirección Lightning)
  const parts = []
  const npub = info.pubkey ? toNpub(info.pubkey) : null
  if (npub) parts.push(`${npub.slice(0, 12)}…${npub.slice(-6)}`)
  if (info.contact) parts.push(info.contact)
  $('operator').textContent = parts.length ? `${t('contact')}: ${parts.join(' · ')}` : ''
  $('operator').title = npub || ''
  // "dev" es la versión de una compilación sin etiquetar: no aporta nada al visitante
  if (info.version && info.version !== 'dev') $('version').textContent = ' · ' + info.version
}

// --- Actividad: cifras agregadas de /stats.json (todo con textContent / atributos; nada de HTML) ---
const SVG = 'http://www.w3.org/2000/svg'
const compact = (n) => (n >= 100000 ? new Intl.NumberFormat(lang, { notation: 'compact', maximumFractionDigits: 1 }).format(n) : fmt(n))

function duration(seconds) {
  const m = Math.max(0, Math.floor(seconds / 60))
  const d = Math.floor(m / 1440), h = Math.floor((m % 1440) / 60)
  if (d > 0) return `${d} ${t('d')}${h ? ` ${h} ${t('h')}` : ''}`
  if (h > 0) return `${h} ${t('h')}`
  return `${m} ${t('min')}`
}

function renderChart(hours) {
  const svg = $('chart')
  svg.replaceChildren()
  svg.setAttribute('aria-label', t('chart_label'))
  const peak = Math.max(1, ...hours.map((h) => h.saved + h.ephemeral))
  const w = 240 / hours.length
  hours.forEach((h, i) => {
    const g = document.createElementNS(SVG, 'g')
    const title = document.createElementNS(SVG, 'title')
    title.textContent = t('chart_tip')(new Date(h.t * 1000).toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit' }), fmt(h.saved), fmt(h.ephemeral))
    g.append(title)
    // stored events are far fewer than ephemeral ones: give them a visible minimum so the base strip never disappears
    const sav = h.saved > 0 ? Math.max(3, (h.saved / peak) * 60) : 0
    const eph = Math.max(0, (h.ephemeral / peak) * 60 - (sav > (h.saved / peak) * 60 ? sav - (h.saved / peak) * 60 : 0))
    const bar = (cls, y, height) => {
      const r = document.createElementNS(SVG, 'rect')
      r.setAttribute('class', cls)
      r.setAttribute('x', String(i * w + 0.5))
      r.setAttribute('width', String(Math.max(0.5, w - 1)))
      r.setAttribute('y', String(y))
      r.setAttribute('height', String(Math.max(0, height)))
      g.append(r)
    }
    bar('bar-eph', 62 - eph - sav, eph)
    bar('bar-saved', 62 - sav, sav)
    // a thin baseline mark so that quiet hours are still visible as a column
    bar('bar-base', 62, 0.8)
    svg.append(g)
  })
}

function renderStats() {
  const sec = $('activity')
  if (!stats) { sec.hidden = true; return }
  sec.hidden = false
  const ev = stats.events, day = stats.last24h
  $('s-events').textContent = compact(ev.total)
  $('s-authors').textContent = compact(ev.authors)
  $('s-live').textContent = fmt(stats.connections)
  $('s-accepted').textContent = compact(day.saved + day.ephemeral)
  $('s-blocked').textContent = compact(day.rejected)
  renderChart(day.hours)
  const kinds = $('kinds')
  kinds.replaceChildren()
  for (const k of ev.byKind.slice(0, 6)) {
    const li = document.createElement('li')
    const a = document.createElement('span')
    a.className = 'pill'
    const b = document.createElement('b')
    b.textContent = fmt(k.count)
    a.append(b, document.createTextNode(` ${T[lang].kinds[k.kind] || `${t('kindWord')} ${k.kind}`}`))
    li.append(a)
    kinds.append(li)
  }
  const since = ev.oldest ? new Date(ev.oldest * 1000).toLocaleDateString(lang, { year: 'numeric', month: 'short', day: 'numeric' }) : ''
  $('s-note').textContent = t('note')(duration(stats.now - stats.startedAt), since)
}

function loadStats() {
  fetch('/stats.json')
    .then((r) => { if (!r.ok) throw new Error(String(r.status)); return r.json() })
    .then((s) => { stats = s; renderStats() })
    .catch(() => { /* sin cifras, la sección queda oculta: el resto de la página no depende de ella */ })
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

document.querySelectorAll('[data-lang]').forEach((b) => b.addEventListener('click', () => { lang = b.dataset.lang; rememberLang(lang); render(); renderStats() }))
$('copy').addEventListener('click', copyAddress)
render()
loadStats()
setInterval(() => { if (!document.hidden) loadStats() }, 60000) // las cifras se refrescan solas mientras la pestaña se ve

fetch('/', { headers: { Accept: 'application/nostr+json' } })
  .then((r) => { if (!r.ok) throw new Error(String(r.status)); return r.json() })
  .then((doc) => { info = doc; status = 'ok'; render() })
  .catch(() => { status = 'down'; render() })
