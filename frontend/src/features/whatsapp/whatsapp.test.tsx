import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { center, group, list, meOwner } from '@/test/fixtures'
import { apiError, mockApi, renderRoute } from '@/test/utils'

const C = '/api/v1/c/c1'
const URL_WA = 'https://wa.me/996700333444?text=hello'

const debtor = (over: object) => ({
  student_id: 's1',
  full_name: 'Бекова Айжан',
  status: 'active',
  groups: [{ id: 'g1', name: 'Английский A1' }],
  parent: { name: 'Нургуль', phone: '+996700333444', tg_status: 'none', last_reminder_at: null, whatsapp_phone: '996700333444' },
  debt: 150_000,
  days_overdue: 3,
  oldest_due_date: '2026-10-15',
  due_in_days: 0,
  reminded_today: false,
  whatsapp_opened_today: false,
  ...over,
})
const page = (items: object[]) => ({
  items,
  next_cursor: null,
  summary: { period: '2026-10-01', charged: 0, collected: 0, debt_total: 150_000, debtors_count: 1, pending_total: 0, pending_count: 0 },
})
const message = { parent_id: 'p1', parent_name: 'Нургуль', phone: '996700333444', students: [{ id: 's1', full_name: 'Бекова Айжан', debt: 150_000 }], total: 150_000, text: '*Напоминание*', url: URL_WA, opened_today: false }

const base = (items: object[], me: object = meOwner) => ({
  'GET /api/v1/me': { body: me },
  [`GET ${C}/debtors`]: (req: Request) => ({ body: new URL(req.url).searchParams.get('status') === 'pending' ? page([]) : page(items) }),
  [`GET ${C}/groups`]: { body: list([group]) },
})

