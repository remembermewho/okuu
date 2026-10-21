import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { group, list, meAdmin, meOwner, meTeacher, student, tariffMonth, tariffPackage } from '@/test/fixtures'
import { apiError, bodyOf, mockApi, renderRoute } from '@/test/utils'
import { checkSchedule } from './GroupDialog'
import { scheduleText } from './schedule'

const C = '/api/v1/c/c1'
const staff = list([
  { user_id: 'u1', name: 'Айгуль', phone: '+996700123456', role: 'owner' },
  { user_id: 'u3', name: 'Нурлан', phone: '+996700333333', role: 'teacher' },
])

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('расписание', () => {
  it('дни с одинаковым временем — вместе, по порядку дней', () => {
    expect(
      scheduleText([
        { dow: 5, start: '10:00', end: '11:00' },
        { dow: 3, start: '15:00', end: '16:30' },
        { dow: 1, start: '15:00', end: '16:30' },
      ]),
    ).toBe('Пн, Ср 15:00–16:30 · Пт 10:00–11:00')
    expect(scheduleText([{ dow: 7, start: '09:00', end: '10:00' }])).toBe('Вс 09:00–10:00')
  })

  it('проверка: время заполнено и конец позже начала', () => {
    expect(checkSchedule([{ dow: 1, start: '15:00', end: '16:00' }])).toEqual({})
    expect(checkSchedule([{ dow: 1, start: '', end: '16:00' }, { dow: 2, start: '16:00', end: '15:00' }])).toEqual({ 0: 'Заполните поле', 1: 'Конец позже начала' })
  })
})

describe('группы', () => {
  it('список: расписание, преподаватель, тариф', async () => {
    mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/groups`]: { body: list([{ ...group, teacher_id: 'u3', teacher_name: 'Нурлан', schedule: [{ dow: 1, start: '15:00', end: '16:30' }, { dow: 3, start: '15:00', end: '16:30' }] }]) },
      [`GET ${C}/tariffs`]: { body: list([tariffMonth]) },
    })
    renderRoute('/c/c1/groups')
    expect(await screen.findByRole('link', { name: 'Английский A1' })).toHaveAttribute('href', '/c/c1/groups/g1')
    expect(screen.getByText('Пн, Ср 15:00–16:30')).toBeInTheDocument()
    expect(screen.getByText('Нурлан')).toBeInTheDocument()
    expect(await screen.findByText(/3 ученика · Месяц · 3\s000\sс/)).toBeInTheDocument()
  })

  it('создание: дни, время, преподаватель и тариф', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meAdmin },
      [`GET ${C}/groups`]: { body: list([]) },
      [`GET ${C}/tariffs`]: { body: list([tariffMonth, tariffPackage]) },
      [`GET ${C}/staff`]: { body: staff },
      [`POST ${C}/groups`]: { status: 201, body: group },
    })
    renderRoute('/c/c1/groups')
    expect(await screen.findByText('Групп пока нет')).toBeInTheDocument()
    await userEvent.click(screen.getAllByRole('button', { name: 'Создать группу' })[0]!)
    const dialog = await screen.findByRole('dialog', { name: 'Новая группа' })
    await userEvent.type(within(dialog).getByLabelText('Название'), 'Английский A1')
    await userEvent.selectOptions(await within(dialog).findByLabelText('Преподаватель'), 'u3')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Пн' }))
    await userEvent.type(within(dialog).getByLabelText('Пн: начало'), '15:00')
    await userEvent.type(within(dialog).getByLabelText('Пн: конец'), '16:30')
    // Новый день берёт время предыдущего.
    await userEvent.click(within(dialog).getByRole('button', { name: 'Ср' }))
    expect(within(dialog).getByLabelText('Ср: начало')).toHaveValue('15:00')
    await userEvent.selectOptions(within(dialog).getByLabelText('Тариф по умолчанию'), 't1')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Создать' }))
    await waitFor(() => expect(calls.some((r) => r.method === 'POST')).toBe(true))
    expect(await bodyOf(calls.find((r) => r.method === 'POST')!)).toEqual({
      name: 'Английский A1',
      teacher_id: 'u3',
      default_tariff_id: 't1',
      schedule: [
        { dow: 1, start: '15:00', end: '16:30' },
        { dow: 3, start: '15:00', end: '16:30' },
      ],
    })
  })

  it('ошибки: конец раньше начала — без запроса; ошибка сервера по полю — под полем', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meAdmin },
      [`GET ${C}/groups`]: { body: list([]) },
      [`GET ${C}/tariffs`]: { body: list([tariffMonth]) },
      [`GET ${C}/staff`]: { body: staff },
      [`POST ${C}/groups`]: apiError(422, 'validation', 'Преподаватель должен быть сотрудником этого центра', 'teacher_id'),
    })
    renderRoute('/c/c1/groups')
    await userEvent.click((await screen.findAllByRole('button', { name: 'Создать группу' }))[0]!)
    const dialog = await screen.findByRole('dialog')
    await userEvent.type(within(dialog).getByLabelText('Название'), 'Группа')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Вт' }))
    await userEvent.type(within(dialog).getByLabelText('Вт: начало'), '16:00')
    await userEvent.type(within(dialog).getByLabelText('Вт: конец'), '15:00')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Создать' }))
    expect(await within(dialog).findByText('Конец позже начала')).toBeInTheDocument()
    expect(calls.some((r) => r.method === 'POST')).toBe(false)

    await userEvent.click(within(dialog).getByRole('button', { name: 'Удалить: Вт' }))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Создать' }))
    expect(await within(dialog).findByText('Преподаватель должен быть сотрудником этого центра')).toBeInTheDocument()
  })

  it('преподаватель: только список своих групп, без изменений и состава', async () => {
    mockApi({ 'GET /api/v1/me': { body: meTeacher }, [`GET ${C}/groups`]: { body: list([group]) } })
    renderRoute('/c/c1/groups')
    expect(await screen.findByText('Английский A1')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Английский A1' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Создать группу' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Изменить' })).not.toBeInTheDocument()
  })

  it('состав группы — ученики с балансом', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/groups/g1`]: { body: group },
      [`GET ${C}/tariffs`]: { body: list([tariffMonth]) },
      [`GET ${C}/students`]: { body: list([{ ...student(), groups: [{ id: 'g1', name: 'Английский A1' }] }]) },
    })
    renderRoute('/c/c1/groups/g1')
    expect(await screen.findByRole('link', { name: /Асанов Бекзат/ })).toHaveAttribute('href', '/c/c1/students/s1')
    expect(calls.some((r) => r.url.includes('/students?group=g1'))).toBe(true)
  })

  it('архив — с подтверждением', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/groups`]: { body: list([group]) },
      [`GET ${C}/tariffs`]: { body: list([tariffMonth]) },
      [`DELETE ${C}/groups/g1`]: { status: 204 },
    })
    renderRoute('/c/c1/groups')
    await userEvent.click(await screen.findByRole('button', { name: 'В архив' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'В архив' }))
    await waitFor(() => expect(calls.some((r) => r.method === 'DELETE')).toBe(true))
    expect(await screen.findByText('Группа в архиве')).toBeInTheDocument()
  })
})
