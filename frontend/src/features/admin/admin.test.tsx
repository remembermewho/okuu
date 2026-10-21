import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Schemas } from '@/api/client'
import { list, meAdmin, meOwner } from '@/test/fixtures'
import { apiError, bodyOf, mockApi, renderRoute } from '@/test/utils'
import { actionLabel } from './AuditPage'
import { settingsPatch } from './SettingsPage'

const C = '/api/v1/c/c1'

beforeEach(() => {
  // jsdom не умеет Blob-ссылки — скачивание проверяем по вызовам.
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:x'), revokeObjectURL: vi.fn() }))
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
    downloads.push(this.download)
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  downloads.length = 0
})

const downloads: string[] = []

const row = (over: object) => ({
  row: 2,
  status: 'ok',
  messages: null,
  skip: false,
  full_name: 'Асанов Бекзат',
  parent_name: 'Гульнара',
  parent_phone: '+996700111222',
  group: 'Английский A1',
  group_id: 'g1',
  tariff: 'Месяц',
  tariff_id: 't1',
  tariff_kind: 'monthly',
  start_date: null,
  debt: 150_000,
  lessons_left: null,
  ...over,
})

const preview = {
  preview_id: 'p1',
  expires_at: '2026-10-20T12:00:00Z',
  summary: { total: 3, ok: 1, warning: 1, error: 1, skipped: 0, to_import: 2 },
  rows: [
    row({}),
    row({ row: 3, status: 'warning', full_name: 'Бекова Айжан', messages: ['Группа «Математика» будет создана'], group: 'Математика', group_id: null }),
    row({ row: 4, status: 'error', full_name: 'Без телефона', parent_phone: '', messages: ['Телефон родителя: обязательное поле'] }),
  ],
}

describe('импорт', () => {
  it('файл → проверка с ошибками по строкам → загрузка без начисления за месяц → итог', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meAdmin },
      [`POST ${C}/import/preview`]: { body: preview },
      [`POST ${C}/import/commit`]: { body: { students_created: 2, parents_created: 2, groups_created: 1, enrollments_created: 2, skipped: 0, debt_total: 300_000 } },
    })
    renderRoute('/c/c1/import')
    await userEvent.click(await screen.findByRole('button', { name: 'Проверить файл' }))
    expect(await screen.findByText('Выберите файл')).toBeInTheDocument()

    const file = new File(['ФИО;Телефон'], 'students.csv', { type: 'text/csv' })
    await userEvent.upload(screen.getByLabelText('Файл'), file)
    expect(screen.getByText('students.csv')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Проверить файл' }))

    expect(await screen.findByText('Будет загружено: 2 ученика')).toBeInTheDocument()
    // multipart в jsdom не проверить (FormData jsdom ≠ FormData Node) — настоящая загрузка файла в e2e.
    expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/import/preview'))).toBe(true)
    // По умолчанию — строки с замечаниями: ошибка и предупреждение, с текстами.
    expect(screen.getByText('Телефон родителя: обязательное поле')).toBeInTheDocument()
    expect(screen.getByText('Группа «Математика» будет создана')).toBeInTheDocument()
    expect(screen.queryByText('Асанов Бекзат')).not.toBeInTheDocument()
    await userEvent.selectOptions(screen.getByLabelText('Показать'), 'all')
    expect(screen.getByText('Асанов Бекзат')).toBeInTheDocument()

    await userEvent.click(screen.getByLabelText(/Не начислять/))
    await userEvent.click(screen.getByRole('button', { name: 'Загрузить 2 ученика' }))
    expect(await screen.findByRole('heading', { name: 'Импорт завершён' })).toBeInTheDocument()
    expect(screen.getByText('1 новая группа')).toBeInTheDocument()
    expect(screen.getByText(/Начальный долг: 3\s000\sс/)).toBeInTheDocument()
    expect(await bodyOf(calls.find((r) => r.url.endsWith('/import/commit'))!)).toEqual({ preview_id: 'p1', first_month: 'none' })
  })

  it('ошибка файла с сервера — под полем файла; шаблон скачивается', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meAdmin },
      [`POST ${C}/import/preview`]: apiError(400, 'validation', 'Не найден заголовок «ФИО ученика»', 'file'),
      [`GET ${C}/import/template.xlsx`]: { body: 'xlsx', headers: { 'Content-Type': 'application/octet-stream', 'Content-Disposition': 'attachment; filename="okuu-import.xlsx"' } },
    })
    renderRoute('/c/c1/import')
    await userEvent.click(await screen.findByRole('button', { name: 'Скачать шаблон' }))
    await waitFor(() => expect(downloads).toEqual(['okuu-import.xlsx']))
    expect(calls.some((r) => r.url.endsWith('/import/template.xlsx'))).toBe(true)

    await userEvent.upload(screen.getByLabelText('Файл'), new File(['x'], 'bad.xlsx'))
    await userEvent.click(screen.getByRole('button', { name: 'Проверить файл' }))
    expect(await screen.findByText('Не найден заголовок «ФИО ученика»')).toBeInTheDocument()
    expect(screen.getByLabelText('Файл')).toHaveAttribute('aria-invalid', 'true')
  })
})