describe('WhatsApp в «Должниках»', () => {
  let tab: { opener: unknown; location: { href: string }; close: ReturnType<typeof vi.fn> }

  beforeEach(() => {
    sessionStorage.clear()
    tab = { opener: {}, location: { href: '' }, close: vi.fn() }
    vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
  })
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('кнопка открывает ссылку в новой вкладке и записывает открытие', async () => {
    const { calls } = mockApi({ ...base([debtor({})]), [`POST ${C}/students/s1/whatsapp-reminder`]: { body: { ...message, opened_today: true } } })
    renderRoute('/c/c1/debtors')
    await userEvent.click(await screen.findByRole('button', { name: 'WhatsApp: Бекова Айжан' }))
    await waitFor(() => expect(tab.location.href).toBe(URL_WA))
    expect(window.open).toHaveBeenCalledWith('', '_blank')
    expect(tab.opener).toBeNull()
    expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/whatsapp-reminder'))).toBe(true)
  })

  it('отметка «открыто в WhatsApp» в строке', async () => {
    mockApi(base([debtor({ whatsapp_opened_today: true })]))
    renderRoute('/c/c1/debtors')
    const row = (await screen.findByText('Бекова Айжан')).closest('li')!
    expect(within(row).getByText('открыто в WhatsApp')).toBeInTheDocument()
  })

  it('браузер не открыл вкладку — окно с текстом и ссылкой', async () => {
    vi.mocked(window.open).mockReturnValue(null)
    mockApi({ ...base([debtor({})]), [`POST ${C}/students/s1/whatsapp-reminder`]: { body: message } })
    renderRoute('/c/c1/debtors')
    await userEvent.click(await screen.findByRole('button', { name: 'WhatsApp: Бекова Айжан' }))
    const dialog = await screen.findByRole('dialog', { name: 'Текст для WhatsApp' })
    expect(within(dialog).getByRole('link', { name: 'Открыть WhatsApp' })).toHaveAttribute('href', URL_WA)
  })

  it('ошибка сервера — вкладка закрывается', async () => {
    mockApi({ ...base([debtor({})]), [`POST ${C}/students/s1/whatsapp-reminder`]: apiError(409, 'no_debt', 'нет') })
    renderRoute('/c/c1/debtors')
    await userEvent.click(await screen.findByRole('button', { name: 'WhatsApp: Бекова Айжан' }))
    await waitFor(() => expect(tab.close).toHaveBeenCalled())
    expect(await screen.findByText('У ученика нет долга — напоминать не о чем.')).toBeInTheDocument()
  })

  it('нет телефона — кнопка неактивна; телефон некорректный — кнопки нет, копировать можно', async () => {
    mockApi(
      base([
        debtor({ student_id: 's1', full_name: 'Без Телефона', parent: { name: 'Н', phone: '', tg_status: 'none', last_reminder_at: null, whatsapp_phone: null } }),
        debtor({ student_id: 's2', full_name: 'Плохой Номер', parent: { name: 'Н', phone: '12345', tg_status: 'none', last_reminder_at: null, whatsapp_phone: null } }),
      ]),
    )
    renderRoute('/c/c1/debtors')
    const button = await screen.findByRole('button', { name: 'WhatsApp: Без Телефона' })
    expect(button).toBeDisabled()
    expect(button).toHaveAttribute('title', 'У родителя нет телефона')
    expect(screen.queryByRole('button', { name: 'WhatsApp: Плохой Номер' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Скопировать текст: Плохой Номер' })).toBeEnabled()
  })

  it('«Скопировать текст» не записывает открытие', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const { calls } = mockApi({ ...base([debtor({})]), [`GET ${C}/students/s1/whatsapp-reminder`]: { body: message } })
    renderRoute('/c/c1/debtors')
    await userEvent.click(await screen.findByRole('button', { name: 'Скопировать текст: Бекова Айжан' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('*Напоминание*'))
    expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/whatsapp-reminder'))).toBe(false)
    expect(window.open).not.toHaveBeenCalled()
  })

  it('демо-центр — текст в окне, ссылка не открывается', async () => {
    const demo = { ...meOwner, centers: [{ ...center('owner'), is_demo: true }] }
    const { calls } = mockApi({ ...base([debtor({})], demo), [`GET ${C}/students/s1/whatsapp-reminder`]: { body: message } })
    renderRoute('/c/c1/debtors')
    await userEvent.click(await screen.findByRole('button', { name: 'WhatsApp: Бекова Айжан' }))
    const dialog = await screen.findByRole('dialog', { name: 'Текст для WhatsApp' })
    expect(within(dialog).getByRole('textbox')).toHaveValue('*Напоминание*')
    expect(within(dialog).queryByRole('link')).not.toBeInTheDocument()
    expect(window.open).not.toHaveBeenCalled()
    expect(calls.some((r) => r.method === 'POST')).toBe(false)
  })

  const remindAll = async () => {
    await userEvent.click(await screen.findByRole('button', { name: 'Напомнить всем' }))
    const confirm = await screen.findByRole('dialog')
    await userEvent.click(within(confirm).getByRole('button', { name: 'Напомнить всем' }))
    return screen.findByRole('dialog', { name: 'Ещё 2 родителя без Telegram — отправьте в WhatsApp' })
  }
  const queueReply = {
    sent: 1,
    messages: 1,
    students_in_messages: 1,
    skipped: [
      { student_id: 's1', reason: 'no_telegram' },
      { student_id: 's9', reason: 'no_telegram' },
      { student_id: 's3', reason: 'rate_limited' },
    ],
    whatsapp: [
      message,
      { ...message, parent_id: 'p9', parent_name: 'Без Номера', phone: null, url: null, students: [{ id: 's9', full_name: 'Дети Без Номера', debt: 100_000 }], total: 100_000 },
    ],
  }

  it('«Напомнить всем»: окно «ещё N», клик отмечает строку, список переживает перезагрузку', async () => {
    const { calls } = mockApi({
      ...base([debtor({})]),
      [`POST ${C}/reminders`]: { body: queueReply },
      [`POST ${C}/students/s1/whatsapp-reminder`]: { body: message },
    })
    const first = renderRoute('/c/c1/debtors')
    const dialog = await remindAll()
    expect(within(dialog).getByText('Отправлено 1 родителю (учеников в сообщении: 1)')).toBeInTheDocument()
    // Кто в списке WhatsApp, тот не в «Не отправлено»; с причиной rate_limited — там.
    expect(within(dialog).getAllByText('Не отправлено')).toHaveLength(1)
    expect(within(dialog).queryByText('родитель не подключён к Telegram')).not.toBeInTheDocument()
    expect(within(dialog).getByText('уже напоминали меньше часа назад')).toBeInTheDocument()
    expect(within(dialog).getByText('нет телефона')).toBeInTheDocument()

    const row = within(dialog).getByText('Бекова Айжан').closest('li')!
    const link = within(row).getByRole('link', { name: 'WhatsApp' })
    expect(link).toHaveAttribute('href', URL_WA)
    expect(link).toHaveAttribute('target', '_blank')
    link.addEventListener('click', (e) => e.preventDefault()) // jsdom не умеет переходить по ссылке
    await userEvent.click(link)
    expect(await within(row).findByText('открыто')).toBeInTheDocument()
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/s1/whatsapp-reminder'))).toBe(true))

    // Браузер выгрузил страницу, пока был открыт WhatsApp: список и отметка на месте.
    first.unmount()
    renderRoute('/c/c1/debtors')
    const again = await screen.findByRole('dialog', { name: 'Ещё 2 родителя без Telegram — отправьте в WhatsApp' })
    expect(within(within(again).getByText('Бекова Айжан').closest('li')!).getByText('открыто')).toBeInTheDocument()

    // Закрыли сами — больше не появляется.
    await userEvent.click(within(again).getAllByRole('button', { name: 'Закрыть' }).at(-1)!)
    expect(sessionStorage.length).toBe(0)
  })
})
