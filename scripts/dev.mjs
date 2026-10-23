// Запуск для разработки одной командой: `make dev` или `npm run dev` в корне (PowerShell, Git Bash, macOS, Linux).
//   1. docker compose up -d db и ожидание готовности PostgreSQL;
//   2. параллельно: API (go run, :8080), приложение (Vite, :5173), лендинг (landing/build.mjs --serve, :5180);
//   3. логи с префиксами [api] [app] [landing]; Ctrl+C останавливает всё; упал один — останавливаются все.
// Только стандартная библиотека Node 20+. Порты и база — из окружения и .env, как обычно:
//   HTTP_ADDR / APP_ORIGIN / DATABASE_URL — бэкенд, VITE_PORT / VITE_API_TARGET — Vite, LANDING_PORT — лендинг.
import { execFileSync, execSync, spawn, spawnSync } from 'node:child_process'
import { copyFileSync, existsSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { createInterface } from 'node:readline'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const win = process.platform === 'win32'
const color = process.stdout.isTTY && !process.env.NO_COLOR
const paint = (code, s) => (color ? `\x1b[${code}m${s}\x1b[0m` : s)
const say = (s) => console.log(paint('1', '[dev] ') + s)

const apiPort = (process.env.HTTP_ADDR ?? ':8080').split(':').pop() || '8080'
const appPort = process.env.VITE_PORT ?? '5173'
const landingPort = process.env.LANDING_PORT ?? '5180'

function fail(msg) {
  console.error(paint('31', '[dev] ') + msg)
  process.exit(1)
}

function have(cmd, args) {
  return spawnSync(cmd, args, { stdio: 'ignore' }).status === 0
}

// --- проверки окружения
if (!have('docker', ['--version'])) fail('нет Docker — установите Docker Desktop и запустите его')
if (!have('go', ['version'])) fail('нет Go 1.23+ — https://go.dev/dl/')
if (!existsSync(join(root, '.env'))) {
  copyFileSync(join(root, '.env.example'), join(root, '.env'))
  say('.env не было — создан из .env.example (секреты при необходимости допишите сами)')
}
if (!existsSync(join(root, 'frontend', 'node_modules'))) {
  say('frontend/node_modules нет — npm ci (один раз)…')
  execSync('npm ci', { cwd: join(root, 'frontend'), stdio: 'inherit' }) // npm на Windows — npm.cmd: через оболочку
}

// --- база
say('поднимаю PostgreSQL (docker compose up -d db)…')
try {
  execFileSync('docker', ['compose', 'up', '-d', 'db'], { cwd: root, stdio: 'inherit' })
} catch {
  fail('docker compose up -d db не удался — Docker Desktop запущен?')
}
const deadline = Date.now() + 60_000
while (!have('docker', ['compose', 'exec', '-T', 'db', 'pg_isready', '-U', 'okuu', '-d', 'okuu'])) {
  if (Date.now() > deadline) fail('PostgreSQL не ответил за 60 секунд — docker compose logs db')
  await new Promise((r) => setTimeout(r, 1000))
}
say('PostgreSQL готов')

// --- процессы. npm — одной строкой через оболочку (на Windows это npm.cmd), остальные — напрямую.
const procs = [
  { name: 'api', code: '36', cmd: 'go', args: ['run', './cmd/server'], cwd: join(root, 'backend') },
  { name: 'app', code: '35', cmd: `npm run dev -- --port ${Number(appPort)} --strictPort`, args: [], cwd: join(root, 'frontend'), shell: true },
  { name: 'landing', code: '33', cmd: process.execPath, args: [join(root, 'landing', 'build.mjs'), `--serve=${landingPort}`], cwd: root },
]
const width = Math.max(...procs.map((p) => p.name.length))
let stopping = false

function pipe(stream, prefix) {
  createInterface({ input: stream }).on('line', (line) => console.log(prefix + line))
}

// Остановить процесс вместе с детьми: go run запускает собранный сервер, npm — vite.
function kill(p) {
  if (!p.child || p.child.exitCode !== null || p.child.signalCode !== null) return
  if (win) spawnSync('taskkill', ['/PID', String(p.child.pid), '/T', '/F'], { stdio: 'ignore' })
  else {
    try {
      process.kill(-p.child.pid, 'SIGTERM') // группа процессов (detached)
    } catch {
      p.child.kill('SIGTERM')
    }
  }
}

function stopAll(code) {
  if (stopping) return
  stopping = true
  say('останавливаю…')
  procs.forEach(kill)
  setTimeout(() => process.exit(code), 500)
}

for (const p of procs) {
  const prefix = paint(p.code, `[${p.name.padEnd(width)}] `)
  p.child = spawn(p.cmd, p.args, { cwd: p.cwd, env: { ...process.env, VITE_PORT: appPort }, shell: p.shell ?? false, detached: !win })
  pipe(p.child.stdout, prefix)
  pipe(p.child.stderr, prefix)
  p.child.on('exit', (code, signal) => {
    // Ctrl+C в консоли получают и дети — они могут выйти раньше, чем сработает SIGINT здесь.
    setTimeout(() => {
      if (stopping) return
      console.error(paint('31', `[dev] ${p.name} остановился (${signal ?? 'код ' + code}) — останавливаю остальные`))
      stopAll(code || 1)
    }, 300)
  })
}

process.on('SIGINT', () => stopAll(0))
process.on('SIGTERM', () => stopAll(0))

say(`приложение  http://localhost:${appPort}`)
say(`лендинг     http://localhost:${landingPort}`)
say(`API         http://localhost:${apiPort}  (через приложение — /api, прокси Vite)`)
say('Ctrl+C — остановить всё')
