// Скриншоты для лендинга (npm run screenshots): экраны демо-центра на 390 и 1280px и макет чата с ботом.
// Сохраняются в landing/img в webp. Данные — только вымышленные из `server demo seed`.
import { execFileSync } from 'node:child_process'
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { expect, test, type Browser, type Page } from '@playwright/test'
import { centerToday } from '../helpers'
import { SHOTS } from './demo'

const OUT = join('..', 'landing', 'img')
const BIN = join('..', 'backend', 'bin', process.platform === 'win32' ? 'okuu-e2e.exe' : 'okuu-e2e')
const QUALITY = 0.82

const VIEWPORTS = [
  { size: 390, width: 390, height: 844, scale: 2 },
  { size: 1280, width: 1280, height: 800, scale: 1 },
]

/** ISO-день недели даты YYYY-MM-DD: 1 — понедельник … 7 — воскресенье. */
function dow(iso: string): number {
  const [y, m, d] = iso.split('-').map(Number)
  return ((new Date(Date.UTC(y!, m! - 1, d!)).getUTCDay() + 6) % 7) + 1
}

function addDays(iso: string, n: number): string {
  const [y, m, d] = iso.split('-').map(Number)
  return new Date(Date.UTC(y!, m! - 1, d! + n)).toISOString().slice(0, 10)
}

/** Последний понедельник или среда до сегодня: две группы, посещаемость отмечена. */
function attendanceDay(today: string): string {
  let d = addDays(today, -1)
  while (dow(d) !== 1 && dow(d) !== 3) d = addDays(d, -1)
  return d
}

/** Прошлый месяц YYYY-MM: в отчёте полный месяц. */
function prevMonth(today: string): string {
  const [y, m] = today.split('-').map(Number)
  return new Date(Date.UTC(y!, m! - 2, 1)).toISOString().slice(0, 7)
}

/** PNG → webp силами самого Chromium (canvas.toDataURL): без лишних зависимостей. */
async function saveWebp(browser: Browser, png: Buffer, name: string) {
  const page = await browser.newPage()
  const b64 = await page.evaluate(
    async ([src, q]) => {
      const img = new Image()
      img.src = src
      await img.decode()
      const c = document.createElement('canvas')
      c.width = img.naturalWidth
      c.height = img.naturalHeight
      c.getContext('2d')!.drawImage(img, 0, 0)
      return c.toDataURL('image/webp', q).split(',')[1]!
    },
    ['data:image/png;base64,' + png.toString('base64'), QUALITY] as const,
  )
  await page.close()
  writeFileSync(join(OUT, name + '.webp'), Buffer.from(b64, 'base64'))
}

async function settle(page: Page) {
  await page.waitForLoadState('networkidle')
  await page.evaluate(() => document.fonts.ready)
}

async function shoot(browser: Browser, page: Page, name: string) {
  await settle(page)
  await page.evaluate(() => window.scrollTo(0, 0)) // после перехода из списка прокрутка остаётся
  await saveWebp(browser, await page.screenshot({ animations: 'disabled', caret: 'hide' }), name)
}

test.beforeAll(() => mkdirSync(OUT, { recursive: true }))

