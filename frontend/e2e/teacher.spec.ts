// Преподаватель по приглашению видит только «Сегодня» (и свои группы), без денег.
import { expect, test } from '@playwright/test'
import { phone, registerCenter } from './helpers'

test('преподаватель видит только «Сегодня» и свои группы', async ({ page, browser }) => {
  const { centerId } = await registerCenter(page)

  await page.goto(`/c/${centerId}/staff`)
  await page.getByRole('button', { name: 'Пригласить' }).click()
  const dialog = page.getByRole('dialog', { name: 'Пригласить сотрудника' })
  await expect(dialog.getByLabel('Преподаватель')).toBeChecked()
  await dialog.getByLabel(/Кого приглашаете/).fill('Нурлан')
  await dialog.getByRole('button', { name: 'Создать ссылку' }).click()
  const url = await page.getByLabel('Ссылка', { exact: true }).inputValue()
  expect(url).toMatch(/\/invite\/[A-Za-z0-9_-]+$/)

  // Второй браузер — преподаватель без аккаунта.
  const ctx = await browser.newContext({ viewport: page.viewportSize() ?? undefined, locale: 'ru-RU', timezoneId: 'Asia/Bishkek' })
  const t = await ctx.newPage()
  await t.goto(url)
  await expect(t.getByText(/Вас приглашают в «Билим Плюс» — преподаватель/)).toBeVisible()
  await expect(t.getByLabel('Ваше имя')).toHaveValue('Нурлан')
  await t.getByLabel('Телефон').fill(phone())
  await t.getByLabel('Пароль').fill('secret-pass')
  await t.getByRole('button', { name: 'Принять приглашение' }).click()
  await t.waitForURL(new RegExp(`/c/${centerId}/today`))
  await expect(t.getByRole('heading', { name: 'Сегодня', level: 1 })).toBeVisible()

  // Меню: только «Сегодня» и «Группы» (на телефоне — нижнее меню, на компьютере — боковое).
  const menu = t.getByRole('navigation', { name: 'Главное меню' }).locator('visible=true')
  await expect(menu.getByRole('link')).toHaveText(['Сегодня', 'Группы'])

  // Разделы с деньгами — «Нет доступа».
  for (const section of ['debtors', 'students', 'report', 'settings']) {
    await t.goto(`/c/${centerId}/${section}`)
    await expect(t.getByText(/Нет доступа/)).toBeVisible()
  }
  await ctx.close()
})