const report = {
  month: '2026-09',
  period: '2026-09-01',
  period_end: '2026-09-30',
  charged: 900_000,
  collected: { total: 600_000, cash: 400_000, transfer: 200_000, card: 0, finik: 0 },
  refunds: 50_000,
  collected_net: 550_000,
  corrections: -10_000,
  debt_end: 310_000,
  debt_end_note: '',
  debtors_end: 2,
  students_active_end: 12,
  joined: 3,
  left: 1,
}

describe('отчёт', () => {
  it('месяц из адреса, итоги и способы, выгрузка xlsx за этот месяц', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/reports/month`]: { body: report },
      [`GET ${C}/reports/month/export.xlsx`]: { body: 'x', headers: { 'Content-Disposition': 'attachment; filename="okuu-report-2026-09.xlsx"' } },
    })
    renderRoute('/c/c1/report?month=2026-09')
    expect(await screen.findByText('01.09.2026 — 30.09.2026')).toBeInTheDocument()
    expect(calls.some((r) => r.url.includes('/reports/month?m=2026-09'))).toBe(true)
    expect(screen.getByText('2 должника')).toBeInTheDocument()
    expect(screen.getByText(/−500\sс/)).toBeInTheDocument() // возвраты
    await userEvent.click(screen.getByRole('button', { name: 'Скачать Excel' }))
    await waitFor(() => expect(calls.some((r) => r.url.includes('/export.xlsx?m=2026-09'))).toBe(true))
    expect(await screen.findByText('Файл скачан')).toBeInTheDocument()
    expect(downloads).toEqual(['okuu-report-2026-09.xlsx'])
  })

  it('админу отчёт недоступен', async () => {
    mockApi({ 'GET /api/v1/me': { body: meAdmin } })
    renderRoute('/c/c1/report')
    expect(await screen.findByText(/Нет доступа/)).toBeInTheDocument()
  })
})

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

describe('настройки', () => {
  it('в PATCH — только изменённые поля; пустые необязательные → null', () => {
    const before = { name: 'Билим', phone: '', timezone: 'Asia/Bishkek', billing_day: '1', due_days: '5', excused_deducts: false, reminders_on: true, overdue_repeat: 'weekly' as const, notify_absent: false, pay_details: 'old' }
    expect(settingsPatch(before, { ...before })).toEqual({})
    expect(settingsPatch(before, { ...before, billing_day: '10', phone: '0700 123 456', pay_details: '  ', notify_absent: true })).toEqual({
      billing_day: 10,
      phone: '+996700123456',
      pay_details: null,
      notify_absent: true,
    })
    expect(settingsPatch(before, { ...before, overdue_repeat: 'off' })).toEqual({ overdue_repeat: 'off' })
  })

  it('повтор напоминаний после 7-го дня — выбор и сохранение', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/settings`]: { body: settings },
      [`PATCH ${C}/settings`]: { body: { ...settings, overdue_repeat: 'off' } },
    })
    renderRoute('/c/c1/settings')
    const select = await screen.findByLabelText('После 7-го дня просрочки')
    expect(select).toHaveValue('weekly')
    expect(screen.getByText(/14, 21, 28… день/)).toBeInTheDocument()
    await userEvent.selectOptions(select, 'off')
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    await waitFor(() => expect(calls.some((r) => r.method === 'PATCH')).toBe(true))
    expect(await bodyOf(calls.find((r) => r.method === 'PATCH')!)).toEqual({ overdue_repeat: 'off' })
  })

  it('смена дня начисления: подсказка, сохранение, «изменится с …»', async () => {
    let saved = settings
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/settings`]: () => ({ body: saved }),
      [`PATCH ${C}/settings`]: () => {
        saved = { ...settings, billing_day_next: 10, billing_day_next_from: '2026-11-01' }
        return { body: saved }
      },
    })
    renderRoute('/c/c1/settings')
    const day = await screen.findByLabelText('День начисления')
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeDisabled()
    await userEvent.clear(day)
    await userEvent.type(day, '10')
    expect(screen.getByText(/начнёт действовать со следующего месяца/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('День начисления изменится с 01.11.2026: 10-е число')).toBeInTheDocument()
    expect(await bodyOf(calls.find((r) => r.method === 'PATCH')!)).toEqual({ billing_day: 10 })
  })

  it('проверка полей до отправки и ошибка поля с сервера', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/settings`]: { body: settings },
      [`PATCH ${C}/settings`]: apiError(400, 'validation', 'Неизвестный часовой пояс', 'timezone'),
    })
    renderRoute('/c/c1/settings')
    const due = await screen.findByLabelText('Срок оплаты, дней')
    await userEvent.clear(due)
    await userEvent.type(due, '90')
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('От 0 до 60')).toBeInTheDocument()
    expect(calls.some((r) => r.method === 'PATCH')).toBe(false)
    await userEvent.clear(due)
    await userEvent.type(due, '7')
    await userEvent.selectOptions(screen.getByLabelText('Часовой пояс'), 'Asia/Almaty')
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('Неизвестный часовой пояс')).toBeInTheDocument()
  })
})

