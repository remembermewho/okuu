// Импорт тестового файла: проверка строк, загрузка подходящих, ученики и долг на месте.
import { expect, test } from '@playwright/test'
import { apiOf, registerCenter } from './helpers'

test('импорт CSV: ошибки по строкам, загрузка остальных', async ({ page }) => {
  const { centerId } = await registerCenter(page)
  const api = apiOf(page)
  const C = `/api/v1/c/${centerId}`
  // Тариф из файла должен существовать; группа «Английский A1» — тоже, «Математика» создастся.
  const tariff = await api.post(`${C}/tariffs`, { name: 'Английский — месяц', kind: 'monthly', price: 300000 })
  await api.post(`${C}/groups`, { name: 'Английский A1', default_tariff_id: tariff.id })

  await page.goto(`/c/${centerId}/import`)
  await page.getByLabel('Файл').setInputFiles('e2e/fixtures/import.csv')
  await page.getByRole('button', { name: 'Проверить файл' }).click()

  await expect(page.getByText('Строк в файле: 4')).toBeVisible()
  await expect(page.getByText('Будет загружено: 2 ученика')).toBeVisible()
  await expect(page.getByText('2 с ошибками')).toBeVisible()
  await expect(page.getByText(/Телефон родителя не указан/)).toBeVisible()
  await expect(page.getByText(/Тариф «Нет такого тарифа» не найден/)).toBeVisible()
  await expect(page.getByText(/Группа «Математика» не найдена — будет создана/)).toBeVisible()

  await page.getByLabel(/Не начислять/).check()
  await page.getByRole('button', { name: 'Загрузить 2 ученика' }).click()
  await expect(page.getByRole('heading', { name: 'Импорт завершён' })).toBeVisible()
  await expect(page.getByText('1 новая группа')).toBeVisible()
  await expect(page.getByText(/Начальный долг: 1\s500\sс/)).toBeVisible()

  await page.getByRole('link', { name: 'К ученикам' }).click()
  await expect(page.getByText('Импортов Азамат')).toBeVisible()
  await expect(page.getByText('Импортова Айжан')).toBeVisible()
  await expect(page.getByText('Без Телефона')).toHaveCount(0)
})
