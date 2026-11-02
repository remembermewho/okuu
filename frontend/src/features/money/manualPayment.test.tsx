// Ручная оплата: реквизиты и предпросмотр в «Настройках», плашка без реквизитов, быстрая отметка оплаты из «Должников».
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Schemas } from '@/api/client'
import { list, meAdmin, meOwner } from '@/test/fixtures'
import { bodyOf, mockApi, renderRoute } from '@/test/utils'

const C = '/api/v1/c/c1'

const settings: Schemas['Settings'] = {
  name: 'Билим',
  phone: null,
  timezone: 'Asia/Bishkek',
  billing_day: 1,
  billing_day_next: null,
  billing_day_next_from: null,
  due_days: 5,
  excused_deducts: false,
  reminders_on: true,
  overdue_repeat: 'weekly',
  notify_absent: false,
  pay_details: null,
  plan: 'start',
  finik_connected: false,
}

const debtor = (over: object = {}) => ({
  student_id: 's1',
  full_name: 'Асанов Бекзат',
  status: 'active',
  groups: [{ id: 'g1', name: 'Английский A1' }],
  parent: { name: 'Гульнара', phone: '+996700111222', tg_status: 'active', last_reminder_at: null },
  debt: 345_000,
  days_overdue: 12,
  oldest_due_date: '2026-10-05',
  due_in_days: 0,
  reminded_today: false,
  ...over,
})
const debtorsOf = (items: object[]) => ({
  items,
  next_cursor: null,
  summary: { period: '2026-10-01', charged: 900_000, collected: 450_000, debt_total: 345_000, debtors_count: items.length, pending_total: 0, pending_count: 0 },
})

const base = (me = meOwner, s: Schemas['Settings'] = settings) => ({
  'GET /api/v1/me': { body: me },
  'GET /api/v1/config': { body: { online_payments: false } },
  [`GET ${C}/settings`]: { body: s },
  [`GET ${C}/groups`]: { body: list([]) },
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('реквизиты в настройках', () => {
  it('предпросмотр «Так увидит родитель в Telegram» — текст как есть, со счётчиком; пусто — «уточните у администратора»', async () => {
    mockApi(base())
    renderRoute('/c/c1/settings')
    const field = await screen.findByLabelText(/Реквизиты и инструкция для оплаты/)
    expect(field).toHaveAttribute('placeholder', expect.stringContaining('После оплаты отправьте чек'))
    const preview = screen.getByTestId('pay-preview')
    expect(screen.getByText('Так увидит родитель в Telegram')).toBeInTheDocument()
    expect(preview.textContent).toContain('🏫 Центр «Билим»: напоминание об оплате.')
    expect(preview.textContent).toContain('Как оплатить:\nУточните способ оплаты у администратора центра.')
    expect(screen.getByText('0 / 1000')).toBeInTheDocument()

    await userEvent.type(field, 'MBank: 0700 123 456{enter}Чек — в WhatsApp')
    expect(preview.textContent).toContain('Как оплатить:\nMBank: 0700 123 456\nЧек — в WhatsApp\n\nЕсли вы уже оплатили')
    expect(screen.getByText('36 / 1000')).toBeInTheDocument() // 19 + перенос + 16
    expect(field).toHaveAttribute('maxLength', '1000')
  })

  it('без реквизитов — заметная плашка в настройках', async () => {
    mockApi(base())
    renderRoute('/c/c1/settings')
    expect(await screen.findByText(/Заполните реквизиты для оплаты — без них родители не узнают, куда платить/)).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Заполнить в настройках' })).not.toBeInTheDocument() // уже в настройках
  })
})

describe('должники', () => {
  it('без реквизитов: владельцу — плашка со ссылкой на настройки, администратору — без ссылки', async () => {
    mockApi({ ...base(), [`GET ${C}/debtors`]: { body: debtorsOf([debtor()]) } })
    const { unmount } = renderRoute('/c/c1/debtors')
    expect(await screen.findByText(/Заполните реквизиты для оплаты/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Заполнить в настройках' })).toHaveAttribute('href', '/c/c1/settings')
    unmount()

    mockApi({ ...base(meAdmin), [`GET ${C}/debtors`]: { body: debtorsOf([debtor()]) } })
    renderRoute('/c/c1/debtors')
    expect(await screen.findByText(/Попросите владельца центра заполнить их/)).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Заполнить в настройках' })).not.toBeInTheDocument()
  })

  it('реквизиты заполнены — плашки нет', async () => {
    mockApi({ ...base(meOwner, { ...settings, pay_details: 'MBank: 0700 123 456' }), [`GET ${C}/debtors`]: { body: debtorsOf([debtor()]) } })
    renderRoute('/c/c1/debtors')
    expect(await screen.findByText('Асанов Бекзат')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText(/Заполните реквизиты/)).not.toBeInTheDocument())
  })

  it('«Отметить оплату» на строке: сумма = долг, способ — перевод, ключ идемпотентности, строка уходит, тост', async () => {
    let paid = false
    const { calls } = mockApi({
      ...base(meOwner, { ...settings, pay_details: 'MBank' }),
      [`GET ${C}/debtors`]: () => ({ body: debtorsOf(paid ? [] : [debtor(), debtor({ student_id: 's2', full_name: 'Бекова Айжан', parent: null, debt: 100_000 })]) }),
      [`POST ${C}/students/s1/payments`]: () => {
        paid = true
        return { status: 201, body: { id: 'op1', kind: 'payment', amount: 345_000, method: 'transfer' } }
      },
    })
    renderRoute('/c/c1/debtors')
    await userEvent.click(await screen.findByRole('button', { name: 'Отметить оплату: Асанов Бекзат' }))
    const dialog = await screen.findByRole('dialog', { name: 'Оплата' })
    expect(within(dialog).getByText('Асанов Бекзат')).toBeInTheDocument()
    expect(within(dialog).getByLabelText(/Сумма/)).toHaveValue('3450')
    expect(within(dialog).getByRole('radio', { name: 'Перевод' })).toBeChecked()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отметить оплату' }))

    expect(await screen.findByText('Оплата отмечена, родителю отправлено подтверждение')).toBeInTheDocument()
    const post = calls.find((r) => r.method === 'POST')!
    expect(await bodyOf(post)).toEqual({ kind: 'payment', amount: 345_000, method: 'transfer', comment: null })
    expect(post.headers.get('Idempotency-Key')).toBeTruthy()
    await waitFor(() => expect(screen.queryByText('Асанов Бекзат')).not.toBeInTheDocument())
  })

  it('родитель не подключён к Telegram — тост об этом', async () => {
    mockApi({
      ...base(meOwner, { ...settings, pay_details: 'MBank' }),
      [`GET ${C}/debtors`]: { body: debtorsOf([debtor({ parent: { name: 'Нургуль', phone: '+996700333444', tg_status: 'none', last_reminder_at: null } })]) },
      [`POST ${C}/students/s1/payments`]: { status: 201, body: { id: 'op1' } },
    })
    renderRoute('/c/c1/debtors')
    await userEvent.click(await screen.findByRole('button', { name: 'Отметить оплату: Асанов Бекзат' }))
    const dialog = await screen.findByRole('dialog', { name: 'Оплата' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отметить оплату' }))
    expect(await screen.findByText('Оплата отмечена, родитель не подключён к Telegram')).toBeInTheDocument()
  })
})
