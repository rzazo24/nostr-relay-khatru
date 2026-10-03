// Pruebas de extremo a extremo del panel de control (/admin): un relé real (RELAY_BIN) y un Chromium real
// (Playwright), con la extensión NIP-07 (nos2x) simulada. Ejecutar: RELAY_BIN=../../nostr-relay-khatru npm test
// Cubre: entrada y sesión, resumen, ayuda, histórico, moderación, búsqueda y que la política de contenido
// estricta del panel no bloquee nada (ni una sola violación en la consola).
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import fs from 'node:fs'
import zlib from 'node:zlib'
import { execFileSync } from 'node:child_process'
import { after, before, describe, it } from 'node:test'
import { chromium, devices } from 'playwright'
import WebSocket from 'ws'
import { Relay, useWebSocketImplementation } from 'nostr-tools/relay'
import { finalizeEvent, generateSecretKey, getPublicKey } from 'nostr-tools/pure'
import * as nip19 from 'nostr-tools/nip19'
import * as nip44 from 'nostr-tools/nip44'
import { startStack } from './harness.mjs'

useWebSocketImplementation(WebSocket)

let stack, browser, relay
const consoleErrors = []
const now = () => Math.floor(Date.now() / 1000)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

/** Publica un evento firmado; devuelve 'ok' o el motivo del rechazo. */
async function pub(sk, kind, content, tags = [], ago = 0) {
  try {
    await relay.publish(finalizeEvent({ kind, created_at: now() - ago, tags, content }, sk))
    return 'ok'
  } catch (e) {
    return `rechazado: ${e.message}`
  }
}

/** Abre el panel en un contexto nuevo. `extension: false` simula un navegador sin nos2x. */
async function openPanel({ signWith = () => stack.ownerSecret, extension = true, locale = 'es-ES' } = {}) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 1000 }, locale, timezoneId: 'Europe/Madrid' })
  if (extension) {
    await ctx.exposeFunction('__sign', (ev) => finalizeEvent({ ...ev, tags: [...ev.tags, ['nonce', String(Math.random())]] }, signWith()))
    await ctx.addInitScript(() => { window.nostr = { signEvent: (ev) => window.__sign(ev) } })
  }
  const page = await ctx.newPage()
  page.on('dialog', (d) => d.accept())
  page.on('pageerror', (e) => consoleErrors.push(`pageerror: ${e.message}`))
  page.on('console', (m) => {
    // Los 4xx de las acciones que fallan a propósito (o la sesión aún sin abrir) salen como errores de red: no cuentan.
    if (m.type() === 'error' && !/Failed to load resource: the server responded with a status of 4\d\d/.test(m.text())) consoleErrors.push(m.text())
  })
  await page.goto(stack.panelUrl)
  return { ctx, page }
}

// El relé limita los inicios de sesión a 10 por minuto y por IP: las pruebas hacen uno de verdad (`fresh`) y las demás
// reutilizan esa sesión metiendo su cookie en el contexto nuevo.
let sharedCookie = null
async function login(page, { fresh = false } = {}) {
  if (!fresh && sharedCookie) {
    await page.context().addCookies([sharedCookie])
    await page.reload()
    await page.waitForSelector('#dash:not([hidden])', { timeout: 10000 })
    return
  }
  await page.click('#login-btn')
  try {
    await page.waitForSelector('#dash:not([hidden])', { timeout: 10000 })
  } catch (e) {
    throw new Error(`no se pudo entrar: «${await page.innerText('#login-msg')}»`)
  }
  if (!fresh) sharedCookie = (await page.context().cookies()).find((c) => c.httpOnly) ?? null
}

/** Espera un aviso NUEVO del panel (se borra el anterior antes de la acción). */
async function clearToast(page) {
  await page.evaluate(() => { const t = document.getElementById('toast'); t.textContent = ''; t.className = 'toast' })
}
async function toast(page) {
  await page.waitForFunction(() => document.getElementById('toast').textContent.length > 0, null, { timeout: 8000 })
  return page.innerText('#toast')
}

/** Clic que provoca un aviso; devuelve su texto. */
async function clickAndToast(page, selector) {
  await clearToast(page)
  await page.click(selector)
  return toast(page)
}

/** Las estadísticas se cachean 10 s en el relé: pulsa «Actualizar» hasta que se cumpla la condición. */
async function refreshUntil(page, cond, arg) {
  for (let i = 0; i < 25; i++) {
    if (await page.evaluate(cond, arg)) return
    await sleep(1000)
    await page.click('#refresh')
  }
  throw new Error('el panel no llegó a mostrar los datos esperados')
}

const newKey = () => { const sk = generateSecretKey(); return { sk, pk: getPublicKey(sk) } }

/** Un firmador NIP-46 de mentira (lo que haría Clave): abre el enlace nostrconnect://, acepta la conexión y firma. */
function mockSigner(uri, { userSecret = null, onReplied = () => {}, relayIndex = 0 } = {}) {
  const u = new URL(uri)
  const clientPk = u.hostname
  const secret = u.searchParams.get('secret')
  const userSk = userSecret ?? stack.ownerSecret
  const signerSk = generateSecretKey()
  const signerPk = getPublicKey(signerSk)
  const key = nip44.getConversationKey(signerSk, clientPk)
  // el firmador usa el relé del enlace que le toque (en las pruebas todos son rutas del mismo relé)
  const ws = new WebSocket(stack.relayWs + new URL(u.searchParams.getAll('relay')[relayIndex]).pathname.replace(/\/$/, ''))
  const reply = (obj) => ws.send(JSON.stringify(['EVENT', finalizeEvent({ kind: 24133, created_at: now(), tags: [['p', clientPk]], content: nip44.encrypt(JSON.stringify(obj), key) }, signerSk)]))
  const seen = { methods: [], ids: new Set() }
  ws.on('open', () => {
    ws.send(JSON.stringify(['REQ', 's', { kinds: [24133], '#p': [signerPk], since: now() - 30 }]))
    reply({ id: Math.random().toString(16).slice(2), result: secret }) // aceptación de la conexión
    onReplied()
  })
  ws.on('message', (raw) => {
    const m = JSON.parse(raw)
    if (m[0] !== 'EVENT') return
    if (seen.ids.has(m[2].id)) return // el panel manda lo mismo por todos los relés del enlace; aquí llega repetido
    seen.ids.add(m[2].id)
    let req
    try { req = JSON.parse(nip44.decrypt(m[2].content, key)) } catch { return }
    seen.methods.push(req.method)
    if (req.method === 'get_public_key') reply({ id: req.id, result: getPublicKey(userSk) })
    else if (req.method === 'sign_event') reply({ id: req.id, result: JSON.stringify(finalizeEvent(JSON.parse(req.params[0]), userSk)) })
    else reply({ id: req.id, error: 'método no soportado' })
  })
  return { seen, close: () => ws.close() }
}

