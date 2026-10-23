// Панель суперадмина на настоящем бэкенде: права только «из консоли» (SQL как server admin grant),
// блокировка центра закрывает его владельцу, ссылка регистрации одноразовая; доступность панели.
import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { grantPlatformAdmin, registerCenter } from './helpers'

test('суперадмин: приостановить и вернуть доступ центру, ссылка регистрации', async ({ page, browser }) => {
  test.setTimeout(90_000)
  // Обычный владелец — панель ему «не найдена».
  const ownerCtx = await browser.newContext()
  const owner = await ownerCtx.newPage()
  const name = `Блокировка ${Date.now() % 1_000_000}` // база общая для проектов mobile и desktop
  const { centerId } = await registerCenter(owner, name, 'Нургуль Асанова')
  await owner.goto('/admin')
  await expect(owner.getByText('Не найдено')).toBeVisible()

  const { phone } = await registerCenter(page, 'Центр суперадмина', 'Суперадмин')
  grantPlatformAdmin(phone)
  await page.goto('/admin')
  await expect(page.getByRole('heading', { name: 'Центры' })).toBeVisible()
  await page.getByLabel('Поиск').fill(name)
  await page.getByRole('link', { name }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()

  const axe = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(axe.violations.filter((v) => v.impact === 'critical' || v.impact === 'serious').map((v) => v.id)).toEqual([])

  await page.getByRole('button', { name: 'Приостановить доступ' }).click()
  const dialog = page.getByRole('dialog', { name: 'Приостановить доступ к центру' })
  await dialog.getByLabel(/Причина/).fill('Не оплачен ноябрь')
  await dialog.getByRole('button', { name: 'Приостановить доступ' }).click()
  await expect(page.getByRole('button', { name: 'Вернуть доступ' })).toBeVisible()

  await owner.goto(`/c/${centerId}/debtors`)
  await expect(owner.getByRole('heading', { name: 'Доступ приостановлен' })).toBeVisible()
  await expect(owner.getByText('Не оплачен ноябрь')).toHaveCount(0) // причина — только в панели

  await page.getByRole('button', { name: 'Вернуть доступ' }).click()
  await expect(page.getByRole('button', { name: 'Приостановить доступ' })).toBeVisible()
  await owner.getByRole('button', { name: 'Проверить снова' }).click()
  await expect(owner.getByRole('heading', { name: 'Должники' })).toBeVisible()

  // Ссылка регистрации: по ней регистрируются один раз.
  await page.getByRole('link', { name: 'Ссылки регистрации' }).click()
  await page.getByLabel(/Для кого/).fill('Садик «Балапан»')
  await page.getByRole('button', { name: 'Создать ссылку' }).click()
  const url = await page.getByRole('dialog', { name: 'Ссылка регистрации' }).getByLabel('Ссылка').inputValue()
  expect(url).toMatch(/\/register\/[\w-]+$/)
  await ownerCtx.close()

  const guest = await (await browser.newContext()).newPage()
  await guest.goto(new URL(url).pathname)
  await guest.getByLabel('Название центра').fill('Балапан')
  await guest.getByLabel('Ваше имя').fill('Айгерим')
  await guest.getByLabel('Телефон').fill('0700 999 1' + String(Date.now() % 100).padStart(2, '0'))
  await guest.getByLabel('Пароль', { exact: true }).fill('secret-pass')
  await guest.getByRole('button', { name: 'Создать центр' }).click()
  await guest.waitForURL(/\/c\/[^/]+\/debtors/)
  await guest.context().clearCookies()
  await guest.goto(new URL(url).pathname)
  await expect(guest.getByRole('heading', { name: 'Ссылка недействительна' })).toBeVisible()
})
