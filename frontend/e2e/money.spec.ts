// Главный сценарий: центр → тариф → группа → ученик с первым месяцем → должник → оплата → не должник →
// отмена оплаты → снова должник. Всё через интерфейс, на настоящем бэкенде.
import { expect, test } from '@playwright/test'
import { registerCenter } from './helpers'

const pad = (n: number) => String(n).padStart(2, '0')

test('ученик с долгом: оплата убирает из должников, отмена оплаты возвращает', async ({ page }) => {
  const now = new Date()
  // Срок оплаты считается от 1-го числа: 1-го числа долг ещё не просрочен — сценарий не проверить.
  test.skip(now.getDate() === 1, '1-го числа начисление ещё не просрочено')
  const monthStart = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-01`

  const { centerId } = await registerCenter(page)
  // Видимое меню: нижнее на телефоне, боковое на компьютере.
  const nav = page.getByRole('navigation', { name: 'Главное меню' }).locator('visible=true')

  // Срок оплаты 0 дней — долг просрочен со следующего дня после начисления.
  await page.goto(`/c/${centerId}/settings`)
  await page.getByLabel('Срок оплаты, дней').fill('0')
  await page.getByRole('button', { name: 'Сохранить' }).click()
  await expect(page.getByText('Настройки сохранены')).toBeVisible()

  // Тариф
  await page.goto(`/c/${centerId}/tariffs`)
  await page.getByRole('button', { name: 'Создать тариф' }).first().click()
  let dialog = page.getByRole('dialog', { name: 'Новый тариф' })
  await dialog.getByLabel('Название').fill('Английский — месяц')
  await dialog.getByLabel('Цена').fill('3000')
  await dialog.getByRole('button', { name: 'Создать' }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByText('Английский — месяц')).toBeVisible()

  // Группа с тарифом по умолчанию
  await page.goto(`/c/${centerId}/groups`)
  await page.getByRole('button', { name: 'Создать группу' }).first().click()
  dialog = page.getByRole('dialog', { name: 'Новая группа' })
  await dialog.getByLabel('Название').fill('Английский A1')
  await dialog.getByRole('button', { name: 'Пн', exact: true }).click()
  await dialog.getByLabel('Пн: начало').fill('15:00')
  await dialog.getByLabel('Пн: конец').fill('16:30')
  await dialog.getByLabel('Тариф по умолчанию').selectOption({ label: 'Английский — месяц · 3 000 с' })
  await dialog.getByRole('button', { name: 'Создать' }).click()
  await expect(dialog).toBeHidden()

  // Ученик с записью в группу с 1-го числа и начислением за текущий месяц
  await page.goto(`/c/${centerId}/students`)
  await page.getByRole('button', { name: 'Добавить ученика' }).first().click()
  dialog = page.getByRole('dialog', { name: 'Новый ученик' })
  await dialog.getByLabel('ФИО ученика').fill('Асанов Бекзат')
  await dialog.getByLabel('Имя родителя').fill('Гульнара')
  await dialog.getByLabel('Телефон родителя').fill('0700 111 222')
  await dialog.getByLabel('Группа').selectOption({ label: 'Английский A1' })
  await dialog.getByLabel('Дата начала').fill(monthStart)
  await expect(dialog.getByLabel('Начислить полностью')).toBeChecked()
  await dialog.getByRole('button', { name: 'Добавить ученика' }).click()
  await page.waitForURL(/\/students\/[0-9a-f-]{36}$/)
  const cardUrl = page.url()
  await expect(page.getByText('Долг', { exact: true })).toBeVisible()
  await expect(page.getByText('Начисление').first()).toBeVisible()

  // В должниках
  await nav.getByRole('link', { name: 'Должники' }).click()
  await expect(page.getByText('Асанов Бекзат')).toBeVisible()

  // Оплата
  await page.goto(cardUrl)
  await page.getByRole('button', { name: 'Отметить оплату' }).click()
  dialog = page.getByRole('dialog', { name: 'Оплата' })
  await expect(dialog.getByLabel('Сумма')).toHaveValue('3000')
  await dialog.getByRole('button', { name: 'Отметить оплату' }).click()
  await expect(page.getByText('Оплата отмечена')).toBeVisible()
  await expect(page.getByText('Долгов нет').first()).toBeVisible()

  await nav.getByRole('link', { name: 'Должники' }).click()
  await expect(page.getByText('Должников нет')).toBeVisible()
  await expect(page.getByText('Асанов Бекзат')).toHaveCount(0)

  // Отмена оплаты с причиной
  await page.goto(cardUrl)
  await page.getByRole('button', { name: /^Отменить: Оплата/ }).click()
  dialog = page.getByRole('dialog', { name: 'Отменить операцию?' })
  await dialog.getByRole('button', { name: 'Отменить' }).click()
  await expect(dialog.getByText('Причина — от 3 символов')).toBeVisible()
  await dialog.getByLabel('Причина отмены').fill('Ошибочная отметка')
  await dialog.getByRole('button', { name: 'Отменить' }).click()
  await expect(page.getByText('Операция отменена')).toBeVisible()
  await expect(page.getByText('Отменено: Ошибочная отметка')).toBeVisible()

  await nav.getByRole('link', { name: 'Должники' }).click()
  await expect(page.getByText('Асанов Бекзат')).toBeVisible()
})
