// Журнал: в строке ledger.* и attendance.* — ученик ссылкой и сумма; «Подробнее» — по-русски, суммы в сомах,
// даты 03.11.2026, без id; сырой JSON — «Технические данные», только владельцу.
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { can } from '@/app/permissions'
import { list, meOwner } from '@/test/fixtures'
import { mockApi, renderRoute } from '@/test/utils'
import type { AuditItem } from './api'
import { auditFields, fieldLabel, formatValue } from './auditDetails'

const C = '/api/v1/c/c1'
const S1 = '11111111-1111-4111-8111-111111111111'
const S2 = '22222222-2222-4222-8222-222222222222'

const item = (over: Partial<AuditItem>): AuditItem => ({
  id: 1,
  at: '2026-11-03T08:00:00Z',
  user: { id: 'u1', name: 'Айгуль' },
  user_name: 'Айгуль',
  action: 'ledger.payment',
  entity: 'ledger',
  entity_id: 'l1',
  before: null,
  after: null,
  students: [],
  amount: null,
  lesson: null,
  ...over,
})

const payment = item({
  action: 'ledger.payment',
  after: { student_id: S1, kind: 'payment', amount: 150_000, method: 'cash', comment: null },
  students: [{ id: S1, full_name: 'Айдана Токтосунова' }],
  amount: 150_000,
})
const charge = item({
  id: 2,
  action: 'ledger.charge',
  user: null,
  user_name: 'Система',
  after: { student_id: S1, enrollment_id: 'e1', amount: -315_000, list_price: 350_000, discount: 35_000, period: '2026-12-01', due_date: '2026-12-01', comment: null },
  students: [{ id: S1, full_name: 'Айдана Токтосунова' }],
  amount: -315_000,
})
const cancel = item({
  id: 3,
  action: 'ledger.cancel',
  before: { cancelled: false, kind: 'payment', amount: 150_000, student_id: S1 },
  after: { cancelled: true, reason: 'ошибка кассира' },
  students: [{ id: S1, full_name: 'Айдана Токтосунова' }],
  amount: 150_000,
})
const marks = item({
  id: 4,
  action: 'attendance.mark',
  entity: 'lesson',
  before: { [S1]: null, [S2]: 'present' },
  after: { [S1]: 'present', [S2]: 'absent' },
  students: [
    { id: S2, full_name: 'Асанова София' },
    { id: S1, full_name: 'Токтосунова Айдана' },
  ],
  lesson: { date: '2026-11-03', group_name: 'Английский A1' },
})

describe('расшифровка записи', () => {
  it('значения: сомы, даты, месяц, да/нет, коды по-русски, расписание', () => {
    expect(formatValue('amount', -315_000)).toMatch(/^−3\s150\sс$/)
    expect(formatValue('due_date', '2026-11-03')).toBe('03.11.2026')
    expect(formatValue('period', '2026-12-01')).toBe('Декабрь 2026')
    expect(formatValue('expires_at', '2026-11-03T08:05:00Z')).toMatch(/^03\.11\.2026, \d\d:05$/)
    expect(formatValue('cancelled', true)).toBe('да')
    expect(formatValue('comment', null)).toBe('—')
    expect(formatValue('kind', 'refund')).toBe('Возврат')
    expect(formatValue('method', 'transfer')).toBe('Перевод')
    expect(formatValue('status', 'left')).toBe('Ушёл')
    expect(formatValue('role', 'admin')).toBe('Администратор')
    expect(formatValue('discount', { kind: 'percent', value: 10 })).toBe('10%')
    expect(formatValue('discount', { kind: 'fixed', value: 50_000 })).toMatch(/^500\sс$/)
    expect(formatValue('discount', 35_000)).toMatch(/^350\sс$/) // скидка в начислении — тыйыны
    expect(formatValue('due_days', 7)).toBe('7')
    expect(formatValue('schedule', [{ dow: 1, start: '15:00', end: '16:30' }, { dow: 3, start: '15:00', end: '16:30' }])).toBe('Пн 15:00–16:30, Ср 15:00–16:30')
    expect(formatValue('sent', [S1, S2])).toBe('2 ученика') // id учеников — только сколько
    expect(formatValue('marks', { [S1]: 'present' })).toBe('1 ученик')
    expect(formatValue('reason', 'student.left')).toBe('Ученик ушёл')
  })

  it('оплата: поля по-русски, без student_id', () => {
    expect(auditFields(payment).map((f) => [f.label, f.after])).toEqual([
      ['Вид', 'Оплата'],
      ['Сумма', expect.stringMatching(/^1\s500\sс$/)],
      ['Способ', 'Наличные'],
      ['Комментарий', '—'],
    ])
  })

  it('начисление: цена, скидка, месяц и срок; enrollment_id не показан', () => {
    const f = Object.fromEntries(auditFields(charge).map((x) => [x.label, x.after]))
    expect(f).toMatchObject({ 'За месяц': 'Декабрь 2026', 'Срок оплаты': '01.12.2026' })
    expect(f['Цена по тарифу']).toMatch(/^3\s500\sс$/)
    expect(f['Скидка']).toMatch(/^350\sс$/)
    expect(Object.keys(f).some((k) => k.includes('_id'))).toBe(false)
  })

  it('было/стало — только изменившиеся поля (снимок ученика целиком)', () => {
    const upd = item({
      action: 'student.update',
      entity: 'student',
      before: { full_name: 'Айдана Токтосунова', birth_date: null, status: 'active', discount: null, parent_id: 'p1' },
      after: { full_name: 'Айдана Токтосунова', birth_date: null, status: 'left', discount: { kind: 'percent', value: 10 }, parent_id: 'p2' },
    })
    expect(auditFields(upd)).toEqual([
      { key: 'status', label: 'Статус', before: 'Учится', after: 'Ушёл' },
      { key: 'discount', label: 'Скидка', before: '—', after: '10%' },
    ])
  })

  it('отметки: ученики по имени, «не отмечен» → «Был»', () => {
    expect(auditFields(marks)).toEqual([
      { key: S2, label: 'Асанова София', before: 'Был', after: 'Не был' },
      { key: S1, label: 'Токтосунова Айдана', before: 'не отмечен', after: 'Был' },
    ])
  })

  it('у каждого поля, которое пишет бэкенд в журнал, есть русская подпись', () => {
    // Ключи before/after из audit.Record в backend/internal/** (кроме *_id — их не показываем).
    const keys = `amount kind method comment list_price discount discount_capped period due_date cancelled reason
      full_name birth_date status parent_existing phone timezone billing_day billing_day_next billing_day_next_from due_days
      excused_deducts reminders_on overdue_repeat notify_absent pay_details schedule price lessons_count archived role
      expires_at revoked new_user start_date end_date lessons_left first_month manual_amount billed_through package_imported
      date start_time marks returned_lessons students_created parents_created groups_created enrollments_created skipped
      debt_total all sent messages tg_status consent_at lang link_amount paid_amount sessions_closed other_sessions_closed by`
    for (const k of keys.split(/\s+/)) expect(fieldLabel(k, 'student'), k).not.toBe(k)
    expect(fieldLabel('name', 'group')).toBe('Название')
    expect(fieldLabel('name', 'invite')).toBe('Имя')
  })
})

