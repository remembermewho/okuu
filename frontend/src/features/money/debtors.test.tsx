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
      parent: { name: 'Гульнара', phone: '+996700111222', tg_status: 'active', last_reminder_at: '2026-10-19T05:00:00Z' },
      debt: 300_000,
      days_overdue: 12,
      oldest_due_date: '2026-10-05',
      due_in_days: 0,
      reminded_today: true,
    },
    {
      student_id: 's2',
      full_name: 'Бекова Айжан',
      status: 'left',
      groups: [{ id: 'g1', name: 'Английский A1' }],
      parent: { name: 'Нургуль', phone: '+996700333444', tg_status: 'none', last_reminder_at: null },
      debt: 150_000,
      days_overdue: 3,
      oldest_due_date: '2026-10-15',
      due_in_days: 0,
      reminded_today: false,
    },
  ],
  next_cursor: null,
  summary: { period: '2026-10-01', charged: 900_000, collected: 450_000, debt_total: 450_000, debtors_count: 2, pending_total: 240_000, pending_count: 1 },
}

const pendingList = {
  ...debtors,
  items: [
    {
      ...debtors.items[0]!,
      student_id: 's3',
      full_name: 'Сыдыков Эрлан',
      status: 'active',
      debt: 240_000,
      days_overdue: 0,
      oldest_due_date: '2026-10-24',
      due_in_days: 5,
      reminded_today: false,
    },
  ],
}

/** /debtors: просроченные или ожидающие — по ?status. */
const byStatus = (overdue: unknown, pending: unknown) => (req: Request) => ({
  body: new URL(req.url).searchParams.get('status') === 'pending' ? pending : overdue,
})

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
    // В обеих вкладках.
    await waitFor(() => expect(calls.some((r) => r.url.includes('status=overdue') && r.url.includes('group=g1'))).toBe(true))
    expect(calls.some((r) => r.url.includes('status=pending') && r.url.includes('group=g1'))).toBe(true)
  })

  it('вкладки со счётчиками; «Ожидают оплаты» — срок, «Напомнить»; пометки «ушёл» и «напоминали сегодня»', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/debtors`]: byStatus(debtors, pendingList),
      [`GET ${C}/groups`]: { body: list([group]) },
      [`POST ${C}/reminders`]: { body: { sent: 1, messages: 1, skipped: [] } },
    })
    const { router } = renderRoute('/c/c1/debtors')
    const overdueTab = await screen.findByRole('tab', { name: /Просрочено/ })
    expect(overdueTab).toHaveAttribute('aria-selected', 'true')
    await waitFor(() => expect(overdueTab).toHaveTextContent('2'))
    expect(screen.getByRole('tab', { name: /Ожидают оплаты/ })).toHaveTextContent('1')
    // Пометки в «Просрочено».
    const asanov = screen.getByText('Асанов Бекзат').closest('li')!
    expect(within(asanov).getByText('напоминали сегодня')).toBeInTheDocument()
    const bekova = screen.getByText('Бекова Айжан').closest('li')!
    expect(within(bekova).getByText('Ушёл')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('tab', { name: /Ожидают оплаты/ }))
    expect(router.state.location.search).toBe('?tab=pending')
    expect(await screen.findByText('Сыдыков Эрлан')).toBeInTheDocument()
    expect(screen.getByText('срок через 5 дней')).toBeInTheDocument()
    expect(screen.getByText('до 24.10.2026')).toBeInTheDocument()
    expect(screen.queryByText('Асанов Бекзат')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Напомнить: Сыдыков Эрлан' }))
    await screen.findByRole('dialog', { name: 'Напоминания' })
    expect(await bodyOf(calls.find((r) => r.url.endsWith('/reminders'))!)).toEqual({ student_ids: ['s3'] })
  })

  it('«Напомнить всем» в «Ожидают оплаты» — только ученики этого списка', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/debtors`]: byStatus(debtors, pendingList),
      [`GET ${C}/groups`]: { body: list([group]) },
      [`POST ${C}/reminders`]: { body: { sent: 1, messages: 1, skipped: [] } },
    })
    renderRoute('/c/c1/debtors?tab=pending')
    await screen.findByText('Сыдыков Эрлан')
    await userEvent.click(screen.getByRole('button', { name: 'Напомнить всем' }))
    const confirm = await screen.findByRole('dialog', { name: 'Напомнить всем из «Ожидают оплаты»?' })
    await userEvent.click(within(confirm).getByRole('button', { name: 'Напомнить всем' }))
    await screen.findByRole('dialog', { name: 'Напоминания' })
    expect(await bodyOf(calls.find((r) => r.url.endsWith('/reminders'))!)).toEqual({ student_ids: ['s3'] })
  })

  it('«Напомнить всем» — подтверждение и итог с причинами', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/debtors`]: byStatus(debtors, pendingList),
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
