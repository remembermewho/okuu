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
  const teacherPhone = phone()
  await t.getByLabel('Телефон').fill(teacherPhone)
  await t.getByLabel('Пароль', { exact: true }).fill('secret-pass')
  // Глаз: пароль видно, фокус не теряется.
  await t.getByRole('button', { name: 'Показать пароль' }).click()
  await expect(t.getByLabel('Пароль', { exact: true })).toHaveAttribute('type', 'text')
  await t.getByRole('button', { name: 'Скрыть пароль' }).click()
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

  // Владелец сбрасывает пароль преподавателю (он работает только в этом центре): ссылка → новый пароль → вход.
  await page.reload()
  await page.getByRole('button', { name: 'Действия: Нурлан' }).click()
  await page.getByRole('menuitem', { name: 'Сбросить пароль' }).click()
  const resetDialog = page.getByRole('dialog', { name: 'Ссылка для смены пароля' })
  await expect(resetDialog.getByRole('link', { name: 'Отправить в WhatsApp' })).toBeVisible()
  const resetUrl = await resetDialog.getByLabel('Ссылка', { exact: true }).inputValue()
  expect(resetUrl).toMatch(/\/reset\/[A-Za-z0-9_-]+$/)

  await t.goto(resetUrl)
  await expect(t.getByText(/Нурлан, центр «Билим Плюс»/)).toBeVisible()
  await t.getByLabel('Новый пароль', { exact: true }).fill('new-secret-1')
  await t.getByLabel('Повторите новый пароль').fill('new-secret-1')
  await t.getByRole('button', { name: 'Сохранить пароль' }).click()
  await expect(t.getByRole('heading', { name: 'Пароль изменён' })).toBeVisible()
  // Все сессии закрыты — старая вкладка уходит на вход; повтор ссылки — недействительна.
  await t.goto(`/c/${centerId}/today`)
  await t.waitForURL(/\/login/)
  await t.goto(resetUrl)
  await expect(t.getByRole('heading', { name: 'Ссылка недействительна' })).toBeVisible()
  await t.goto('/login')
  await t.getByLabel('Телефон').fill(teacherPhone)
  await t.getByLabel('Пароль', { exact: true }).fill('new-secret-1')
  await t.getByRole('button', { name: 'Войти' }).click()
  await t.waitForURL(new RegExp(`/c/${centerId}/today`))
  await ctx.close()
})
