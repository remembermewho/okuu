import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { group, list, meOwner, operation, student, tariffMonth } from '@/test/fixtures'
import { apiError, bodyOf, mockApi, renderRoute } from '@/test/utils'

const C = '/api/v1/c/c1'

afterEach(() => {
  vi.unstubAllGlobals()
})

function cardApi(extra: Parameters<typeof mockApi>[0] = {}, st = student({ discount: { kind: 'percent', value: 10 }, birth_date: '2015-03-01' })) {
  return mockApi({
    'GET /api/v1/me': { body: meOwner },
    [`GET ${C}/students/s1`]: { body: st },
    [`GET ${C}/students/s1/ledger`]: { body: list([operation()]) },
    [`GET ${C}/students/s1/attendance`]: { body: list([]) },
    ...extra,
  })
}

async function openEdit() {
  await userEvent.click(await screen.findByRole('button', { name: 'Изменить данные' }))
  return screen.findByRole('dialog', { name: 'Данные ученика' })
}

describe('изменение ученика', () => {
  it('ФИО, дата рождения, родитель и скидка — в форме; уходят только изменённые поля', async () => {
    const { calls } = cardApi({ [`PATCH ${C}/students/s1`]: { body: student() } })
    renderRoute('/c/c1/students/s1')
    const dialog = await openEdit()
    expect(within(dialog).getByLabelText('ФИО ученика')).toHaveValue('Асанов Бекзат')
    expect(within(dialog).getByLabelText(/Дата рождения/)).toHaveValue('2015-03-01')
    expect(within(dialog).getByLabelText('Имя родителя')).toHaveValue('Гульнара')
    expect(within(dialog).getByLabelText('Телефон родителя')).toHaveValue('+996700111222')
    expect(within(dialog).getByLabelText('Скидка')).toHaveValue('percent')
    expect(within(dialog).getByLabelText('Размер скидки')).toHaveValue('10')

    await userEvent.clear(within(dialog).getByLabelText('ФИО ученика'))
    await userEvent.type(within(dialog).getByLabelText('ФИО ученика'), 'Асанов Бекзат Нурланович')
    await userEvent.clear(within(dialog).getByLabelText('Телефон родителя'))
    await userEvent.type(within(dialog).getByLabelText('Телефон родителя'), '0555 333 444')
    await userEvent.selectOptions(within(dialog).getByLabelText('Скидка'), 'fixed')
    await userEvent.clear(within(dialog).getByLabelText('Размер скидки'))
    await userEvent.type(within(dialog).getByLabelText('Размер скидки'), '500')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('Данные сохранены')).toBeInTheDocument()
    expect(await bodyOf(calls.find((r) => r.method === 'PATCH')!)).toEqual({
      full_name: 'Асанов Бекзат Нурланович',
      parent: { phone: '+996555333444' },
      discount: { kind: 'fixed', value: 50_000 },
    })
  })

  it('скидка: процент 0–100, фиксированная — не больше цены активного тарифа; ошибка до отправки', async () => {
    const { calls } = cardApi()
    renderRoute('/c/c1/students/s1')
    const dialog = await openEdit()
    const value = within(dialog).getByLabelText('Размер скидки')
    await userEvent.clear(value)
    await userEvent.type(value, '101')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Сохранить' }))
    expect(await within(dialog).findByText('От 0 до 100')).toBeInTheDocument()

    await userEvent.clear(value)
    await userEvent.type(value, '0')
    await userEvent.selectOptions(within(dialog).getByLabelText('Скидка'), 'fixed')
    await userEvent.clear(within(dialog).getByLabelText('Размер скидки'))
    await userEvent.type(within(dialog).getByLabelText('Размер скидки'), '3001')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Сохранить' }))
    expect(await within(dialog).findByText(/Не больше цены тарифа «Месяц»: 3\s000\sс/)).toBeInTheDocument()
    expect(calls.some((r) => r.method === 'PATCH')).toBe(false)
  })

  it('422 по скидке с сервера — под полем', async () => {
    cardApi({ [`PATCH ${C}/students/s1`]: apiError(422, 'validation', 'Скидка 2 500 с больше цены тарифа «Месяц»', 'discount') })
    renderRoute('/c/c1/students/s1')
    const dialog = await openEdit()
    await userEvent.clear(within(dialog).getByLabelText('Размер скидки'))
    await userEvent.type(within(dialog).getByLabelText('Размер скидки'), '20')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Сохранить' }))
    expect(await within(dialog).findByText(/больше цены тарифа/)).toBeInTheDocument()
    expect(within(dialog).getByLabelText('Размер скидки')).toHaveAttribute('aria-invalid', 'true')
  })
})

