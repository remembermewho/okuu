// Бэкенд для e2e: чистая база okuu_e2e, сервер на :8091. Запускает Playwright (webServer) и сам же
// его останавливает. Рабочую базу okuu и сервер на :8080 не трогает. Telegram выключен — реальный бот
// в e2e не участвует.
//
// Скриншоты для лендинга (playwright.screenshots.config.ts): своя база (E2E_DB) и демо-центр —
// `server demo seed` с E2E_DEMO_PHONE / E2E_DEMO_PASSWORD перед запуском сервера.
//
// Postgres: локально — dev-контейнер `okuu-db` (docker exec psql, порт 5433); в CI — сервис postgres:
// E2E_PG_URL=postgres://user:pass@host:port (без имени базы), psql — клиент на машине.
import { execFileSync, spawn } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const backend = join(here, '..', '..', 'backend')
const container = process.env.E2E_PG_CONTAINER ?? 'okuu-db'
const pgURL = process.env.E2E_PG_URL?.replace(/\/+$/, '')
const db = process.env.E2E_DB ?? 'okuu_e2e'
if (!/^okuu_[a-z0-9_]+$/.test(db)) throw new Error(`E2E_DB=${db}: ожидается okuu_<имя>`) // имя идёт в SQL
const port = process.env.E2E_API_PORT ?? '8091'
const origin = process.env.E2E_ORIGIN ?? 'http://localhost:5174'

function psql(sql) {
  const args = ['-v', 'ON_ERROR_STOP=1', '-q', '-c', sql]
  if (pgURL) execFileSync('psql', [`${pgURL}/postgres`, ...args], { stdio: 'inherit' })
  else execFileSync('docker', ['exec', container, 'psql', '-U', 'okuu', '-d', 'postgres', ...args], { stdio: 'inherit' })
}

// 1. Чистая база: каждый прогон e2e — с нуля.
psql(`DROP DATABASE IF EXISTS ${db} WITH (FORCE)`)
psql(`CREATE DATABASE ${db}`)

// 2. Свежий бинарник.
mkdirSync(join(backend, 'bin'), { recursive: true })
const bin = join(backend, 'bin', process.platform === 'win32' ? 'okuu-e2e.exe' : 'okuu-e2e')
execFileSync('go', ['build', '-o', bin, './cmd/server'], { cwd: backend, stdio: 'inherit' })

// 3. Сервер. Переменные окружения сильнее .env (его dev-загрузчик не перезаписывает заданное).
const env = {
  ...process.env,
  APP_ENV: 'dev',
  HTTP_ADDR: ':' + port,
  APP_ORIGIN: origin,
  DATABASE_URL: `${pgURL ?? 'postgres://okuu:okuu_dev_password@localhost:5433'}/${db}?sslmode=disable`,
  TELEGRAM_MODE: 'off',
  TELEGRAM_BOT_USERNAME: 'okuu_e2e_bot',
  ALERT_TELEGRAM_CHAT_ID: '',
  FINIK_ENABLED: 'false',
}
if (process.env.E2E_DEMO_PHONE) {
  const demo = ['demo', 'seed', `--phone=${process.env.E2E_DEMO_PHONE}`, `--password=${process.env.E2E_DEMO_PASSWORD ?? ''}`]
  execFileSync(bin, demo, { cwd: backend, env, stdio: ['ignore', 'ignore', 'inherit'] })
}
const child = spawn(bin, [], { cwd: backend, stdio: 'inherit', env })
const stop = () => child.kill()
process.on('SIGINT', stop)
process.on('SIGTERM', stop)
process.on('exit', stop)
child.on('exit', (code) => process.exit(code ?? 0))
