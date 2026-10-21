// Общие шаги e2e: регистрация центра, данные через API, ссылка оплаты прямо в базе okuu_e2e.
import { execFileSync } from 'node:child_process'
import { createHash, randomBytes, randomUUID } from 'node:crypto'
import { expect, type APIRequestContext, type Page } from '@playwright/test'

export const API_PORT = 8091
export const FRONT_PORT = 5174
export const ORIGIN = `http://localhost:${FRONT_PORT}`
const container = process.env.E2E_PG_CONTAINER ?? 'okuu-db'
// В CI — прямое подключение к сервису postgres (см. e2e/start-backend.mjs).
const pgURL = process.env.E2E_PG_URL?.replace(/\/+$/, '')

/** «Сегодня» центра (Asia/Bishkek), YYYY-MM-DD — машина с тестами может жить в другом поясе (CI — UTC). */
export function centerToday(): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Bishkek', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date())
}

/** День недели центра: 1 — понедельник … 7 — воскресенье. */
export function centerDow(): number {
  const [y, m, d] = centerToday().split('-').map(Number)
  return ((new Date(Date.UTC(y!, m! - 1, d!)).getUTCDay() + 6) % 7) + 1
}

/** Уникальный телефон на прогон: база общая для проектов mobile/desktop. */
let seq = 0
export function phone(): string {
  seq += 1
  const n = (Date.now() % 1_000_000) * 10 + (seq % 10)
  return '0700' + String(n).padStart(6, '0').slice(-6)
}

/** Регистрация центра через форму; возвращает id центра. */
export async function registerCenter(page: Page, centerName = 'Билим Плюс', name = 'Айгуль Асанова'): Promise<{ centerId: string; phone: string }> {
  const ph = phone()
  await page.goto('/register')
  await page.getByLabel('Название центра').fill(centerName)
  await page.getByLabel('Ваше имя').fill(name)
  await page.getByLabel('Телефон').fill(ph)
  // exact: рядом кнопка-глаз «Показать пароль».
  await page.getByLabel('Пароль', { exact: true }).fill('secret-pass')
  await page.getByRole('button', { name: 'Создать центр' }).click()
  await page.waitForURL(/\/c\/[^/]+\/debtors/)
  const centerId = /\/c\/([^/]+)\//.exec(page.url())?.[1]
  expect(centerId).toBeTruthy()
  return { centerId: centerId!, phone: ph }
}

/** Изменяющие запросы API от имени вошедшего (cookie страницы); Origin — как у браузера. */
export function apiOf(page: Page) {
  const req: APIRequestContext = page.request
  const h = { Origin: ORIGIN }
  // Ответы API в шагах подготовки не типизируем: проверки — через интерфейс.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const ok = async (p: ReturnType<APIRequestContext['post']>): Promise<any> => {
    const r = await p
    expect(r.ok(), `${r.url()} → ${r.status()} ${await r.text()}`).toBeTruthy()
    return r.status() === 204 ? null : r.json()
  }
  return {
    get: (path: string) => ok(req.get(path)),
    // Idempotency-Key обязателен для оплат; остальным эндпоинтам не мешает.
    post: (path: string, data?: unknown) => ok(req.post(path, { headers: { ...h, 'Idempotency-Key': randomUUID() }, data })),
    patch: (path: string, data: unknown) => ok(req.patch(path, { headers: h, data })),
  }
}

function psql(sql: string): string {
  const args = ['-v', 'ON_ERROR_STOP=1', '-qtA', '-c', sql]
  const out = pgURL
    ? execFileSync('psql', [`${pgURL}/okuu_e2e`, ...args], { encoding: 'utf8' })
    : execFileSync('docker', ['exec', container, 'psql', '-U', 'okuu', '-d', 'okuu_e2e', ...args], { encoding: 'utf8' })
  return out.trim()
}

/**
 * Ссылка оплаты, как её выдаёт бот (Telegram в e2e выключен): в базе — hex(sha256) токена,
 * сумма — долг ученика. Возвращает сырой токен для адреса /pay/<токен>.
 */
export function seedPayLink(centerId: string, studentId: string, amount: number): string {
  const token = randomBytes(32).toString('base64url')
  const hash = createHash('sha256').update(token).digest('hex')
  const uuid = /^[0-9a-f-]{36}$/
  if (!uuid.test(centerId) || !uuid.test(studentId) || !Number.isSafeInteger(amount)) throw new Error('bad seed args')
  psql(
    `INSERT INTO payment_links (token, center_id, parent_id, student_id, amount, provider, expires_at)
     SELECT '${hash}', s.center_id, s.parent_id, s.id, ${amount}, 'manual', now() + interval '1 day'
     FROM students s WHERE s.id = '${studentId}' AND s.center_id = '${centerId}'`,
  )
  return token
}
