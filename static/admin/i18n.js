// Idiomas del panel (español, que es el original, e inglés). Se carga antes de admin.js y deja `window.I18N`.
//
// Cómo funciona:
//  · El HTML y el JavaScript están escritos en español. El inglés vive en `i18n.en.js` (el diccionario).
//  · Textos fijos del HTML: se traducen solos al cambiar de idioma. Cada «unidad» (un elemento con texto e inline
//    como <b> o <code>, o un trozo suelto de texto, o un atributo title/aria-label/placeholder) se busca en el
//    diccionario por un hash de su texto original (`EN_HTML`), sin tener que marcar nada en el HTML.
//  · Textos que escribe el JavaScript: pasan por `t('texto en español', {variables})`, que busca la frase en `EN_UI`.
//  · Textos que manda el servidor ya en español (resultados de búsqueda, historial): `tx()` los traduce por patrones.
//  · Lo que no tenga traducción se muestra en español y se apunta en `I18N.missing` (una prueba lo vigila).
// La opción elegida se recuerda en este navegador; por defecto manda el idioma del navegador (como la página principal).
'use strict'

;(function () {
  const KEY = 'panel-idioma'
  const EN_HTML = (window.PANEL_EN && window.PANEL_EN.html) || {}
  const EN_UI = (window.PANEL_EN && window.PANEL_EN.ui) || {}
  const missing = new Set()

  const stored = () => { try { return localStorage.getItem(KEY) } catch { return null } }
  let lang = stored() || ((navigator.language || 'en').toLowerCase().startsWith('es') ? 'es' : 'en')
  if (lang !== 'es' && lang !== 'en') lang = 'en'

  // Hash corto y estable (FNV-1a de 32 bits) del texto original: es la clave del diccionario de textos fijos.
  function hash(s) {
    let h = 0x811c9dc5
    for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 0x01000193) >>> 0 }
    return h.toString(16).padStart(8, '0')
  }

  const fill = (s, vars) => (vars ? s.replace(/\{(\w+)\}/g, (_, k) => (k in vars ? String(vars[k]) : `{${k}}`)) : s)

  /** Traduce una frase del JavaScript. `es` es el original; `vars` rellena {huecos}. */
  function t(es, vars) {
    if (lang === 'es') return fill(es, vars)
    if (es in EN_UI) return fill(EN_UI[es], vars)
    missing.add(es)
    return fill(es, vars)
  }

  // ---- textos fijos del HTML ----
  const INLINE = new Set(['B', 'I', 'CODE', 'A', 'SPAN', 'EM', 'STRONG', 'BR', 'KBD', 'SMALL'])
  const SKIP_TAGS = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT', 'SVG', 'TEXTAREA'])
  const BLOCKY = 'button, input, select, textarea, table, ul, ol, dl, p, div, section, label, details, summary, li, dd, dt, h1, h2, h3, header, nav'
  const ATTRS = ['title', 'aria-label', 'placeholder']
  const units = new WeakMap() // elemento o nodo de texto -> { es, en } (lo que pusimos al traducir)
  const attrUnits = new WeakMap() // elemento -> { atributo: { es, en } }

  const hasDirectText = (el) => [...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim())
  const isWholeUnit = (el) => hasDirectText(el) && [...el.children].every((k) => INLINE.has(k.tagName) && !k.querySelector(BLOCKY))

  function eachUnit(root, fn) {
    const walk = (el) => {
      if (SKIP_TAGS.has(el.tagName) || el.hasAttribute('data-i18n-skip')) return
      for (const a of ATTRS) if (el.hasAttribute(a)) fn({ kind: 'attr', el, attr: a })
      if (isWholeUnit(el)) { fn({ kind: 'html', el }); return }
      for (const n of el.childNodes) {
        if (n.nodeType === 3 && n.textContent.trim()) fn({ kind: 'text', node: n })
        else if (n.nodeType === 1) walk(n)
      }
    }
    if (root.nodeType === 1) walk(root)
    else for (const c of root.children) walk(c)
  }

  // Lee el contenido actual de una unidad y el registro de lo que pusimos nosotros (si ya la habíamos traducido).
  const current = (u) => (u.kind === 'html' ? u.el.innerHTML.trim() : u.kind === 'text' ? u.node.nodeValue : u.el.getAttribute(u.attr))
  const record = (u) => (u.kind === 'html' ? units.get(u.el) : u.kind === 'text' ? units.get(u.node) : attrUnits.get(u.el)?.[u.attr])
  function write(u, value) {
    if (u.kind === 'html') u.el.innerHTML = value
    else if (u.kind === 'text') u.node.nodeValue = value
    else u.el.setAttribute(u.attr, value)
  }
  function remember(u, rec) {
    if (u.kind === 'html') units.set(u.el, rec)
    else if (u.kind === 'text') units.set(u.node, rec)
    else { const all = attrUnits.get(u.el) || {}; all[u.attr] = rec; attrUnits.set(u.el, all) }
  }
  /** El texto en español de una unidad (null si el JavaScript la ha cambiado desde que la traducimos: entonces no se toca). */
  function original(u) {
    const cur = current(u), rec = record(u)
    if (!rec) return cur
    return cur === rec.en || cur === rec.es ? rec.es : null
  }

  function setUnit(u, to) { // to: 'es' | 'en'
    const es = original(u)
    if (es === null) return
    const key = es.trim()
    let out = es
    if (to === 'en') {
      const en = EN_HTML[hash(key)]
      if (en === undefined) missing.add(`${u.kind}:${hash(key)} ${key.slice(0, 80)}`) // la clave del diccionario y el comienzo del texto
      else out = u.kind === 'text' ? es.replace(key, en) : en
    }
    if (current(u) !== out) write(u, out)
    remember(u, { es, en: out })
  }

  /** Traduce (o devuelve al español) todos los textos fijos bajo `root`. */
  function apply(root = document.body) {
    eachUnit(root, (u) => setUnit(u, lang))
    document.documentElement.lang = lang
    document.title = t('Panel del relé')
    document.querySelectorAll('[data-lang]').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.lang === lang)))
  }

  /** Para el desarrollo y las pruebas: los textos fijos del HTML con su clave, tal como los ve el traductor. */
  function collect(root = document.body) {
    const out = []
    eachUnit(root, (u) => {
      const es = original(u)
      if (es !== null) out.push({ key: hash(es.trim()), kind: u.kind, es: es.trim() })
    })
    return out
  }

  const listeners = []
  function setLang(next) {
    if (next !== 'es' && next !== 'en') return
    lang = next
    try { localStorage.setItem(KEY, next) } catch { /* sin almacenamiento: no se recuerda */ }
    apply()
    listeners.forEach((f) => f(next))
  }

  // ---- textos que manda el servidor en español ----
  // Cada regla: patrón en español -> función que da la frase en inglés.
  const FIELD = { nombre: 'name', 'descripción': 'description', icono: 'icon', contacto: 'contact', etiquetas: 'tags', idiomas: 'languages', 'normas de uso': 'posting policy' }
  const fields = (list) => list.split(', ').map((f) => FIELD[f] || f).join(', ')
  const RULES = [
    [/^todos los eventos$/, () => 'all events'],
    [/^eventos de la clave (.+)$/, (m) => `events of key ${m[1]}`],
    [/^el evento (.+)$/, (m) => `the event ${m[1]}`],
    [/^la clave o el id (.+)$/, (m) => `the key or id ${m[1]}`],
    [/^claves o ids que empiezan por (.+)$/, (m) => `keys or ids starting with ${m[1]}`],
    [/^eventos de tipo (\d+)$/, (m) => `events of kind ${m[1]}`],
    [/^eventos cuyo contenido contiene «(.*)»$/, (m) => `events whose content contains “${m[1]}”`],
    [/^(.*) \(tipo (\d+)\)$/, (m) => `${tx(m[1])} (kind ${m[2]})`],
    [/^cambiados: (.+)$/, (m) => `changed: ${fields(m[1])}`],
    [/^restaurados: (.+)$/, (m) => `restored: ${fields(m[1])}`],
    [/^(.*) \(borrados (\d+) eventos\)$/, (m) => `${m[1] ? m[1] + ' ' : ''}(deleted ${m[2]} events)`.trim()],
    [/^(\S+\.sqlite\.gz) \(base de datos de ([\d.,]+) MB sin comprimir\)$/, (m) => `${m[1]} (database of ${m[2]} MB uncompressed)`],
  ]
  /** Traduce una cadena que el servidor genera en español (o la devuelve igual si no la reconoce). */
  function tx(s) {
    if (lang === 'es' || typeof s !== 'string' || !s) return s
    return s.split(' · ').map((part) => {
      for (const [re, f] of RULES) { const m = re.exec(part); if (m) return f(m) }
      return part
    }).join(' · ')
  }

  window.I18N = { t, tx, apply, collect, setLang, hash, get lang() { return lang }, missing, onChange: (f) => listeners.push(f) }
})()
