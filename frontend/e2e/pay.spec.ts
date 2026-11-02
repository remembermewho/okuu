// Ручная оплата: владелец пишет реквизиты и инструкцию (предпросмотр как в Telegram), родитель платит сам и
// присылает чек, администратор отмечает оплату прямо из «Должников». Онлайн-оплата выключена
// (ONLINE_PAYMENTS_ENABLED=false): страница /pay/<токен> — «не подключена».
import { expect, test } from '@playwright/test'
import { apiOf, registerCenter, seedPayLink } from './helpers'

test('реквизиты с предпросмотром, «Отметить оплату» из «Должников», онлайн-оплаты нет', async ({ page, browser }) => {
  const { centerId } = await registerCenter(page)
  const api = apiOf(page)
  const C = `/api/v1/c/${centerId}`
  const st = await api.post(`${C}/students`, { full_name: 'Асанов Бекзат', parent: { name: 'Гульнара', phone: '0700 111 222' } })
  // Долг 2 500 сом со сроком сегодня — «Ожидают оплаты».
  await api.post(`${C}/students/${st.id}/payments`, { kind: 'correction', amount: -250000, comment: 'Долг за сентябрь' })

  // Реквизитов нет — плашка на «Должниках» со ссылкой в настройки.
  await page.goto(`/c/${centerId}/debtors?tab=pending`)
  await expect(page.getByText(/Заполните реквизиты для оплаты/)).toBeVisible()
  await page.getByRole('link', { name: 'Заполнить в настройках' }).click()
  await page.waitForURL(/\/settings$/)

  // Реквизиты и инструкция — многострочно; предпросмотр «Так увидит родитель в Telegram».
  const field = page.getByLabel(/Реквизиты и инструкция для оплаты/)
  await field.fill('Оплата на MBank: 0700 123 456 (Айгуль А.).\nПосле оплаты отправьте чек администратору в WhatsApp: +996 700 123 456.')
  const preview = page.getByTestId('pay-preview')
  await expect(preview).toContainText('Как оплатить:')
  await expect(preview).toContainText('После оплаты отправьте чек администратору в WhatsApp')
  await expect(page.getByText(/^\d+ \/ 1000$/)).toBeVisible()
  await page.getByRole('button', { name: 'Сохранить' }).click()
  await expect(page.getByText('Настройки сохранены')).toBeVisible()

  // «Должники»: плашки нет; «Отметить оплату» — сумма долга и «перевод» уже подставлены.
  await page.goto(`/c/${centerId}/debtors?tab=pending`)
  await expect(page.getByText('Асанов Бекзат')).toBeVisible()
  await expect(page.getByText(/Заполните реквизиты для оплаты/)).toHaveCount(0)
  await page.getByRole('button', { name: 'Отметить оплату: Асанов Бекзат' }).click()
  const dialog = page.getByRole('dialog', { name: 'Оплата' })
  await expect(dialog.getByLabel('Сумма')).toHaveValue('2500')
  await expect(dialog.getByRole('radio', { name: 'Перевод' })).toBeChecked()
  await dialog.getByRole('button', { name: 'Отметить оплату' }).click()
  await expect(page.getByText('Оплата отмечена, родитель не подключён к Telegram')).toBeVisible()
  await expect(page.getByText('Асанов Бекзат')).toHaveCount(0)

  // Онлайн-оплата выключена: даже настоящая ссылка — «не подключена», без суммы и данных ученика.
  const token = seedPayLink(centerId, st.id, 100000)
  const ctx = await browser.newContext({ viewport: page.viewportSize() ?? undefined, locale: 'ru-RU' })
  const parent = await ctx.newPage()
  await parent.goto(`/pay/${token}`)
  await expect(parent.getByRole('heading', { name: 'Онлайн-оплата не подключена' })).toBeVisible()
  await expect(parent.getByText('Онлайн-оплата не подключена, уточните способ оплаты у администратора центра.')).toBeVisible()
  await expect(parent.getByText('Асанов Бекзат')).toHaveCount(0)
  await expect(parent.getByRole('navigation')).toHaveCount(0)
  expect((await (await parent.request.get(`/api/v1/pay/${token}`)).json()).error.code).toBe('not_found')
  await ctx.close()
})
