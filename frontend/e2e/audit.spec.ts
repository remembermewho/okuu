// Журнал на настоящем API: строка оплаты — с учеником (ссылкой на карточку) и суммой в сомах; «Подробнее» —
// по-русски, без id и тыйынов; сырой JSON — по кнопке «Технические данные» (владелец).
import { expect, test } from '@playwright/test'
import { apiOf, centerDow, centerToday, registerCenter } from './helpers'

test('журнал: ученик и сумма в строке, «Подробнее» по-русски, технические данные по кнопке', async ({ page }) => {
  const { centerId } = await registerCenter(page)
  const api = apiOf(page)
  const C = `/api/v1/c/${centerId}`
  const month = await api.post(`${C}/tariffs`, { name: 'Английский — месяц', kind: 'monthly', price: 350_000 })
  const english = await api.post(`${C}/groups`, { name: 'English', schedule: [{ dow: centerDow(), start: '15:00', end: '16:30' }], default_tariff_id: month.id })
  const st = await api.post(`${C}/students`, { full_name: 'Айдана Токтосунова', parent: { name: 'Гульнара', phone: '0700 333 201' } })
  await api.post(`${C}/students/${st.id}/enrollments`, { group_id: english.id, start_date: centerToday(), first_month: 'full' })
  await api.post(`${C}/students/${st.id}/payments`, { kind: 'payment', amount: 150_000, method: 'cash' })

  await page.goto(`/c/${centerId}/audit?action=ledger.`)
  const row = page.getByRole('listitem').filter({ hasText: /^Оплата/ })
  await expect(row.getByRole('link', { name: 'Айдана Токтосунова' })).toHaveAttribute('href', `/c/${centerId}/students/${st.id}`)
  await expect(row.getByTestId('audit-subject')).toContainText(/1\s500\sс/)
  const charge = page.getByRole('listitem').filter({ hasText: /^Начисление/ })
  await expect(charge.getByTestId('audit-subject')).toContainText(/−3\s500\sс/)

  await row.getByText('Подробнее').click()
  const table = row.getByRole('table')
  await expect(table.getByRole('row', { name: /Способ\s+Наличные/ })).toBeVisible()
  await expect(table.getByRole('row', { name: /Сумма\s+1\s500\sс/ })).toBeVisible()
  await expect(table).not.toContainText(st.id)
  await expect(table).not.toContainText('150000')
  await expect(row.getByText(/"student_id"/)).toHaveCount(0)
  await row.getByRole('button', { name: 'Технические данные' }).click()
  await expect(row.getByText(/"student_id"/)).toBeVisible()

  // Ссылка ведёт в карточку ученика.
  await row.getByRole('link', { name: 'Айдана Токтосунова' }).click()
  await expect(page).toHaveURL(new RegExp(`/students/${st.id}$`))
})