for (const vp of VIEWPORTS) {
  test(`экраны демо ${vp.size}px`, async ({ browser }) => {
    const ctx = await browser.newContext({ viewport: { width: vp.width, height: vp.height }, deviceScaleFactor: vp.scale, hasTouch: vp.size < 1024 })
    const page = await ctx.newPage()
    await page.goto('/login')
    await page.getByLabel('Телефон').fill(SHOTS.phone)
    await page.getByLabel('Пароль', { exact: true }).fill(SHOTS.password)
    await page.getByRole('button', { name: 'Войти' }).click()
    await page.waitForURL(/\/c\/[^/]+\//)
    const center = /\/c\/([^/]+)\//.exec(page.url())![1]
    const today = centerToday()

    await page.goto(`/c/${center}/debtors`)
    await expect(page.getByText('Бекова Айпери').first()).toBeVisible()
    await shoot(browser, page, `debtors-${vp.size}`)

    await page.getByText('Жумабаев Эрлан').first().click()
    await page.waitForURL(/\/students\//)
    await expect(page.getByRole('heading', { name: 'Жумабаев Эрлан' })).toBeVisible()
    await shoot(browser, page, `student-${vp.size}`)

    await page.goto(`/c/${center}/today?date=${attendanceDay(today)}`)
    await expect(page.getByText('Английский A1').first()).toBeVisible()
    await shoot(browser, page, `today-${vp.size}`)

    await page.goto(`/c/${center}/report?month=${prevMonth(today)}`)
    await settle(page)
    await shoot(browser, page, `report-${vp.size}`)
    await ctx.close()
  })
}

// Картинка для соцсетей (og:image) — JPEG: webp понимают не все. Слоган — из landing/config.json.
test('картинка для соцсетей', async ({ browser }) => {
  const cfg = JSON.parse(readFileSync(join('..', 'landing', 'config.json'), 'utf8')) as { name: string; tagline: string }
  const shot = readFileSync(join(OUT, 'debtors-390.webp')).toString('base64')
  const font = readFileSync(join('node_modules', '@fontsource', 'golos-text', 'files', 'golos-text-cyrillic-700-normal.woff2')).toString('base64')
  const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;')
  // Предлог из 1–2 букв не остаётся в конце строки: «об оплате» переносится целиком.
  const nbsp = (s: string) => s.replace(/(^|\s)([а-яё]{1,2}) /gi, '$1$2 ')
  const ctx = await browser.newContext({ viewport: { width: 1200, height: 630 } })
  const page = await ctx.newPage()
  await page.setContent(`<!doctype html><html lang="ru"><style>
    @font-face { font-family: G; font-weight: 700; src: url(data:font/woff2;base64,${font}) format('woff2'); }
    * { margin: 0; box-sizing: border-box; }
    body { width: 1200px; height: 630px; background: #F4F5F1; font-family: G, sans-serif; display: flex; align-items: center; gap: 56px; padding: 0 72px; overflow: hidden; }
    .text { flex: 1; }
    .logo { display: flex; align-items: center; gap: 16px; font-size: 40px; font-weight: 700; color: #17201B; }
    h1 { margin-top: 36px; font-size: 54px; line-height: 1.12; color: #17201B; }
    h1 span { color: #1E6B47; }
    .phone { flex: none; width: 330px; margin-top: 210px; padding: 10px; border-radius: 40px; background: #17201B; }
    .phone img { display: block; width: 100%; border-radius: 30px; }
  </style><div class="text"><div class="logo"><svg width="56" height="56" viewBox="0 0 32 32"><rect width="32" height="32" rx="8" fill="#1E6B47"/><circle cx="16" cy="16" r="7" fill="none" stroke="#fff" stroke-width="3"/></svg>${esc(cfg.name)}</div>
    <h1>${nbsp(esc(cfg.tagline)).replace(' — ', ' — <span>')}</span></h1></div>
    <div class="phone"><img src="data:image/webp;base64,${shot}" alt=""></div></html>`)
  await page.evaluate(() => document.fonts.ready)
  writeFileSync(join(OUT, 'og.jpg'), await page.screenshot({ type: 'jpeg', quality: 86 }))
  await ctx.close()
})

test('сообщение бота', async ({ browser }) => {
  // Настоящее напоминание: тот же код, что и рассылка (backend/internal/demo/message.go).
  const out = execFileSync(BIN, ['demo', 'message'], { encoding: 'utf8' })
  const msg = JSON.parse(out.split('\n').find((l) => l.startsWith('{'))!) as { text: string; buttons: string[] }
  const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  const font = (w: number) =>
    readFileSync(join('node_modules', '@fontsource', 'golos-text', 'files', `golos-text-cyrillic-${w}-normal.woff2`)).toString('base64')
  const html = readFileSync(join('e2e', 'screenshots', 'bot-chat.html'), 'utf8')
    .replace('{{FONT_400}}', font(400))
    .replace('{{FONT_600}}', font(600))
    .replace('{{BOT}}', 'Окуу')
    .replace('{{INITIAL}}', 'О')
    .replace('{{TIME}}', '10:00')
    .replace('{{TEXT}}', esc(msg.text))
    .replace('{{BUTTONS}}', msg.buttons.map((b) => `<div class="btn">${esc(b)}</div>`).join(''))

  const ctx = await browser.newContext({ viewport: { width: 390, height: 900 }, deviceScaleFactor: 2 })
  const page = await ctx.newPage()
  await page.setContent(html)
  await page.evaluate(() => document.fonts.ready)
  await saveWebp(browser, await page.locator('.chat').screenshot({ animations: 'disabled' }), 'bot-390')
  await ctx.close()
})