describe('уход ученика', () => {
  it('«Ученик ушёл» — последний день занятий (по умолчанию сегодня центра), диапазон проверяется до отправки', async () => {
    const { calls } = cardApi({ [`PATCH ${C}/students/s1`]: { body: student({ status: 'left' }) } })
    renderRoute('/c/c1/students/s1')
    await userEvent.click(await screen.findByRole('button', { name: 'Ещё действия' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Ученик ушёл' }))
    const dialog = await screen.findByRole('dialog', { name: 'Отметить, что ученик ушёл?' })
    const date = within(dialog).getByLabelText('Последний день занятий')
    expect(date).toHaveValue('2026-10-19') // «сегодня» центра из /me
    expect(date).toHaveAttribute('min', '2026-09-01') // начало активной записи
    expect(date).toHaveAttribute('max', '2026-11-18') // сегодня + 30

    await userEvent.clear(date)
    await userEvent.type(date, '2026-12-01')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Ученик ушёл' }))
    expect(await within(dialog).findByText('Дата — с 01.09.2026 по 18.11.2026')).toBeInTheDocument()
    expect(calls.some((r) => r.method === 'PATCH')).toBe(false)

    await userEvent.clear(date)
    await userEvent.type(date, '2026-10-25')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Ученик ушёл' }))
    await waitFor(() => expect(calls.some((r) => r.method === 'PATCH')).toBe(true))
    expect(await bodyOf(calls.find((r) => r.method === 'PATCH')!)).toEqual({ status: 'left', left_date: '2026-10-25' })
  })

  it('список: ушедшие скрыты по умолчанию, «Показать ушедших» — показывает', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/students`]: { body: list([]) },
      [`GET ${C}/groups`]: { body: list([group]) },
    })
    const { router } = renderRoute('/c/c1/students')
    await screen.findByText('Пока нет учеников')
    expect(calls.filter((r) => r.url.includes('/students?')).every((r) => r.url.includes('hide_left=true'))).toBe(true)
    await userEvent.click(screen.getByLabelText('Показать ушедших'))
    await waitFor(() => expect(router.state.location.search).toContain('show_left=1'))
    await waitFor(() => expect(calls.some((r) => r.url.includes('/students?') && !r.url.includes('hide_left'))).toBe(true))
    // Фильтр по статусу — флажок не нужен.
    await userEvent.selectOptions(screen.getByLabelText('Статус'), 'left')
    expect(screen.queryByLabelText('Показать ушедших')).not.toBeInTheDocument()
  })
})

describe('скидка при добавлении ученика', () => {
  it('фиксированная больше цены выбранного тарифа — ошибка под полем без запроса', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/students`]: { body: list([]) },
      [`GET ${C}/groups`]: { body: list([group]) },
      [`GET ${C}/tariffs`]: { body: list([tariffMonth]) },
    })
    renderRoute('/c/c1/students')
    await userEvent.click((await screen.findAllByRole('button', { name: 'Добавить ученика' }))[0]!)
    const dialog = await screen.findByRole('dialog')
    await userEvent.type(within(dialog).getByLabelText('ФИО ученика'), 'Асанов Бекзат')
    await userEvent.type(within(dialog).getByLabelText('Имя родителя'), 'Гульнара')
    await userEvent.type(within(dialog).getByLabelText('Телефон родителя'), '0700111222')
    await userEvent.selectOptions(within(dialog).getByLabelText('Скидка'), 'fixed')
    await userEvent.type(within(dialog).getByLabelText('Размер скидки'), '3500')
    await userEvent.selectOptions(await within(dialog).findByLabelText('Группа'), 'g1')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Добавить ученика' }))
    expect(await within(dialog).findByText(/Не больше цены тарифа «Месяц»/)).toBeInTheDocument()
    await waitFor(() => expect(calls.some((r) => r.method === 'POST')).toBe(false))
  })
})
