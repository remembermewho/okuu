// Публичная страница оплаты B2: без входа и без меню; устаревшая ссылка — понятное сообщение.
import { expect, test } from '@playwright/test'
import { apiOf, registerCenter, seedPayLink } from './helpers'

test('страница оплаты по ссылке: сумма, ученик, реквизиты; устаревшая ссылка', async ({ page, browser }) => {
  const { centerId } = await registerCenter(page)
  const api = apiOf(page)
  const C = `/api/v1/c/${centerId}`
  await api.patch(`${C}/settings`, { pay_details: 'Mbank: 0700 123 456\nПолучатель: Айгуль А.' })
  const st = await api.post(`${C}/students`, { full_name: 'Асанов Бекзат', parent: { name: 'Гульнара', phone: '0700 111 222' } })
  await api.post(`${C}/students/${st.id}/payments`, { kind: 'correction', amount: -250000, comment: 'Долг за сентябрь' })
  const token = seedPayLink(centerId, st.id, 250000)

  // Родитель — без сессии сотрудника.
  const ctx = await browser.newContext({ viewport: page.viewportSize() ?? undefined, locale: 'ru-RU' })
  const parent = await ctx.newPage()
  await parent.goto(`/pay/${token}`)
  await expect(parent.getByRole('heading', { name: 'Оплата обучения' })).toBeVisible()
  await expect(parent.getByText('Билим Плюс')).toBeVisible()
  await expect(parent.getByText('Асанов Бекзат')).toBeVisible()
  await expect(parent.getByText(/2\s500\sс/)).toBeVisible()
  await expect(parent.getByText(/Mbank: 0700 123 456/)).toBeVisible()
  await expect(parent.getByRole('button', { name: 'Скопировать реквизиты' })).toBeVisible()
  await expect(parent.getByRole('navigation')).toHaveCount(0)

  await parent.goto('/pay/this-link-does-not-exist')
  await expect(parent.getByRole('heading', { name: 'Ссылка устарела' })).toBeVisible()
  await expect(parent.getByText(/Запросите новую ссылку в боте/)).toBeVisible()
  await ctx.close()
})