describe('журнал', () => {
  it('подписи действий', () => {
    expect(actionLabel('ledger.cancel')).toBe('Операция отменена')
    expect(actionLabel('something.new')).toBe('something.new')
  })

  it('фильтры уходят в запрос; система и подробности', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/staff`]: { body: list([{ user_id: 'u1', name: 'Айгуль', phone: '+996700123456', role: 'owner' }]) },
      [`GET ${C}/audit`]: {
        body: list([
          { id: 2, at: '2026-10-19T09:00:00Z', user: { id: 'u1', name: 'Айгуль' }, user_name: 'Айгуль', action: 'ledger.cancel', entity: 'ledger', entity_id: 'l1', before: { amount: 1 }, after: { cancelled: true } },
          { id: 1, at: '2026-10-01T00:05:00Z', user: null, user_name: 'Система', action: 'ledger.charge', entity: 'ledger', entity_id: 'l0', before: null, after: null },
        ]),
      },
    })
    renderRoute('/c/c1/audit')
    expect(await screen.findByText('Операция отменена')).toBeInTheDocument()
    expect(screen.getByText('Система')).toBeInTheDocument()
    await userEvent.click(screen.getByText('Подробнее'))
    expect(screen.getByText(/"cancelled": true/)).toBeInTheDocument()

    await userEvent.selectOptions(screen.getByLabelText('Тип'), 'ledger.')
    await userEvent.selectOptions(await screen.findByLabelText('Сотрудник'), 'u1')
    await waitFor(() => expect(calls.some((r) => r.url.includes('action=ledger.') && r.url.includes('user=u1'))).toBe(true))
    const list2 = screen.getByRole('list', { name: 'Журнал действий' })
    expect(within(list2).getAllByRole('listitem')).toHaveLength(2)
  })
})