describe('страница журнала', () => {
  const pageApi = (items: AuditItem[]) =>
    mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/staff`]: { body: list([]) },
      [`GET ${C}/audit`]: { body: list(items) },
    })

  it('в строке — ученик ссылкой на карточку и сумма; у отмены — что отменили; у отметок — занятие', async () => {
    pageApi([payment, charge, cancel, marks])
    renderRoute('/c/c1/audit')
    await screen.findByRole('list', { name: 'Журнал действий' })
    const [pay, chg, cnl, att] = screen.getAllByTestId('audit-subject') as [HTMLElement, HTMLElement, HTMLElement, HTMLElement]

    expect(within(pay).getByRole('link', { name: 'Айдана Токтосунова' })).toHaveAttribute('href', `/c/c1/students/${S1}`)
    expect(within(pay).getByText(/^1\s500\sс$/)).toBeInTheDocument()
    expect(within(chg).getByText(/^−3\s150\sс$/)).toBeInTheDocument()
    expect(within(cnl).getByText(/^Оплата 1\s500\sс$/)).toBeInTheDocument()
    expect(within(att).getByText('Английский A1 · 03.11.2026')).toBeInTheDocument()
    expect(within(att).getByRole('link', { name: 'Асанова София' })).toHaveAttribute('href', `/c/c1/students/${S2}`)
    expect(within(att).getByRole('link', { name: 'Токтосунова Айдана' })).toBeInTheDocument()
  })

  it('«Подробнее» — таблица по-русски без id; «Технические данные» — сырой JSON по кнопке', async () => {
    pageApi([cancel])
    renderRoute('/c/c1/audit')
    await userEvent.click(await screen.findByText('Подробнее'))
    const table = screen.getByRole('table')
    expect(within(table).getByRole('columnheader', { name: 'Было' })).toBeInTheDocument()
    expect(within(table).getByRole('row', { name: 'Отменено нет да' })).toBeInTheDocument()
    expect(within(table).getByRole('row', { name: 'Вид Оплата —' })).toBeInTheDocument()
    expect(within(table).getByRole('row', { name: 'Причина — ошибка кассира' })).toBeInTheDocument()
    expect(table).not.toHaveTextContent(S1)
    expect(table).not.toHaveTextContent('150000')

    const tech = screen.getByRole('button', { name: 'Технические данные' })
    expect(tech).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText(/"student_id"/)).not.toBeInTheDocument()
    await userEvent.click(tech)
    expect(screen.getByText(/"student_id"/)).toBeInTheDocument()
  })

  it('«Технические данные» — только владельцу', () => {
    expect(can.auditRaw('owner')).toBe(true)
    expect(can.auditRaw('admin')).toBe(false)
    expect(can.auditRaw('teacher')).toBe(false)
  })
})
