// Профиль: имя, смена пароля (другая сессия закрывается), «Выйти на всех устройствах».
import { expect, test } from '@playwright/test'
import { registerCenter } from './helpers'

test('профиль: имя, смена пароля, выход на всех устройствах', async ({ page, browser }) => {
  const { centerId, phone } = await registerCenter(page)

  // Вторая сессия того же человека (другое устройство).
  const ctx = await browser.newContext({ viewport: page.viewportSize() ?? undefined, locale: 'ru-RU' })
  const other = await ctx.newPage()
  await other.goto('/login')
  await other.getByLabel('Телефон').fill(phone)
  await other.getByLabel('Пароль', { exact: true }).fill('secret-pass')
  await other.getByRole('button', { name: 'Войти' }).click()
  await other.waitForURL(/\/c\//)

  await page.getByRole('button', { name: 'Аккаунт' }).click()
  await page.getByRole('menuitem', { name: 'Профиль' }).click()
  await page.waitForURL(/\/account$/)
  await expect(page.getByLabel('Телефон')).toHaveAttribute('readonly', '')
  await page.getByLabel('Имя').fill('Айгуль Нурлановна')
  await page.getByRole('button', { name: 'Сохранить' }).click()
  await expect(page.getByText('Имя сохранено')).toBeVisible()

  await page.getByRole('button', { name: 'Сменить пароль' }).click()
  const dialog = page.getByRole('dialog', { name: 'Сменить пароль' })
  await dialog.getByLabel('Текущий пароль').fill('wrong-pass')
  await dialog.getByLabel('Новый пароль', { exact: true }).fill('changed-pass')
  await dialog.getByLabel('Повторите новый пароль').fill('changed-pass')
  await dialog.getByRole('button', { name: 'Сохранить' }).click()
  await expect(dialog.getByText('Текущий пароль неверный')).toBeVisible()
  await dialog.getByLabel('Текущий пароль').fill('secret-pass')
  await dialog.getByRole('button', { name: 'Сохранить' }).click()
  await expect(page.getByText(/Пароль изменён/)).toBeVisible()

  // Текущая сессия жива, другая — закрыта.
  await page.getByRole('link', { name: 'Назад' }).click()
  await page.waitForURL(new RegExp(`/c/${centerId}/`))
  await other.reload()
  await other.waitForURL(/\/login/)

  // Выйти на всех устройствах — и с этого тоже.
  await page.goto('/account')
  await page.getByRole('button', { name: 'Выйти на всех устройствах' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Выйти на всех устройствах' }).click()
  await page.waitForURL(/\/login/)
  await page.goto(`/c/${centerId}/debtors`)
  await page.waitForURL(/\/login/)
  await ctx.close()
})
