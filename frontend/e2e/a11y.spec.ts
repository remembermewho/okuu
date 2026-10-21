// Доступность основных экранов (axe-core): критических нарушений нет. Скриншоты экранов (390 и 1280px —
// по проекту) прикладываются к отчёту Playwright; серьёзные (serious) нарушения — тоже, для разбора.
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { apiOf, centerDow, centerToday, registerCenter, seedPayLink } from './helpers'

async function check(page: Page, info: TestInfo, name: string) {
  await page.waitForLoadState('networkidle')
  // Ждём, пока уйдут скелетоны загрузки.
  await expect(page.locator('[aria-busy="true"]')).toHaveCount(0)
  await info.attach(`${name}.png`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  const serious = result.violations.filter((v) => v.impact === 'serious')
  if (serious.length) {
    await info.attach(`${name}-axe-serious.json`, { body: JSON.stringify(serious.map((v) => ({ id: v.id, help: v.help, nodes: v.nodes.map((n) => n.target) })), null, 2), contentType: 'application/json' })
  }
  const critical = result.violations.filter((v) => v.impact === 'critical')
  expect(critical.map((v) => `${v.id}: ${v.help} → ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`), `${name}: критические нарушения`).toEqual([])
}

test('основные экраны: нет критических нарушений доступности', async ({ page, browser }, info) => {
  test.setTimeout(120_000)
  await page.goto('/login')
  await check(page, info, 'login')

  const { centerId } = await registerCenter(page)
  const api = apiOf(page)
  const C = `/api/v1/c/${centerId}`
  const tariff = await api.post(`${C}/tariffs`, { name: 'Английский — месяц', kind: 'monthly', price: 300000 })
  const group = await api.post(`${C}/groups`, { name: 'Английский A1', schedule: [{ dow: centerDow(), start: '15:00', end: '16:30' }], default_tariff_id: tariff.id })
  const start = `${centerToday().slice(0, 7)}-01`
  let firstId = ''
  for (const [i, name] of ['Асанов Бекзат', 'Бекова Айжан', 'Сыдыков Эрлан'].entries()) {
    const st = await api.post(`${C}/students`, { full_name: name, parent: { name: 'Родитель', phone: `0700 222 33${i}` } })
    await api.post(`${C}/students/${st.id}/enrollments`, { group_id: group.id, start_date: start, first_month: 'full' })
    firstId ||= st.id
  }
  await api.post(`${C}/students/${firstId}/payments`, { kind: 'payment', amount: 100000, method: 'cash' })

  const screens: [string, string][] = [
    ['debtors', 'debtors'],
    ['students', 'students'],
    ['student-card', `students/${firstId}`],
    ['today', 'today'],
    ['groups', 'groups'],
    ['group', `groups/${group.id}`],
    ['tariffs', 'tariffs'],
    ['staff', 'staff'],
    ['import', 'import'],
    ['report', 'report'],
    ['settings', 'settings'],
    ['audit', 'audit'],
    ['more', 'more'],
  ]
  for (const [name, path] of screens) {
    await page.goto(`/c/${centerId}/${path}`)
    await check(page, info, name)
  }

  // Вкладка «Ожидают оплаты», профиль.
  await page.goto(`/c/${centerId}/debtors?tab=pending`)
  await check(page, info, 'debtors-pending')
  await page.goto('/account')
  await check(page, info, 'account')

  // Окно оплаты (диалог) — тоже.
  await page.goto(`/c/${centerId}/students/${firstId}`)
  await page.getByRole('button', { name: 'Отметить оплату' }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await check(page, info, 'payment-dialog')

  const token = seedPayLink(centerId, firstId, 200000)
  const ctx = await browser.newContext({ viewport: page.viewportSize() ?? undefined, locale: 'ru-RU' })
  const parent = await ctx.newPage()
  await parent.goto(`/pay/${token}`)
  await expect(parent.getByText('Асанов Бекзат')).toBeVisible()
  await check(parent, info, 'pay')
  await ctx.close()
})
