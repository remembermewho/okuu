import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { group, list, meOwner } from '@/test/fixtures'
import { bodyOf, mockApi, renderRoute } from '@/test/utils'

const C = '/api/v1/c/c1'

const debtors = {
  items: [
    {
      student_id: 's1',
      full_name: 'Асанов Бекзат',
      status: 'active',
      groups: [{ id: 'g1', name: 'Английский A1' }],
      parent: { name: 'Гульнара', phone: '+996700111222', tg_status: 'active' },
      debt: 300_000,
      days_overdue: 12,
      oldest_due_date: '2026-10-05',
    },
    {
      student_id: 's2',
      full_name: 'Бекова Айжан',
      status: 'active',
      groups: [{ id: 'g1', name: 'Английский A1' }],
      parent: { name: 'Нургуль', phone: '+996700333444', tg_status: 'none' },
      debt: 150_000,
      days_overdue: 3,
      oldest_due_date: '2026-10-15',
    },
  ],
  next_cursor: null,
  summary: { period: '2026-10-01', charged: 900_000, collected: 450_000, debt_total: 450_000, debtors_count: 2, pending_total: 0, pending_count: 0 },
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('должники', () => {
  it('итоги месяца и список с днями просрочки', async () => {
    mockApi({ 'GET /api/v1/me': { body: meOwner }, [`GET ${C}/debtors`]: { body: debtors }, [`GET ${C}/groups`]: { body: list([group]) } })
    renderRoute('/c/c1/debtors')
    expect(await screen.findByText('Асанов Бекзат')).toBeInTheDocument()
    expect(screen.getByText(/просрочено 12 дней/)).toBeInTheDocument()
    expect(screen.getByText(/просрочено 3 дня/)).toBeInTheDocument()
  })

  it('фильтр по группе уходит в запрос', async () => {
    const { calls } = mockApi({ 'GET /api/v1/me': { body: meOwner }, [`GET ${C}/debtors`]: { body: debtors }, [`GET ${C}/groups`]: { body: list([group]) } })
    renderRoute('/c/c1/debtors')
    await screen.findByText('Асанов Бекзат')
    await userEvent.selectOptions(await screen.findByLabelText('Группа'), 'g1')
    await waitFor(() => expect(calls.some((r) => r.url.includes('/debtors?group=g1'))).toBe(true))
  })

  it('«Напомнить всем» — подтверждение и итог с причинами', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/debtors`]: { body: debtors },
      [`GET ${C}/groups`]: { body: list([group]) },
      [`POST ${C}/reminders`]: { body: { sent: 1, messages: 1, skipped: [{ student_id: 's2', reason: 'no_telegram' }] } },
    })
    renderRoute('/c/c1/debtors')
    await userEvent.click(await screen.findByRole('button', { name: 'Напомнить всем' }))
    const confirm = await screen.findByRole('dialog')
    await userEvent.click(within(confirm).getByRole('button', { name: 'Напомнить всем' }))
    const result = await screen.findByRole('dialog', { name: 'Напоминания' })
    expect(within(result).getByText(/Отправлено: 1 сообщение/)).toBeInTheDocument()
    expect(within(result).getByText(/Бекова Айжан/)).toBeInTheDocument()
    expect(within(result).getByText(/родитель не подключён к Telegram/)).toBeInTheDocument()
    expect(await bodyOf(calls.find((r) => r.url.endsWith('/reminders'))!)).toEqual({ all: true })
  })

  it('«Напомнить» по строке — только этот ученик', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/debtors`]: { body: debtors },
      [`GET ${C}/groups`]: { body: list([group]) },
      [`POST ${C}/reminders`]: { body: { sent: 1, messages: 1, skipped: [] } },
    })
    renderRoute('/c/c1/debtors')
    await userEvent.click(await screen.findByRole('button', { name: 'Напомнить: Асанов Бекзат' }))
    await screen.findByRole('dialog', { name: 'Напоминания' })
    expect(await bodyOf(calls.find((r) => r.url.endsWith('/reminders'))!)).toEqual({ student_ids: ['s1'] })
  })

  it('пустой список', async () => {
    mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/debtors`]: { body: { ...debtors, items: [], summary: { ...debtors.summary, debtors_count: 0, debt_total: 0 } } },
      [`GET ${C}/groups`]: { body: list([group]) },
    })
    renderRoute('/c/c1/debtors')
    expect(await screen.findByText('Должников нет')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Напомнить всем' })).not.toBeInTheDocument()
  })
})
