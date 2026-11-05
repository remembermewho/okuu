// Итоги «Должников»: «Общий долг» = «Просрочено» + «Ожидают оплаты»; подсказка суммы со скидкой в форме ученика
// совпадает с тем, что потом начислено. Подготовка — через API, проверки — через интерфейс.
import { expect, test } from '@playwright/test'
import { apiOf, centerDow, centerToday, registerCenter } from './helpers'

const addDays = (d: string, n: number) => new Date(Date.parse(d + 'T00:00:00Z') + n * 86_400_000).toISOString().slice(0, 10)

test('общий долг = просрочено + ожидают; подсказка суммы со скидкой', async ({ page }) => {
  const today = centerToday()
  // Просрочка — запись со вчерашнего дня; 1-го числа вчера — прошлый месяц, и срок считается от 1-го.
  test.skip(today.endsWith('-01'), '1-го числа начисление ещё не просрочено')

  const { centerId } = await registerCenter(page)
  const api = apiOf(page)
  const C = `/api/v1/c/${centerId}`
  await api.patch(`${C}/settings`, { due_days: 0 })
  const month = await api.post(`${C}/tariffs`, { name: 'Английский — месяц', kind: 'monthly', price: 300_000 })
  const pkg = await api.post(`${C}/tariffs`, { name: 'Робототехника — 8 занятий', kind: 'package', price: 400_000, lessons_count: 8 })
  const slot = [{ dow: centerDow(), start: '15:00', end: '16:30' }]
  const english = await api.post(`${C}/groups`, { name: 'English', schedule: slot, default_tariff_id: month.id })
  await api.post(`${C}/groups`, { name: 'Робототехника', schedule: slot, default_tariff_id: pkg.id })

  // Просрочено: месяц со вчерашнего дня, срок 0 дней → 3 000.
  const late = await api.post(`${C}/students`, { full_name: 'Айдана Токтосунова', parent: { name: 'Гульнара', phone: '0700 333 101' } })
  await api.post(`${C}/students/${late.id}/enrollments`, { group_id: english.id, start_date: addDays(today, -1), first_month: 'full' })
  // Ожидает оплаты: месяц с сегодняшнего дня — срок сегодня, 3 000 − 10% = 2 700.
  const soon = await api.post(`${C}/students`, {
    full_name: 'Элина Ким', parent: { name: 'Асель', phone: '0700 333 102' }, discount: { kind: 'percent', value: 10 },
  })
  await api.post(`${C}/students/${soon.id}/enrollments`, { group_id: english.id, start_date: today, first_month: 'full' })

  // Форма: суммы со скидкой — до сохранения.
  await page.goto(`/c/${centerId}/students`)
  await page.getByRole('button', { name: 'Добавить ученика' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Новый ученик' })
  await dialog.getByLabel('ФИО ученика').fill('Нурислам Жумабеков')
  await dialog.getByLabel('Имя родителя').fill('Бакыт')
  await dialog.getByLabel('Телефон родителя').fill('0700 333 103')
  // exact: подпись «Начислить полностью — … (скидка 10%)» тоже содержит слово «скидка».
  const discount = dialog.getByLabel('Скидка', { exact: true })
  await discount.selectOption('percent')
  await dialog.getByLabel('Размер скидки').fill('10')
  await dialog.getByLabel('Группа').selectOption({ label: 'English' })
  await expect(dialog.getByRole('radio', { name: /Начислить полностью\s+2\s700\sс \(скидка 10%\)/ })).toBeChecked()
  // Скидка больше 100% — суммы нет (не «0 с (скидка 100%)»), только ошибка под полем.
  await dialog.getByLabel('Размер скидки').fill('150')
  await expect(dialog.getByRole('radio', { name: 'Начислить полностью', exact: true })).toBeChecked()
  await expect(dialog.getByText(/скидка 100%/)).toHaveCount(0)
  await discount.selectOption('fixed')
  await dialog.getByLabel('Размер скидки').fill('500')
  await dialog.getByLabel('Группа').selectOption({ label: 'Робототехника' })
  await expect(dialog.getByText(/Абонемент на 8 занятий: начислится 3\s500\sс \(скидка 500\sс\)/)).toBeVisible()
  await dialog.getByRole('button', { name: 'Добавить ученика' }).click()
  await page.waitForURL(/\/students\/[0-9a-f-]{36}$/)
  // Начислено ровно то, что обещала подсказка.
  await expect(page.getByText(/^3\s500\sс$/).first()).toBeVisible()

  // Итоги: просрочено 3 000 (1), ожидают 2 700 + 3 500 = 6 200 (2), общий 9 200 (3).
  await page.goto(`/c/${centerId}/debtors`)
  const total = page.getByTestId('stat-total')
  await expect(total).toContainText('Общий долг')
  await expect(total).toContainText(/9\s200\sс/)
  await expect(total).toContainText('3 ученика')
  await expect(page.getByTestId('stat-overdue')).toContainText(/Просрочено.*3\s000\sс.*1 ученик/)
  await expect(page.getByTestId('stat-pending')).toContainText(/Ожидают оплаты.*6\s200\sс.*2 ученика/)
  await expect(page.getByTestId('stat-collected')).toContainText('за вычетом возвратов')
  await expect(page.getByRole('tab', { name: /Просрочено/ })).toContainText('1')
  await expect(page.getByRole('tab', { name: /Ожидают оплаты/ })).toContainText('2')
})
