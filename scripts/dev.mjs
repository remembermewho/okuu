// Запуск для разработки одной командой: `make dev` или `npm run dev` в корне (PowerShell, Git Bash, macOS, Linux).
//   1. docker compose up -d db и ожидание готовности PostgreSQL;
//   2. параллельно: API (go run, :8080), приложение (Vite, :5173), лендинг (landing/build.mjs --serve, :5180);
//   3. логи с префиксами [api] [app] [landing]; Ctrl+C останавливает всё; упал один — останавливаются все.
// Только стандартная библиотека Node 20+. Порты и база — из окружения и .env, как обычно:
//   HTTP_ADDR / APP_ORIGIN / DATABASE_URL — бэкенд, VITE_PORT / VITE_API_TARGET — Vite, LANDING_PORT — лендинг.
import { execFileSync, execSync, spawn, spawnSync } from 'node:child_process'
import { copyFileSync, existsSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { connect } from 'node:net'
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
  // Не через npm.cmd (batch): Ctrl+C в batch-файле спрашивает «Завершить выполнение пакетного файла [Y/N]?».
  const npmCli = join(dirname(process.execPath), 'node_modules', 'npm', 'bin', 'npm-cli.js')
  if (existsSync(npmCli)) execFileSync(process.execPath, [npmCli, 'ci'], { cwd: join(root, 'frontend'), stdio: 'inherit' })
  else execSync('npm ci', { cwd: join(root, 'frontend'), stdio: 'inherit' })
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

// --- процессы. Все — напрямую исполняемым файлом (go, node), без npm/npx и без shell: у .cmd-обёрток
// на Windows Ctrl+C вызывает вопрос «Завершить выполнение пакетного файла [Y/N]?».
const vite = join(root, 'frontend', 'node_modules', 'vite', 'bin', 'vite.js')
const procs = [
  { name: 'api', code: '36', cmd: 'go', args: ['run', './cmd/server'], cwd: join(root, 'backend'), port: apiPort },
  { name: 'app', code: '35', cmd: process.execPath, args: [vite, '--port', appPort, '--strictPort'], cwd: join(root, 'frontend'), port: appPort },
  { name: 'landing', code: '33', cmd: process.execPath, args: [join(root, 'landing', 'build.mjs'), `--serve=${landingPort}`], cwd: root, port: landingPort },
]
const width = Math.max(...procs.map((p) => p.name.length))
let stopping = false

function pipe(stream, prefix) {
  createInterface({ input: stream }).on('line', (line) => console.log(prefix + line))
}

const running = (p) => p.child && p.child.exitCode === null && p.child.signalCode === null

// Остановить свой процесс вместе с его деревом: go run запускает собранный сервер. Только по своим PID.
function kill(p) {
  if (!running(p)) return
  if (win) spawnSync('taskkill', ['/PID', String(p.child.pid), '/T', '/F'], { stdio: 'ignore' })
  else {
    try {
      process.kill(-p.child.pid, 'SIGTERM') // группа процессов (detached)
    } catch {
      p.child.kill('SIGTERM')
    }
  }
}

// Порт свободен — никто не принимает соединения ни на IPv4, ни на IPv6 (Vite слушает localhost → ::1).
function listening(port, host) {
  return new Promise((resolve) => {
    const s = connect({ port: Number(port), host })
    s.setTimeout(500)
    s.once('connect', () => (s.destroy(), resolve(true)))
    s.once('error', () => resolve(false))
    s.once('timeout', () => (s.destroy(), resolve(false)))
  })
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function stopAll(code) {
  if (stopping) return
  stopping = true
  say('останавливаю…')
  procs.forEach(kill)
  // Ждём выхода своих процессов и освобождения их портов: сервер, собранный go run, мог получить Ctrl+C сам
  // и ещё завершается (если go.exe к этому моменту уже вышел, его дерево taskkill не достанет).
  const deadline = Date.now() + 25_000
  for (;;) {
    const busy = []
    for (const p of procs) {
      if (running(p) || (await listening(p.port, '127.0.0.1')) || (await listening(p.port, '::1'))) busy.push(p.name)
    }
    if (busy.length === 0) break
    if (Date.now() > deadline) {
      console.error(paint('31', `[dev] не остановились за 25 секунд: ${busy.join(', ')}`))
      process.exit(1)
    }
    procs.forEach(kill)
    await sleep(300)
  }
  say('все процессы остановлены')
  process.exit(code)
}

for (const p of procs) {
  const prefix = paint(p.code, `[${p.name.padEnd(width)}] `)
  p.child = spawn(p.cmd, p.args, { cwd: p.cwd, env: { ...process.env, VITE_PORT: appPort }, detached: !win })
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
