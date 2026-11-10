// WhatsApp-кнопка, окно «ещё N без Telegram» и раздел «Рассылка» на настоящем API.
// В e2e Telegram выключен, поэтому все родители идут по ссылкам wa.me. Настоящий wa.me не открываем: новая вкладка перехватывается.
import { expect, test, type Page } from '@playwright/test'
import { apiOf, centerDow, centerToday, registerCenter } from './helpers'

async function seed(page: Page) {
  const { centerId } = await registerCenter(page)
  const api = apiOf(page)
  const C = `/api/v1/c/${centerId}`
  await api.patch(`${C}/settings`, { due_days: 0, pay_details: 'MBank 0700 111 222' })
  const month = await api.post(`${C}/tariffs`, { name: 'Английский — месяц', kind: 'monthly', price: 300_000 })
  const slot = [{ dow: centerDow(), start: '15:00', end: '16:30' }]
  const english = await api.post(`${C}/groups`, { name: 'English', schedule: slot, default_tariff_id: month.id })
  const stamp = String(Date.now() % 100_000).padStart(5, '0')
  const add = async (fullName: string, parent: string, tail: string) => {
    const s = await api.post(`${C}/students`, { full_name: fullName, parent: { name: parent, phone: `0701${stamp}${tail}` } })
    await api.post(`${C}/students/${s.id}/enrollments`, { group_id: english.id, start_date: centerToday(), first_month: 'full' })
    return s
  }
  await add('Айдана Токтосунова', 'Гульнара', '1')
  await add('Элина Ким', 'Асель', '2')
  return { centerId, stamp }
}

// Перехват: вкладка wa.me открывается, но внешний сайт не грузится.
const stubWhatsApp = (page: Page) => page.context().route(/https:\/\/(wa\.me|api\.whatsapp\.com)\//, (r) => r.fulfill({ status: 200, contentType: 'text/html', body: 'wa' }))

test('кнопка WhatsApp: ссылка с текстом, отметка «открыто в WhatsApp»', async ({ page, context }) => {
  const { centerId, stamp } = await seed(page)
  await stubWhatsApp(page)
  await page.goto(`/c/${centerId}/debtors?tab=pending`)

  const tabPromise = context.waitForEvent('page', (p) => p.url().includes('wa.me') || p.url() === 'about:blank')
  await page.getByRole('button', { name: 'WhatsApp: Айдана Токтосунова' }).click()
  const tab = await tabPromise
  await tab.waitForURL(/wa\.me/)
  const url = new URL(tab.url())
  expect(url.pathname).toBe(`/9967${'01'}${stamp}1`)
  const text = url.searchParams.get('text')!
  expect(text).toContain('*🏫 Центр «Билим Плюс»')
  expect(text).toContain('Айдана Токтосунова — 3 000 сом')
  expect(text).toContain('*Как оплатить:*\nMBank 0700 111 222')
  await tab.close()

  const row = page.getByRole('listitem').filter({ hasText: 'Айдана Токтосунова' })
  await expect(row.getByText('открыто в WhatsApp')).toBeVisible()
  await expect(page.getByRole('listitem').filter({ hasText: 'Элина Ким' }).getByText('открыто в WhatsApp')).toHaveCount(0)
})

test('«Напомнить всем»: окно «ещё N без Telegram», список не теряется', async ({ page }) => {
  const { centerId } = await seed(page)
  await stubWhatsApp(page)
  await page.goto(`/c/${centerId}/debtors?tab=pending`)
  await page.getByRole('button', { name: 'Напомнить всем' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Напомнить всем' }).click()

  const dialog = page.getByRole('dialog', { name: 'Ещё 2 родителя без Telegram — отправьте в WhatsApp' })
  await expect(dialog).toBeVisible()
  const row = dialog.getByRole('listitem').filter({ hasText: 'Айдана Токтосунова' })
  await expect(row).toContainText(/3\s000\sс/)

  const [tab] = await Promise.all([page.context().waitForEvent('page'), row.getByRole('link', { name: 'WhatsApp' }).click()])
  await tab.close()
  await expect(row.getByText('открыто', { exact: true })).toBeVisible()

  // Вкладку WhatsApp закрыли, страницу перезагрузили — окно и отметка на месте.
  await page.reload()
  await expect(dialog).toBeVisible()
  await expect(row.getByText('открыто', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('listitem').filter({ hasText: 'Элина Ким' }).getByText('открыто', { exact: true })).toHaveCount(0)
})

test('рассылка: предпросмотр, отправка, итоги, история и журнал; вторая за 5 минут — отказ', async ({ page }) => {
  const { centerId } = await seed(page)
  await stubWhatsApp(page)
  await page.goto(`/c/${centerId}/broadcasts`)

  const form = async (text: string) => {
    await page.getByLabel('Текст объявления').fill(text)
    await page.getByRole('button', { name: 'Предпросмотр' }).click()
    return page.getByRole('dialog', { name: 'Проверьте перед отправкой' })
  }
  const preview = await form('В понедельник занятий не будет')
  await expect(preview.getByTestId('broadcast-preview')).toContainText('В WhatsApp — вы отправите по ссылкам2')
  await expect(preview).toContainText('🏫 Центр «Билим Плюс»')
  await preview.getByRole('button', { name: 'Отправить' }).dblclick()

  const result = page.getByRole('dialog', { name: 'Итоги рассылки' })
  await expect(result.getByTestId('broadcast-stats')).toContainText('Ссылок WhatsApp открыто0 из 2')
  const row = result.getByRole('listitem').filter({ hasText: 'Гульнара' })
  const [tab] = await Promise.all([page.context().waitForEvent('page'), row.getByRole('link', { name: 'WhatsApp' }).click()])
  expect(new URL(tab.url()).searchParams.get('text')).toContain('В понедельник занятий не будет')
  await tab.close()
  await expect(row.getByText('открыто', { exact: true })).toBeVisible()
  await expect(result.getByTestId('broadcast-stats')).toContainText('Ссылок WhatsApp открыто1 из 2')

  // Перезагрузка: итоги — с сервера.
  await page.reload()
  await expect(result.getByTestId('broadcast-stats')).toContainText('Ссылок WhatsApp открыто1 из 2')
  await result.getByRole('button', { name: 'Закрыть' }).last().click()

  // Двойное нажатие дало одну рассылку.
  const history = page.getByRole('list', { name: 'История рассылок' })
  await expect(history.getByRole('listitem')).toHaveCount(1)
  await expect(history).toContainText('В понедельник занятий не будет')
  await expect(history).toContainText('WhatsApp открыто: 1 из 2')

  // Вторая сразу — понятный отказ.
  const again = await form('Ещё одно объявление')
  await again.getByRole('button', { name: 'Отправить' }).click()
  await expect(page.getByText('Рассылка уже была меньше 5 минут назад.', { exact: false })).toBeVisible()

  await page.goto(`/c/${centerId}/audit`)
  await expect(page.getByText('Рассылка отправлена')).toBeVisible()
  await expect(page.getByText('Рассылка: открыто в WhatsApp')).toBeVisible()
})
