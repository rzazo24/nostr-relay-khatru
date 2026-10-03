// Monta lo que hace falta para probar el panel sin Docker ni Caddy: el binario del relé de verdad (RELAY_BIN)
// y un servidor pequeño que hace de Caddy: sirve static/ con la MISMA política de contenido que el Caddyfile
// (se lee de ahí, para que no se desincronicen) y reenvía /admin/api/* al relé.
import { spawn } from 'node:child_process'
import fs from 'node:fs'
import http from 'node:http'
import net from 'node:net'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { getPublicKey, generateSecretKey } from 'nostr-tools/pure'

const here = path.dirname(fileURLToPath(import.meta.url))
export const repoRoot = path.resolve(here, '../..')
const staticRoot = path.join(repoRoot, 'static')
const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png' }

async function freePort() {
  return new Promise((resolve) => {
    const s = net.createServer().listen(0, '127.0.0.1', () => { const { port } = s.address(); s.close(() => resolve(port)) })
  })
}

// La política de contenido de /admin, leída del Caddyfile.
function adminCSP() {
  const caddyfile = fs.readFileSync(path.join(repoRoot, 'Caddyfile'), 'utf8')
  const m = caddyfile.match(/handle @adminpage[\s\S]*?Content-Security-Policy "([^"]+)"/)
  if (!m) throw new Error('no se encontró la política de contenido de /admin en el Caddyfile')
  return m[1]
}

export async function startStack(extraEnv = {}) {
  const bin = process.env.RELAY_BIN ? path.resolve(process.env.RELAY_BIN) : path.join(repoRoot, 'nostr-relay-khatru')
  if (!fs.existsSync(bin)) throw new Error(`no existe el binario del relé (${bin}): compílalo o define RELAY_BIN`)
  const ownerSecret = generateSecretKey()
  const ownerPk = getPublicKey(ownerSecret)
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'panel-e2e-'))
  const dbPath = path.join(dir, 'relay.sqlite')
  const backupDir = path.join(dir, 'backups')
  fs.mkdirSync(backupDir)
  const backupFile = path.join(backupDir, 'nostr-relay-khatru-20261001T031701Z.sqlite.gz')
  fs.writeFileSync(backupFile, Buffer.alloc(2048)) // una copia «reciente» de mentira
  const relayPort = await freePort()
  const panelPort = await freePort()

  const relay = spawn(bin, [], {
    env: {
      ...process.env,
      RELAY_DB_PATH: dbPath, RELAY_LISTEN_ADDR: `127.0.0.1:${relayPort}`, RELAY_PUBKEY: ownerPk,
      RELAY_NAME: 'Relé de pruebas', RELAY_DESCRIPTION: 'Descripción original',
      // límites de velocidad altos: las pruebas publican muchos eventos seguidos
      RELAY_EVENTS_PER_MINUTE: '1000000', RELAY_EVENTS_BURST: '1000000', RELAY_REQS_PER_MINUTE: '1000000', RELAY_REQS_BURST: '1000000', RELAY_CONNS_PER_MINUTE: '1000000', RELAY_CONNS_BURST: '1000000',
      RELAY_MAX_CONTENT_LENGTH: '800', RELAY_RETENTION_DAYS: '180', RELAY_BACKUP_DIR: backupDir,
      ...extraEnv,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  let relayLog = ''
  relay.stdout.on('data', (d) => { relayLog += d })
  relay.stderr.on('data', (d) => { relayLog += d })

  const csp = adminCSP()
  const server = http.createServer((req, res) => {
    if (req.url.startsWith('/admin/api/')) {
      const up = http.request({ host: '127.0.0.1', port: relayPort, path: req.url, method: req.method, headers: req.headers }, (r) => { res.writeHead(r.statusCode, r.headers); r.pipe(res) })
      up.on('error', () => { res.writeHead(502); res.end() })
      req.pipe(up)
      return
    }
    let f = req.url.split('?')[0]
    const isAdmin = f === '/admin' || f === '/admin/' || f.startsWith('/admin/')
    if (f === '/admin' || f === '/admin/') f = '/admin/index.html'
    const file = path.join(staticRoot, f)
    if (!file.startsWith(staticRoot) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) { res.writeHead(404); res.end('no'); return }
    const headers = { 'Content-Type': types[path.extname(file)] || 'text/plain', 'Cache-Control': 'no-cache' }
    if (isAdmin) headers['Content-Security-Policy'] = csp
    res.writeHead(200, headers)
    fs.createReadStream(file).pipe(res)
  }).listen(panelPort, '127.0.0.1')
  // Como Caddy en producción, el WebSocket del relé cuelga de la misma dirección que el panel (el panel NIP-46 conecta a `ws://<su host>/`).
  server.on('upgrade', (req, socket, head) => {
    const up = net.connect(relayPort, '127.0.0.1', () => {
      let raw = `${req.method} ${req.url} HTTP/1.1\r\n`
      for (let i = 0; i < req.rawHeaders.length; i += 2) raw += `${req.rawHeaders[i]}: ${req.rawHeaders[i + 1]}\r\n`
      up.write(raw + '\r\n')
      up.write(head)
      socket.pipe(up).pipe(socket)
    })
    up.on('error', () => socket.destroy())
    socket.on('error', () => up.destroy())
  })

  // esperar a que el relé conteste
  const relayHttp = `http://127.0.0.1:${relayPort}`
  for (let i = 0; i < 60; i++) {
    try {
      const r = await fetch(relayHttp, { headers: { Accept: 'application/nostr+json' } })
      if (r.ok) break
    } catch { /* aún no */ }
    await new Promise((r) => setTimeout(r, 250))
    if (i === 59) throw new Error(`el relé no arrancó:\n${relayLog}`)
  }

  return {
    ownerSecret, ownerPk, dbPath, backupFile,
    relayWs: `ws://127.0.0.1:${relayPort}`, relayHttp, panelUrl: `http://127.0.0.1:${panelPort}/admin/`,
    log: () => relayLog,
    async stop() {
      server.close()
      relay.kill()
      await new Promise((r) => setTimeout(r, 200))
      fs.rmSync(dir, { recursive: true, force: true })
    },
  }
}
