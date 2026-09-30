// Pruebas de extremo a extremo del panel de control (/admin): un relé real (RELAY_BIN) y un Chromium real
// (Playwright), con la extensión NIP-07 (nos2x) simulada. Ejecutar: RELAY_BIN=../../nostr-relay-khatru npm test
// Cubre: entrada y sesión, resumen, ayuda, histórico, moderación, búsqueda y que la política de contenido
// estricta del panel no bloquee nada (ni una sola violación en la consola).
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { after, before, describe, it } from 'node:test'
import { chromium } from 'playwright'
import WebSocket from 'ws'
import { Relay, useWebSocketImplementation } from 'nostr-tools/relay'
import { finalizeEvent, generateSecretKey, getPublicKey } from 'nostr-tools/pure'
import * as nip19 from 'nostr-tools/nip19'
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
async function openPanel({ signWith = () => stack.ownerSecret, extension = true } = {}) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 1000 }, locale: 'es-ES', timezoneId: 'Europe/Madrid' })
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

async function login(page) {
  await page.click('#login-btn')
  try {
    await page.waitForSelector('#dash:not([hidden])', { timeout: 10000 })
  } catch (e) {
    throw new Error(`no se pudo entrar: «${await page.innerText('#login-msg')}»`)
  }
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
async function refreshUntil(page, cond) {
  for (let i = 0; i < 25; i++) {
    if (await page.evaluate(cond)) return
    await sleep(1000)
    await page.click('#refresh')
  }
  throw new Error('el panel no llegó a mostrar los datos esperados')
}

const newKey = () => { const sk = generateSecretKey(); return { sk, pk: getPublicKey(sk) } }

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
    await login(page)
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
    await pub(someone.sk, 1, 'x'.repeat(300)) // rechazada por tamaño: aparece en Rechazos
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

    for (const id of ['h-cards', 'h-activity', 'h-growth', 'h-kinds', 'h-rejections', 'h-recent', 'h-search', 'h-moderation', 'h-config']) {
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
    await page.fill('#f-info [name=description]', 'escribiendo…')
    await page.click('#refresh')
    await sleep(700)
    assert.equal(await page.inputValue('#f-info [name=description]'), 'escribiendo…', 'el refresco automático no debe borrar lo que estás escribiendo')
    assert.match(await clickAndToast(page, '#info-reset'), /Restaurados/)
    const doc2 = await (await fetch(stack.relayHttp, { headers: { Accept: 'application/nostr+json' } })).json()
    assert.equal(doc2.name, 'Relé de pruebas')
    assert.equal(doc2.description, 'Descripción original')
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
    await pub(bea.sk, 1, 'y'.repeat(300)) // rechazada: da una clave abreviada en Rechazos

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

  it('no hubo errores de consola ni violaciones de la política de contenido en todo el recorrido', () => {
    assert.deepEqual(consoleErrors, [], `errores en la consola del navegador:\n${consoleErrors.join('\n')}`)
  })
})