describe('panel de control', () => {
  before(async () => {
    stack = await startStack()
    relay = await Relay.connect(stack.relayWs)
    browser = await chromium.launch()
    // histórico de 35 días para las pestañas largas (la tabla la crea el relé al arrancar)
    const H = Math.floor(now() / 3600) * 3600
    const rows = []
    for (let i = 0; i < 35 * 24; i++) {
      const h = H - i * 3600
      const wave = 1 + Math.sin(((h / 3600) % 24) / 24 * Math.PI * 2)
      const eph = Math.round(300 * wave)
      rows.push([h, 'saved', Math.round(2 * wave)], [h, 'ephemeral', eph], [h, 'rejected', Math.round(eph * 0.6)], [h, 'rej:rate-limited', Math.round(eph * 0.55)], [h, 'authenticated', 2],
        [h, 'max:conns', 3 + Math.round(wave * 4)], [h, 'max:db_bytes', 400000 + (35 * 24 - i) * 2500], [h, 'max:events', 20 + Math.round((35 * 24 - i) / 6)])
    }
    const sql = 'BEGIN;' + rows.map(([h, m, n]) => `INSERT OR REPLACE INTO activity_hourly VALUES (${h},'${m}',${n});`).join('') + 'COMMIT;'
    execFileSync('python3', ['-c', `import sqlite3,sys; c=sqlite3.connect(${JSON.stringify(stack.dbPath)}, timeout=30); c.executescript(sys.stdin.read())`], { input: sql })
  })

  after(async () => {
    await browser?.close()
    relay?.close()
    await stack?.stop()
  })

  it('sin extensión avisa, y una clave que no es la del dueño no entra', async () => {
    const a = await openPanel({ extension: false })
    await a.page.click('#login-btn')
    assert.match(await a.page.innerText('#login-msg'), /extensión de Nostr/)
    await a.ctx.close()

    const stranger = newKey()
    const b = await openPanel({ signWith: () => stranger.sk })
    await b.page.click('#login-btn')
    await b.page.waitForFunction(() => document.getElementById('login-msg').textContent.includes('No se pudo entrar'), null, { timeout: 8000 })
    assert.match(await b.page.innerText('#login-msg'), /owner|dueño/i)
    assert.equal(await b.page.isVisible('#dash'), false, 'el panel no debe abrirse')
    await b.ctx.close()
  })

  it('el dueño entra, la sesión sobrevive a recargar y cerrar sesión la termina', async () => {
    const { ctx, page } = await openPanel()
    await login(page, { fresh: true })
    await page.reload()
    await page.waitForSelector('#dash:not([hidden])', { timeout: 8000 }) // la cookie mantiene la sesión sin volver a firmar
    await page.click('#logout')
    await page.waitForSelector('#login:not([hidden])')
    await page.reload()
    await sleep(500)
    assert.equal(await page.isVisible('#login'), true, 'tras cerrar sesión recargar no entra solo')
    await ctx.close()
  })

  it('el resumen muestra tarjetas, tipos, recientes y configuración; lo privado no enseña contenido y el HTML es texto', async () => {
    const someone = newKey(), other = newKey()
    await pub(someone.sk, 1, 'Hola desde el panel <img src=x onerror="window.__xss=1"> <script>window.__xss=2</script>')
    await pub(other.sk, 4, 'mensaje privado secreto', [['p', stack.ownerPk]])
    await pub(someone.sk, 1, 'x'.repeat(900)) // rechazada por tamaño: aparece en Rechazos
    const { ctx, page } = await openPanel()
    await login(page)
    await refreshUntil(page, () => document.querySelectorAll('#recent li').length > 0 && document.querySelectorAll('#rejections tbody tr').length > 0)
    const cards = await page.$$eval('.card', (c) => c.map((x) => x.innerText.replace(/\n/g, ' ')))
    assert.ok(cards.length >= 6 && cards.some((c) => /Eventos guardados/.test(c)))
    assert.match(await page.innerText('#kinds'), /Nota/)
    assert.match(await page.innerText('#reasons'), /invalid/)
    const recent = await page.innerText('#recent')
    assert.ok(recent.includes('Hola desde el panel'), 'el contenido público se ve (recortado)')
    assert.ok(!recent.includes('secreto'), 'el contenido de un mensaje privado no llega al panel')
    assert.equal(await page.evaluate(() => window.__xss ?? 'no'), 'no', 'el HTML de un evento no se ejecuta')
    assert.equal(await page.locator('#recent img, #recent script').count(), 0)
    assert.match(await page.innerText('#config'), /Retención\s+180 días/)
    assert.equal(await page.locator('.collapsed').count(), 0, 'en el ordenador no hay nada plegado')
    assert.equal(await page.locator('#dash section.panel > h2[role=button]').count(), 0, 'ni títulos que actúen como botón')
    assert.match(await page.innerText('#subtitle'), /Versión/)
    await ctx.close()
  })

  it('la ayuda explica todas las etiquetas del panel y los botones ? llevan a su sección', async () => {
    const { ctx, page } = await openPanel()
    await page.click('#help-btn')
    assert.equal(await page.evaluate(() => document.getElementById('help').open), true, 'la ayuda se abre incluso sin sesión')
    await page.keyboard.press('Escape')
    assert.equal(await page.evaluate(() => document.getElementById('help').open), false)

    await login(page)
    await page.click('.tabs [data-range="24h"]')
    await page.waitForFunction(() => document.getElementById('hist-summary').children.length > 0, null, { timeout: 8000 })
    const help = (await page.evaluate(() => document.getElementById('help').textContent)).toLowerCase()
    const labels = [
      ...(await page.$$eval('.card .l', (e) => e.map((x) => x.textContent))),
      ...(await page.$$eval('#config dt', (e) => e.map((x) => x.textContent))),
      ...(await page.$$eval('#mod h3', (e) => e.map((x) => x.textContent))),
      ...(await page.$$eval('#hist-summary dt', (e) => e.map((x) => x.textContent))),
      ...(await page.$$eval('.tabs button', (e) => e.map((x) => x.textContent))),
      ...(await page.$$eval('#dash h2', (e) => e.map((x) => x.childNodes[0].textContent.replace(/\s*·.*/, '').trim()))),
      'Actualizar', 'Cerrar sesión', 'Ayuda',
    ]
    const missing = labels.filter((l) => !help.includes(l.toLowerCase().replace(/\s*\(.*\)/, '')))
    assert.deepEqual(missing, [], `etiquetas del panel sin explicar en la ayuda: ${missing.join(', ')}`)
    assert.ok(labels.length > 40, `se esperaban muchas etiquetas, hubo ${labels.length}`)

    for (const id of ['h-cards', 'h-activity', 'h-growth', 'h-kinds', 'h-rejections', 'h-recent', 'h-noisy', 'h-search', 'h-moderation', 'h-config']) {
      await page.click(`[data-help="${id}"]`)
      const offset = await page.evaluate((i) => Math.round(document.getElementById(i).getBoundingClientRect().top - document.getElementById('help-body').getBoundingClientRect().top), id)
      assert.ok(offset >= 0 && offset < 200, `el ? de ${id} debe llevar a su sección (offset ${offset})`)
      await page.click('#help-close')
    }
    await ctx.close()
  })

  it('las pestañas de actividad muestran el histórico persistido', async () => {
    const { ctx, page } = await openPanel()
    await login(page)
    for (const [range, bars] of [['24h', 24], ['7d', 168], ['30d', 30], ['90d', 35]]) {
      await page.click(`.tabs [data-range="${range}"]`)
      await page.waitForFunction(() => document.getElementById('hist-summary').children.length > 0, null, { timeout: 8000 })
      const rects = await page.locator('#chart rect').count()
      assert.ok(rects > 0, `${range}: debe haber barras`)
      assert.equal(await page.getAttribute(`.tabs [data-range="${range}"]`, 'aria-pressed'), 'true')
      const summary = await page.innerText('#hist-summary')
      assert.match(summary, /Guardados\s+[\d.]+/)
      assert.match(summary, /Conexiones máx/)
      if (range !== '24h') assert.match(summary, /Base de datos/)
      void bars
    }
    await page.click('#refresh')
    await sleep(600)
    assert.equal(await page.getAttribute('.tabs [data-range="90d"]', 'aria-pressed'), 'true', 'actualizar mantiene el periodo')
    await page.click('.tabs [data-range="60m"]')
    assert.match(await page.innerText('#hist-summary'), /Guardados/)
    await ctx.close()
  })

  it('moderación: banear y desbanear una clave (borrando sus eventos) desde el formulario', async () => {
    const spammer = newKey()
    await pub(spammer.sk, 1, 'spam uno'); await pub(spammer.sk, 1, 'spam dos')
    const { ctx, page } = await openPanel()
    await login(page)
    await page.fill('#f-ban [name=pubkey]', nip19.npubEncode(spammer.pk))
    await page.fill('#f-ban [name=reason]', '<img src=x onerror="window.__xss=1"> spam')
    await page.check('#f-ban [name=deleteEvents]')
    assert.match(await clickAndToast(page, '#f-ban button[type=submit]'), /baneada y 2 evento/)
    assert.match(await page.innerText('#l-banned'), /spam/)
    assert.equal(await page.locator('#l-banned img').count(), 0, 'el motivo es texto')
    assert.equal(await page.evaluate(() => window.__xss ?? 'no'), 'no')
    assert.match(await pub(spammer.sk, 1, 'otra vez'), /banned/)
    assert.match(await clickAndToast(page, '#l-banned button'), /Baneo quitado/)
    assert.equal(await pub(spammer.sk, 1, 'perdonado'), 'ok')
    await ctx.close()
  })

  it('moderación: lista blanca (el dueño no queda fuera), vetar evento, tipos, IPs e información del relé', async () => {
    const friend = newKey(), stranger = newKey(), author = newKey()
    const { ctx, page } = await openPanel()
    await login(page)

    // lista blanca
    await page.fill('#f-allow [name=pubkey]', friend.pk)
    assert.match(await clickAndToast(page, '#f-allow button[type=submit]'), /permitida/)
    assert.match(await pub(stranger.sk, 1, 'extraño'), /restricted/)
    assert.equal(await pub(friend.sk, 1, 'amigo'), 'ok')
    assert.equal(await pub(generateSecretKey() && stack.ownerSecret, 1, 'dueño'), 'ok', 'el dueño nunca queda bloqueado')
    assert.equal((await (await fetch(stack.relayHttp, { headers: { Accept: 'application/nostr+json' } })).json()).limitation.restricted_writes, true)
    assert.match(await clickAndToast(page, '#l-allowed button'), /quitada de la lista blanca/)
    assert.equal(await pub(stranger.sk, 1, 'ya abierto'), 'ok')

    // vetar un evento por su note1
    const bad = finalizeEvent({ kind: 1, created_at: now(), tags: [], content: 'contenido a vetar' }, author.sk)
    await relay.publish(bad)
    await page.fill('#f-event [name=id]', nip19.noteEncode(bad.id))
    assert.match(await clickAndToast(page, '#f-event button[type=submit]'), /vetado y borrado/)
    assert.match(await relay.publish(bad).then(() => 'ok', (e) => String(e.message)), /banned/)

    // tipos
    await page.fill('#f-kind [name=kind]', '7')
    await page.selectOption('#f-kind [name=rule]', 'disallow')
    assert.match(await clickAndToast(page, '#f-kind button[type=submit]'), /Regla aplicada/)
    assert.match(await pub(friend.sk, 7, '+'), /kind 7/)
    assert.match(await clickAndToast(page, '#l-kinds button'), /Regla quitada/)
    assert.equal(await pub(friend.sk, 7, '+'), 'ok')

    // IPs (una inválida se rechaza)
    await page.fill('#f-ip [name=ip]', '203.0.113.9')
    assert.match(await clickAndToast(page, '#f-ip button[type=submit]'), /IP bloqueada/)
    assert.match(await page.innerText('#l-ips'), /203\.0\.113\.9/)
    assert.match(await clickAndToast(page, '#l-ips button'), /desbloqueada/)
    await page.fill('#f-ip [name=ip]', 'no-es-una-ip')
    assert.match(await clickAndToast(page, '#f-ip button[type=submit]'), /not a valid IP/)

    // información del relé: guardar, que el refresco no borre lo que escribes, y restaurar
    await page.fill('#f-info [name=name]', 'Nombre desde el panel')
    await page.fill('#f-info [name=description]', 'English text | Texto en español')
    assert.match(await clickAndToast(page, '#f-info button[type=submit]'), /guardada/)
    const doc = await (await fetch(stack.relayHttp, { headers: { Accept: 'application/nostr+json' } })).json()
    assert.equal(doc.name, 'Nombre desde el panel')
    assert.equal(doc.description, 'English text | Texto en español')

    // contacto, etiquetas, idiomas y normas: el NIP-11 los anuncia, los repetidos y vacíos se limpian, y los inválidos se rechazan
    await page.fill('#f-info [name=contact]', 'zhash@rizful.com')
    await page.fill('#f-info [name=tags]', ' general, open ,General,, es ')
    await page.fill('#f-info [name=languages]', 'en, es')
    await page.fill('#f-info [name=postingPolicy]', 'https://example.com/normas')
    assert.match(await clickAndToast(page, '#f-info button[type=submit]'), /guardada/)
    const doc1 = await (await fetch(stack.relayHttp, { headers: { Accept: 'application/nostr+json' } })).json()
    assert.equal(doc1.contact, 'zhash@rizful.com')
    assert.deepEqual(doc1.tags, ['general', 'open', 'es'])
    assert.deepEqual(doc1.language_tags, ['en', 'es'])
    assert.equal(doc1.posting_policy, 'https://example.com/normas')
    assert.equal(await page.inputValue('#f-info [name=tags]'), 'general, open, es', 'el formulario muestra la lista limpia')
    for (const id of ['contact', 'tags', 'languages', 'postingPolicy']) assert.equal(await page.isVisible(`#o-${id}`), true, `marca «cambiado» en ${id}`)
    await page.fill('#f-info [name=languages]', 'en, no es')
    assert.match(await clickAndToast(page, '#f-info button[type=submit]'), /invalid item/)
    await page.fill('#f-info [name=languages]', 'en, es')
    await page.fill('#f-info [name=postingPolicy]', 'http://inseguro.example')
    assert.match(await clickAndToast(page, '#f-info button[type=submit]'), /https/)
    await page.fill('#f-info [name=postingPolicy]', 'https://example.com/normas')

    await page.fill('#f-info [name=description]', 'escribiendo…')
    await page.click('#refresh')
    await sleep(700)
    assert.equal(await page.inputValue('#f-info [name=description]'), 'escribiendo…', 'el refresco automático no debe borrar lo que estás escribiendo')
    assert.match(await clickAndToast(page, '#info-reset'), /Restaurados/)
    const doc2 = await (await fetch(stack.relayHttp, { headers: { Accept: 'application/nostr+json' } })).json()
    assert.equal(doc2.name, 'Relé de pruebas')
    assert.equal(doc2.description, 'Descripción original')
    assert.equal(doc2.contact, '', 'restaurar vuelve al contacto de la configuración (vacío en las pruebas)')
    assert.equal(doc2.tags ?? null, null)
    assert.equal(await page.isVisible('#o-tags'), false)
    await ctx.close()
  })

  it('claves más ruidosas: ranking de rechazos, copiar la clave, buscar y banear (la del dueño no se puede banear)', async () => {
    const loud = newKey(), quiet = newKey()
    for (let i = 0; i < 4; i++) await pub(loud.sk, 1, 'z'.repeat(900)) // demasiado largo: rechazado
    await pub(quiet.sk, 1, 'z'.repeat(900))
    await pub(stack.ownerSecret, 1, 'z'.repeat(900)) // el dueño también, para ver que no se ofrece banearlo
    const { ctx, page } = await openPanel()
    await ctx.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: new URL(stack.panelUrl).origin })
    await login(page)
    await refreshUntil(page, (pk) => [...document.querySelectorAll('#noisy tbody tr')].some((r) => r.textContent.includes(pk)), loud.pk.slice(0, 12))
    const rows = await page.$$eval('#noisy tbody tr', (r) => r.map((x) => x.innerText.replace(/\s+/g, ' ')))
    assert.ok(rows[0].includes(loud.pk.slice(0, 12)) && /\b4\b/.test(rows[0]), `la más ruidosa va primero con sus 4 rechazos: ${rows[0]}`)
    const ownerRow = page.locator('#noisy tbody tr', { hasText: stack.ownerPk.slice(0, 12) })
    assert.equal(await ownerRow.locator('button:has-text("Banear")').count(), 0, 'la clave del dueño no se puede banear desde aquí')
    assert.match(await ownerRow.innerText(), /tuya/)

    await page.locator('#noisy tbody tr').first().locator('button.pk').click()
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), loud.pk, 'pulsar la clave copia la completa')

    await page.locator('#noisy tbody tr', { hasText: loud.pk.slice(0, 12) }).locator('button:has-text("Buscar")').click()
    await page.waitForSelector('#search-out .resultinfo')
    assert.match(await page.innerText('#search-out .resultinfo'), new RegExp(loud.pk.slice(0, 12)))

    assert.match(await clickAndToast(page, `#noisy tbody tr:has-text("${loud.pk.slice(0, 12)}") button:has-text("Banear")`), /Clave baneada/)
    assert.match(await pub(loud.sk, 1, 'hola'), /banned/)
    assert.match(await page.innerText('#l-banned'), /claves más ruidosas/)
    await page.click('#l-banned button')
    await ctx.close()
  })

  it('estado del servidor: disco y última copia de seguridad (en rojo si es antigua)', async () => {
    const { ctx, page } = await openPanel()
    await login(page)
    const card = (label) => page.locator('.card', { hasText: label })
    assert.match(await card('Disco usado').innerText(), /\d+ %[\s\S]*libres/)
    assert.equal(await card('Disco usado').locator('.v').getAttribute('class'), 'v', 'con disco de sobra no se avisa')
    const backup = card('Última copia de seguridad')
    assert.match(await backup.innerText(), /hace[\s\S]*2\.0 KB · 1 guardadas/)
    assert.equal(await backup.locator('.v').getAttribute('class'), 'v', 'una copia reciente no se marca')

    fs.utimesSync(stack.backupFile, new Date(Date.now() - 50 * 3600 * 1000), new Date(Date.now() - 50 * 3600 * 1000)) // hace 50 h
    await page.click('#refresh')
    await page.waitForFunction(() => [...document.querySelectorAll('.card')].some((c) => c.textContent.includes('Última copia') && c.querySelector('.v.warn')), null, { timeout: 8000 })
    assert.match(await backup.innerText(), /hace 2 d/)
    await ctx.close()
  })

  it('copia de seguridad: el botón descarga una base de datos SQLite válida y comprimida, y queda en el historial', async () => {
    await pub(newKey().sk, 1, 'nota para la copia')
    const { ctx, page } = await openPanel()
    await login(page)
    const [download] = await Promise.all([page.waitForEvent('download', { timeout: 15000 }), page.click('#backup-btn')])
    assert.match(download.suggestedFilename(), /^nostr-relay-khatru-\d{8}T\d{6}Z\.sqlite\.gz$/)
    const file = await download.path()
    const raw = zlib.gunzipSync(fs.readFileSync(file))
    assert.equal(raw.subarray(0, 15).toString(), 'SQLite format 3', 'tras descomprimir es una base de datos SQLite')
    assert.ok(raw.length > 4096)
    assert.match(await toast(page), /Copia descargada/)
    assert.equal(await page.isEnabled('#backup-btn'), true, 'el botón vuelve a estar activo')
    await page.waitForFunction(() => document.querySelector('#audit tbody').textContent.includes('Copia de seguridad descargada'), null, { timeout: 8000 })
    await ctx.close()
  })

  it('historial de acciones: anota lo que haces (con tu nota), los inicios de sesión y lo hecho por NIP-86', async () => {
    const target = newKey(), other = newKey()
    const { ctx, page } = await openPanel()
    await login(page)
    await page.fill('#f-ban [name=pubkey]', target.pk)
    await page.fill('#f-ban [name=reason]', 'motivo <b>de prueba</b>')
    assert.match(await clickAndToast(page, '#f-ban button[type=submit]'), /baneada/)
    await page.waitForFunction(() => document.querySelector('#audit tbody').textContent.includes('Clave baneada'), null, { timeout: 8000 })
    const rows = await page.$$eval('#audit tbody tr', (r) => r.map((x) => x.innerText.replace(/\s+/g, ' ')))
    assert.ok(rows[0].includes('Clave baneada') && rows[0].includes(target.pk.slice(0, 12)) && rows[0].includes('motivo <b>de prueba</b>') && rows[0].endsWith('panel'), rows[0])
    assert.equal(await page.locator('#audit tbody tr b').count(), 0, 'la nota es texto, no HTML')
    assert.equal(await page.locator('#audit tbody tr').first().locator('code').getAttribute('title'), target.pk, 'el completo sale al pasar el ratón')
    assert.ok(rows.some((r) => r.includes('Inicio de sesión')), 'el inicio de sesión queda anotado')

    // lo hecho con un cliente NIP-86 también sale, marcado como tal
    const auth = (url, body) => { const ev = finalizeEvent({ kind: 27235, created_at: now(), tags: [['u', url], ['method', 'POST'], ['payload', createHash('sha256').update(body).digest('hex')], ['n', String(Math.random())]], content: '' }, stack.ownerSecret); return 'Nostr ' + Buffer.from(JSON.stringify(ev)).toString('base64') }
    const body = JSON.stringify({ method: 'banpubkey', params: [other.pk, 'desde nip86'] })
    const res = await fetch(stack.relayHttp + '/', { method: 'POST', headers: { 'Content-Type': 'application/nostr+json+rpc', Authorization: auth(stack.relayHttp + '/', body) }, body })
    assert.equal(res.status, 200, await res.text())
    await page.click('#refresh')
    await page.waitForFunction(() => /desde nip86[\s\S]*NIP-86|NIP-86/.test(document.querySelector('#audit tbody').textContent), null, { timeout: 8000 })
    assert.ok((await page.innerText('#audit tbody')).includes('desde nip86'))

    await ctx.close()
  })

  it('búsqueda: clave, texto, id, prefijo, tipo, paginación, mensajes privados y acciones sobre los resultados', async () => {
    const ana = newKey(), bea = newKey()
    await pub(ana.sk, 0, JSON.stringify({ name: 'ana', display_name: 'Ana Pérez' }))
    const marked = finalizeEvent({ kind: 1, created_at: now(), tags: [], content: 'HOLA mundo unico-marcador-e2e' }, ana.sk)
    await relay.publish(marked)
    for (let i = 0; i < 70; i++) await pub(ana.sk, 1, `nota de ana ${i}`, [], 100 + i)
    await pub(bea.sk, 1, 'hola desde bea unico-marcador-e2e'); await pub(bea.sk, 7, '+')
    await pub(bea.sk, 4, 'mensaje privado secreto', [['p', ana.pk]])
    await pub(bea.sk, 1, 'y'.repeat(900)) // rechazada: da una clave abreviada en Rechazos

    const { ctx, page } = await openPanel()
    await login(page)
    const info = async () => (await page.innerText('#search-out .resultinfo')).replace(/\s+/g, ' ')
    const search = async (q, kind = '') => {
      await page.fill('#f-search [name=q]', q)
      await page.fill('#f-search [name=kind]', kind)
      await clearToast(page)
      await page.click('#f-search button[type=submit]')
      await page.waitForSelector('#search-out .resultinfo')
      await sleep(250)
    }

    // por npub: tarjeta de la clave, paginación y filtro por tipo desde la tarjeta
    await search(nip19.npubEncode(ana.pk))
    assert.match(await info(), /72 resultado/)
    const card = await page.innerText('.keycard')
    assert.match(card, /Ana Pérez/); assert.match(card, /72 evento/); assert.match(card, /Banear clave/)
    assert.equal(await page.locator('#search-list > li').count(), 50)
    await page.click('#search-more')
    await page.waitForFunction(() => document.querySelectorAll('#search-list > li').length === 72, null, { timeout: 8000 })
    assert.equal(await page.locator('#search-more').count(), 0)
    await page.click('.keycard .chipbtn:has-text("Perfil")')
    await sleep(300)
    assert.match(await info(), /1 resultado/)

    // texto (sin distinguir mayúsculas), id (note1) y prefijos
    await search('unico-marcador-e2e'); assert.match(await info(), /2 resultado/)
    await search('HOLA'); assert.match(await info(), /[23] resultado/)
    await search(nip19.noteEncode(marked.id)); assert.match(await info(), /1 resultado/)
    await search(bea.pk.slice(0, 8)); assert.match(await info(), /[23] resultado/)
    await search('7'); assert.match(await info(), /\d+ resultado\(s\): eventos de tipo 7/)
    await search('', '4')
    assert.match(await info(), /\d+ resultado\(s\): eventos de tipo 4/)
    assert.ok(!(await page.innerText('#search-list')).includes('secreto'), 'un mensaje privado no enseña su contenido')
    await search('zzz-no-existe-zzz')
    assert.match(await page.innerText('#search-out'), /No hay eventos/)

    // clave abreviada de Rechazos → búsqueda
    await refreshUntil(page, () => document.querySelectorAll('#rejections tbody tr .pk').length > 0)
    await page.click('#rejections tbody tr .pk')
    await page.waitForFunction(() => /empiezan por/.test(document.querySelector('#search-out .resultinfo')?.textContent || ''), null, { timeout: 8000 })

    // acciones sobre los resultados: vetar un evento y banear/desbanear la clave
    await search(nip19.npubEncode(ana.pk))
    await clearToast(page)
    await page.locator('#search-list > li').first().locator('button:has-text("Vetar evento")').click()
    assert.match(await toast(page), /vetado y borrado/)
    await page.waitForFunction(() => /71 resultado/.test(document.querySelector('#search-out .resultinfo')?.textContent || ''), null, { timeout: 8000 })
    await clearToast(page)
    await page.click('.keycard button:has-text("Banear clave")')
    assert.match(await toast(page), /Clave baneada/)
    await page.waitForFunction(() => document.querySelector('.keycard')?.textContent.includes('baneada'), null, { timeout: 8000 })
    assert.match(await pub(ana.sk, 1, 'otra'), /banned/)
    await clearToast(page)
    await page.click('.keycard button:has-text("Quitar baneo")')
    assert.match(await toast(page), /Baneo quitado/)
    assert.equal(await pub(ana.sk, 1, 'perdonada'), 'ok')

    await page.click('#search-clear')
    assert.equal(await page.locator('#search-out *').count(), 0)
    await ctx.close()
  })

  it('búsqueda con rango de fechas: desde, hasta, ambos, botones rápidos, validación y limpiar', async () => {
    const who = newKey()
    const day = 86400
    await pub(who.sk, 1, 'rango marca-fechas vieja', [], 10 * day)
    await pub(who.sk, 1, 'rango marca-fechas media', [], 5 * day)
    await pub(who.sk, 1, 'rango marca-fechas reciente', [], 1 * day)
    const { ctx, page } = await openPanel()
    await login(page)
    const iso = (n) => page.evaluate((k) => { const d = new Date(); d.setDate(d.getDate() - k); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}` }, n)
    const run = async (from, to, q = 'marca-fechas') => {
      await page.fill('#f-search [name=q]', q)
      await page.fill('#f-search [name=kind]', '')
      await page.fill('#f-search [name=from]', from)
      await page.fill('#f-search [name=to]', to)
      await page.click('#f-search button[type=submit]')
      await page.waitForFunction(() => document.querySelector('#search-out .resultinfo'), null, { timeout: 8000 })
      await sleep(250)
      return (await page.innerText('#search-out .resultinfo')).replace(/\s+/g, ' ')
    }
    assert.match(await run('', ''), /^3 resultado/, 'sin fechas salen las 3')
    const d7 = await iso(7), d2 = await iso(2)
    assert.match(await run(d7, ''), /^2 resultado\(s\):.*desde el \d\d\/\d\d\/\d{4}$/, 'desde hace 7 días: la media y la reciente')
    assert.match(await run('', d7), /^1 resultado\(s\):.*hasta el /, 'hasta hace 7 días: la vieja')
    const both = await run(d7, d2)
    assert.match(both, /^1 resultado\(s\):.* · del \d\d\/\d\d\/\d{4} al \d\d\/\d\d\/\d{4}$/, 'entre hace 7 y 2 días: la media')
    assert.ok((await page.innerText('#search-list')).includes('media'))
    const day1 = await iso(1)
    assert.match(await run(day1, day1), /^1 resultado\(s\):.* · el \d\d\/\d\d\/\d{4}$/, 'un solo día completo')

    // sin texto, el rango sirve para ver lo publicado en un periodo
    assert.match(await run(d7, d2, ''), /resultado\(s\): todos los eventos · del /)

    // botones rápidos
    await page.click('#f-search [data-days="7"]')
    assert.equal(await page.inputValue('#f-search [name=from]'), await iso(6), '7 días = hoy y los 6 anteriores')
    assert.equal(await page.inputValue('#f-search [name=to]'), await iso(0))
    await page.fill('#f-search [name=q]', 'marca-fechas')
    await page.click('#f-search button[type=submit]')
    await page.waitForFunction(() => /^2 resultado/.test(document.querySelector('#search-out .resultinfo')?.textContent || ''), null, { timeout: 8000 })
    await page.click('#f-search [data-days="1"]')
    assert.equal(await page.inputValue('#f-search [name=from]'), await iso(0))
    await page.click('#f-search button[type=submit]')
    await page.waitForFunction(() => /^0 resultado/.test(document.querySelector('#search-out .resultinfo')?.textContent || ''), null, { timeout: 8000 })

    // «Desde» posterior a «Hasta»: se avisa sin buscar
    await page.fill('#f-search [name=from]', d2)
    await page.fill('#f-search [name=to]', d7)
    await clearToast(page)
    await page.click('#f-search button[type=submit]')
    assert.match(await toast(page), /no puede ser posterior/)

    // las búsquedas que lanzan otros botones parten sin fechas, y «Limpiar» las borra
    await page.fill('#f-search [name=from]', d7)
    await page.click('#f-search button[type=submit]')
    await sleep(300)
    await page.click('#search-clear')
    assert.equal(await page.inputValue('#f-search [name=from]'), '')
    assert.equal(await page.locator('#search-out *').count(), 0)
    await ctx.close()
  })

  it('entrar con un firmador remoto (NIP-46, como Clave): enlace nostrconnect, aprobación y firma', async () => {
    const { ctx, page } = await openPanel({ extension: false })
    assert.equal(await page.isVisible('#remote-box'), false)
    assert.equal(await page.evaluate(() => document.getElementById('remote-login').open), true, 'sin extensión, el apartado remoto sale abierto')
    await page.click('#remote-btn')
    await page.waitForSelector('#remote-link[href^="https://clave.casa/connect/?uri="]')
    const uri = await page.getAttribute('#remote-link', 'data-uri')
    const u = new URL(uri)
    assert.match(u.hostname, /^[0-9a-f]{64}$/, 'la clave del cliente va en el enlace')
    const relays = u.searchParams.getAll('relay')
    const host = new URL(stack.panelUrl).host
    assert.deepEqual(relays, [`ws://${host}`, `ws://${host}/clave`], 'este relé primero y sin barra final, y después el relé extra (en producción, el de Clave)')
    assert.equal(await page.getAttribute('#remote-link', 'href'), `https://clave.casa/connect/?uri=${encodeURIComponent(uri)}`, '«Abrir Clave» usa el enlace universal de Clave con el enlace codificado dentro')
    assert.match(u.searchParams.get('secret'), /^[0-9a-f]{32}$/)
    assert.equal(u.searchParams.get('perms'), 'sign_event:27235')
    assert.equal(u.searchParams.get('callback'), new URL(stack.panelUrl).origin + '/admin/')
    assert.match(await page.innerText('#remote-msg'), /Esperando a que apruebes/)
    await page.waitForFunction(() => document.getElementById('remote-diag').textContent.includes('conectado ✓'), null, { timeout: 8000 }) // el panel explica en qué punto está

    const signer = mockSigner(uri)
    await page.waitForSelector('#dash:not([hidden])', { timeout: 15000 })
    assert.deepEqual(signer.seen.methods, ['sign_event'], 'solo se pide firmar el inicio de sesión, nada más')
    await page.waitForFunction(() => document.querySelector('#audit tbody').textContent.includes('Inicio de sesión'), null, { timeout: 8000 })
    signer.close()
    await ctx.close()
  })

  it('entrar con un firmador remoto: sobrevive a que la página se suspenda mientras apruebas (buzón del relé)', async () => {
    const { ctx, page } = await openPanel({ extension: false })
    let mode = 'pass'
    const live = new Set()
    await page.routeWebSocket(/^ws:\/\/127\.0\.0\.1:\d+\/(clave)?$/, (ws) => {
      live.add(ws)
      if (mode === 'pass') ws.connectToServer()
      // en modo 'block' la conexión queda muerta: lo que envíe la página no llega y no recibe nada (como un Safari suspendido)
    })
    await page.reload() // la interceptación solo vale para páginas cargadas después de instalarla
    assert.equal(await page.evaluate(() => document.getElementById('remote-login').open), true, 'sin extensión, el apartado remoto sale abierto')
    await page.click('#remote-btn')
    await page.waitForSelector('#remote-link[href^="https://clave.casa/connect/?uri="]')
    const uri = await page.getAttribute('#remote-link', 'data-uri')
    await page.waitForFunction(() => true)
    await sleep(500) // la página ya está conectada y suscrita

    mode = 'block' // se suspende: la conexión se corta y no hay forma de volver a conectar
    for (const ws of [...live]) ws.close()
    live.clear()
    await sleep(300)
    let replied
    const repliedP = new Promise((r) => { replied = r })
    const signer = mockSigner(uri, { onReplied: replied })
    await repliedP // el firmador aprueba mientras la página no escucha
    await sleep(800)
    assert.equal(await page.isVisible('#dash'), false, 'aún sin entrar: la respuesta no ha llegado')

    mode = 'pass' // la página despierta
    for (const ws of [...live]) ws.close() // las reconexiones bloqueadas se cortan y la siguiente ya pasa
    await page.waitForSelector('#dash:not([hidden])', { timeout: 20000 })
    assert.deepEqual(signer.seen.methods, ['sign_event'])
    signer.close()
    await ctx.close()
  })

  it('entrar con un firmador remoto: sirve que el firmador conteste solo por el relé extra (el de Clave)', async () => {
    const { ctx, page } = await openPanel({ extension: false })
    await page.click('#remote-btn')
    await page.waitForSelector('#remote-link[href^="https://clave.casa/connect/?uri="]')
    const signer = mockSigner(await page.getAttribute('#remote-link', 'data-uri'), { relayIndex: 1 })
    await page.waitForSelector('#dash:not([hidden])', { timeout: 15000 })
    assert.deepEqual(signer.seen.methods, ['sign_event'], 'la petición de firma le llega por el relé extra')
    signer.close()
    await ctx.close()
  })

  it('entrar con un firmador remoto: una clave que no es la del dueño no entra, y cancelar limpia todo', async () => {
    const stranger = newKey()
    const { ctx, page } = await openPanel({ extension: false })
    assert.equal(await page.evaluate(() => document.getElementById('remote-login').open), true, 'sin extensión, el apartado remoto sale abierto')
    await page.click('#remote-btn')
    await page.waitForSelector('#remote-link[href^="https://clave.casa/connect/?uri="]')
    const signer = mockSigner(await page.getAttribute('#remote-link', 'data-uri'), { userSecret: stranger.sk })
    await page.waitForFunction(() => document.getElementById('login-msg').textContent.includes('No se pudo entrar'), null, { timeout: 15000 })
    assert.match(await page.innerText('#login-msg'), /owner|dueño/i)
    assert.equal(await page.isVisible('#dash'), false)
    assert.equal(await page.isVisible('#remote-box'), false, 'el cuadro se cierra tras el fallo')
    signer.close()

    await page.click('#remote-btn')
    await page.waitForSelector('#remote-link[href^="https://clave.casa/connect/?uri="]')
    const first = await page.getAttribute('#remote-link', 'data-uri')
    await page.click('#remote-cancel')
    assert.equal(await page.isVisible('#remote-box'), false)
    assert.equal(await page.isVisible('#remote-btn'), true)
    await page.click('#remote-btn')
    await page.waitForSelector('#remote-link[href^="https://clave.casa/connect/?uri="]')
    assert.notEqual(await page.getAttribute('#remote-link', 'data-uri'), first, 'cada intento usa claves y secreto nuevos')
    await ctx.close()
  })

  it('móvil (iPhone): sin desplazamiento horizontal, controles táctiles, letra de 16 px en los campos y tablas apiladas', async () => {
    const lead = newKey()
    for (let i = 0; i < 3; i++) await pub(lead.sk, 1, 'y'.repeat(900)) // rechazos: dan filas en las tablas
    await pub(lead.sk, 1, 'nota para el móvil, con un texto largo largo largo largo largo largo largo largo largo largo largo largo')
    const ctx = await browser.newContext({ ...devices['iPhone 13'], locale: 'es-ES' })
    await ctx.exposeFunction('__sign', (ev) => finalizeEvent(ev, stack.ownerSecret))
    await ctx.addInitScript(() => { window.nostr = { signEvent: (ev) => window.__sign(ev) } })
    const page = await ctx.newPage()
    page.on('dialog', (d) => d.accept())
    page.on('console', (m) => { if (m.type() === 'error' && !/status of 4\d\d/.test(m.text())) consoleErrors.push(m.text()) })
    await page.goto(stack.panelUrl)
    const noScroll = () => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)
    assert.equal(await noScroll(), true, 'la pantalla de entrada no se ensancha')
    await login(page, { fresh: true })
    await refreshUntil(page, () => document.querySelectorAll('#rejections tbody tr').length > 0 && document.querySelectorAll('#noisy tbody tr .pk').length > 0 && document.querySelectorAll('#recent li').length > 0)
    await page.evaluate(() => document.querySelectorAll('#dash section.panel.collapsed > h2').forEach((h) => h.click())) // se miden todas, también las plegadas por defecto
    await page.click('.tabs [data-range="7d"]')
    await page.fill('#f-search [name=q]', 'nota')
    await page.click('#f-search button[type=submit]')
    await page.waitForSelector('#search-out .resultinfo')
    await sleep(300)
    assert.equal(await noScroll(), true, 'el panel completo cabe en el ancho del móvil (sin desplazamiento horizontal)')

    const small = await page.evaluate(() => [...document.querySelectorAll('button, summary, a.btnlike, input:not([type=checkbox]), select, textarea')]
      .filter((e) => { const r = e.getBoundingClientRect(); return r.width > 0 && r.height > 0 && r.height < 32 && !e.closest('dialog:not([open])') })
      .map((e) => `${e.tagName.toLowerCase()}#${e.id || e.name || ''} «${(e.textContent || '').trim().slice(0, 16)}» ${Math.round(e.getBoundingClientRect().height)}px`))
    assert.deepEqual(small, [], `controles demasiado pequeños para el dedo: ${small.join(', ')}`)
    const fonts = await page.evaluate(() => [...document.querySelectorAll('input, select, textarea')].filter((e) => e.getBoundingClientRect().width > 0 && parseFloat(getComputedStyle(e).fontSize) < 16).map((e) => e.name || e.id))
    assert.deepEqual(fonts, [], `campos con letra menor de 16 px (iOS amplía la pantalla al tocarlos): ${fonts.join(', ')}`)

    // las tablas pasan a tarjetas «Etiqueta: valor»
    const display = await page.evaluate(() => ({ thead: getComputedStyle(document.querySelector('#rejections thead')).display, td: getComputedStyle(document.querySelector('#rejections td')).display }))
    assert.deepEqual(display, { thead: 'none', td: 'block' })
    const labels = await page.evaluate(() => ({
      noisy: [...document.querySelectorAll('#noisy tbody tr:first-child td[data-label]')].map((td) => getComputedStyle(td, '::before').content),
      rejections: [...document.querySelectorAll('#rejections tbody tr:first-child td[data-label]')].map((td) => getComputedStyle(td, '::before').content),
    }))
    assert.ok(labels.noisy.includes('"Rechazos: "') && labels.noisy.includes('"Último: "'), `etiquetas de «Claves más ruidosas»: ${labels.noisy}`)
    assert.ok(labels.rejections.includes('"Motivo: "') && labels.rejections.includes('"Hora: "'), `etiquetas de «Rechazos»: ${labels.rejections}`)

    // tras tocar un botón no se queda el borde verde del «hover» (en pantallas táctiles el hover se queda pegado)
    const borderOf = (sel) => page.evaluate((q) => getComputedStyle(document.querySelector(q)).borderTopColor, sel)
    const before = await borderOf('#refresh')
    await page.tap('#refresh')
    await sleep(300)
    assert.equal(await borderOf('#refresh'), before, `«Actualizar» conserva su borde normal tras tocarlo (antes ${before})`)
    assert.notEqual(await borderOf('#refresh'), 'rgb(45, 212, 191)', 'el borde no se queda verde')

    // la ayuda ocupa la pantalla entera
    await page.click('#help-btn')
    const box = await page.locator('#help').boundingBox()
    const vp = page.viewportSize()
    assert.ok(box.width >= vp.width - 1 && box.height >= vp.height - 1, `la ayuda debe ocupar toda la ventana (${vp.width}x${vp.height}): ${JSON.stringify(box)}`)
    assert.equal(await noScroll(), true)
    await page.click('#help-close')
    await ctx.close()
  })

  it('móvil: las secciones se pliegan tocando el título, solo «Actividad» sale abierta y se recuerda lo que abres', async () => {
    const ctx = await browser.newContext({ ...devices['iPhone 13'], locale: 'es-ES' })
    const page = await ctx.newPage()
    page.on('console', (m) => { if (m.type() === 'error' && !/status of 4\d\d/.test(m.text())) consoleErrors.push(m.text()) })
    await page.goto(stack.panelUrl)
    await login(page) // reutiliza la sesión (cookie) para no gastar inicios de sesión
    const titles = await page.$$eval('#dash section.panel > h2', (h) => h.map((x) => x.childNodes[0].textContent.trim()))
    const state = () => page.$$eval('#dash section.panel', (s) => Object.fromEntries(s.map((x) => [x.querySelector(':scope > h2').childNodes[0].textContent.trim(), x.classList.contains('collapsed')])))
    let st = await state()
    assert.ok(titles.length >= 9)
    assert.equal(st['Actividad'], false, 'Actividad sale abierta')
    assert.deepEqual(Object.keys(st).filter((k) => !st[k] && k !== 'Actividad'), [], 'las demás salen plegadas')
    assert.equal(await page.isVisible('#chart'), true)
    assert.equal(await page.isVisible('#noisy'), false, 'lo plegado no se ve')
    assert.equal(await page.getAttribute('#noisy-panel > h2', 'aria-expanded'), 'false')
    assert.equal(await page.getAttribute('#noisy-panel > h2', 'role'), 'button')

    // tocar el título abre; el «?» de ayuda NO pliega
    await page.click('#noisy-panel > h2')
    assert.equal(await page.isVisible('#noisy'), true)
    assert.equal(await page.getAttribute('#noisy-panel > h2', 'aria-expanded'), 'true')
    await page.click('#noisy-panel > h2 .q')
    assert.equal(await page.evaluate(() => document.getElementById('help').open), true, 'el ? abre la ayuda')
    await page.click('#help-close')
    assert.equal(await page.isVisible('#noisy'), true, 'y no ha plegado la sección')
    // con teclado
    await page.focus('#audit-panel > h2')
    await page.keyboard.press('Enter')
    assert.equal(await page.isVisible('#audit'), true)
    // se recuerda al recargar
    await page.reload()
    await page.waitForSelector('#dash:not([hidden])')
    st = await state()
    assert.equal(st['Claves más ruidosas'], false)
    assert.equal(st['Historial de acciones'], false)
    assert.equal(st['Buscar'], true)
    // plegar de nuevo lo que ya no quieres
    await page.click('#noisy-panel > h2')
    assert.equal(await page.isVisible('#noisy'), false)
    // lanzar una búsqueda desde otra sección abre «Buscar» sola
    await page.click('#noisy-panel > h2')
    await page.locator('#noisy tbody tr').first().locator('button:has-text("Buscar")').click()
    await page.waitForSelector('#search-out .resultinfo')
    assert.equal((await state())['Buscar'], false, '«Buscar» se abre cuando se lanza una búsqueda desde otro sitio')
    assert.equal(await page.isVisible('#search-out'), true)
    // al ensanchar la pantalla (ordenador) deja de haber nada plegado
    await page.setViewportSize({ width: 1200, height: 800 })
    assert.equal(await page.locator('.collapsed').count(), 0)
    assert.equal(await page.isVisible('#noisy'), true)
    await ctx.close()
  })

  it('inglés: se elige con EN/ES, se recuerda, y no queda ningún texto en español (ni los que genera el servidor)', async () => {
    // texto de la interfaz que parece español: palabras comunes o letras propias. Se ignoran el contenido de los eventos,
    // los <code> (hay ejemplos como «English | Español» a propósito) y los elementos marcados data-i18n-skip.
    const spanishLeftovers = (page) => page.evaluate(() => {
      const out = []
      const re = /[áéíóúñ¿¡]|\b(de|el|la|los|las|del|que|para|con|una|por|sin|hace|evento|eventos)\b/i
      const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT)
      for (let n; (n = w.nextNode()); ) {
        const txt = n.nodeValue.trim()
        const el = n.parentElement
        if (!txt || !el || el.closest('script, style, code, [data-i18n-skip], .body, .keycard .name, td.reason, .why')) continue
        if (re.test(txt)) out.push(txt.slice(0, 70))
      }
      for (const e of document.querySelectorAll('[title], [aria-label], [placeholder]')) {
        if (e.closest('[data-i18n-skip]')) continue
        for (const a of ['title', 'aria-label', 'placeholder']) { const v = e.getAttribute(a); if (v && re.test(v) && !e.closest('.body, .items code, #recent, #search-list')) out.push(`${a}=${v.slice(0, 60)}`) }
      }
      return [...new Set(out)]
    })

    const who = newKey()
    await pub(who.sk, 1, 'nota en inglés de prueba')
    for (let i = 0; i < 3; i++) await pub(who.sk, 1, 'q'.repeat(900))
    const { ctx, page } = await openPanel({ locale: 'en-US' })
    assert.equal(await page.evaluate(() => document.documentElement.lang), 'en', 'por defecto manda el idioma del navegador')
    assert.equal(await page.innerText('h1'), 'Relay panel')
    assert.equal(await page.title(), 'Relay panel')
    assert.deepEqual(await spanishLeftovers(page), [], 'pantalla de entrada')

    await login(page)
    await refreshUntil(page, () => document.querySelectorAll('#noisy tbody tr .pk').length > 0 && document.querySelectorAll('#recent li').length > 0 && document.querySelectorAll('#rejections tbody tr').length > 0)
    // ejercitar los textos que escribe el JavaScript: pestañas, búsqueda con fechas, acción de moderación y su historial
    await page.click('.tabs [data-range="7d"]')
    await page.waitForFunction(() => document.getElementById('hist-summary').children.length > 0, null, { timeout: 8000 })
    await page.fill('#f-search [name=q]', who.pk)
    await page.fill('#f-search [name=from]', await page.evaluate(() => { const d = new Date(); d.setDate(d.getDate() - 2); return d.toISOString().slice(0, 10) }))
    await page.click('#f-search button[type=submit]')
    await page.waitForSelector('#search-out .resultinfo')
    await sleep(300)
    const info = (await page.innerText('#search-out .resultinfo')).replace(/\s+/g, ' ')
    assert.match(info, /result\(s\): the key or id [0-9a-f]{12}… · since /, `resumen de búsqueda en inglés: ${info}`)
    await page.fill('#f-info [name=contact]', 'owner@example.com')
    assert.match(await clickAndToast(page, '#f-info button[type=submit]'), /Information saved/)
    await page.fill('#f-ip [name=ip]', '203.0.113.20')
    await page.fill('#f-ip [name=reason]', 'scraper')
    assert.match(await clickAndToast(page, '#f-ip button[type=submit]'), /IP blocked/)
    await page.fill('#f-kind [name=kind]', '9')
    await page.selectOption('#f-kind [name=rule]', 'disallow')
    assert.match(await clickAndToast(page, '#f-kind button[type=submit]'), /Rule applied/)
    await page.waitForFunction(() => document.querySelector('#audit tbody').textContent.includes('changed: contact'), null, { timeout: 8000 }) // el servidor lo manda en español: se traduce
    assert.match(await page.innerText('#l-kinds'), /9 — forbidden/)
    await page.click('#info-reset')
    await page.waitForFunction(() => document.querySelector('#audit tbody').textContent.includes('restored: '), null, { timeout: 8000 })
    await page.evaluate(() => { document.getElementById('help').showModal() })
    assert.deepEqual(await spanishLeftovers(page), [], 'panel completo (datos, búsqueda, historial y ayuda) sin restos en español')
    assert.deepEqual(await page.evaluate(() => [...I18N.missing]), [], 'ninguna frase sin traducción en el diccionario')
    await page.evaluate(() => document.getElementById('help').close())

    // cambiar a español y volver; se recuerda al recargar
    await page.click('[data-lang="es"]')
    assert.equal(await page.innerText('h1'), 'Panel del relé')
    assert.equal(await page.getAttribute('[data-lang="es"]', 'aria-pressed'), 'true')
    await page.waitForFunction(() => document.getElementById('subtitle').textContent.startsWith('Versión'), null, { timeout: 8000 })
    assert.match(await page.innerText('#cards'), /Eventos guardados/)
    await page.reload()
    await page.waitForSelector('#dash:not([hidden])')
    assert.equal(await page.innerText('h1'), 'Panel del relé', 'la elección se recuerda')
    await page.click('[data-lang="en"]')
    assert.equal(await page.innerText('h1'), 'Relay panel')
    await page.waitForFunction(() => document.getElementById('subtitle').textContent.startsWith('Version'), null, { timeout: 8000 })
    await ctx.close()
  })

  it('no hubo errores de consola ni violaciones de la política de contenido en todo el recorrido', () => {
    assert.deepEqual(consoleErrors, [], `errores en la consola del navegador:\n${consoleErrors.join('\n')}`)
  })
})
