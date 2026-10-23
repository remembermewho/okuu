import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { list, meOwner } from '@/test/fixtures'
import { bodyOf, mockApi, renderRoute } from '@/test/utils'

const meAdmin = { ...meOwner, is_platform_admin: true }
const A = '/api/v1/admin'

const centerRow = (over: object = {}) => ({
  id: 'c9',
  name: 'Балапан',
  status: 'active',
  blocked_reason: null,
  plan: 'start',
  paid_until: '2026-10-01',
  payment_overdue: true,
  is_demo: false,
  created_at: '2026-09-01T10:00:00+06:00',
  owner: { name: 'Нургуль', phone: '+996700555111' },
  active_students: 42,
  last_activity: '2026-10-18T09:30:00+06:00',
  ...over,
})
const card = (over: object = {}) => ({
  ...centerRow(),
  timezone: 'Asia/Bishkek',
  students: { active: 42, paused: 2, left: 5 },
  groups: 4,
  staff: 3,
  telegram_parents: 30,
  audit: [],
  ...over,
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('панель платформы', () => {
  it('не суперадмин — «не найдено», в меню пункта нет', async () => {
    const { calls } = mockApi({ 'GET /api/v1/me': { body: meOwner } })
    renderRoute('/admin')
    expect(await screen.findByText('Не найдено')).toBeInTheDocument()
    expect(calls.some((r) => r.url.includes('/admin/'))).toBe(false)
  })

  it('таблица центров: просрочка подсвечена, фильтр по статусу уходит в запрос', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meAdmin },
      [`GET ${A}/centers`]: { body: list([centerRow(), centerRow({ id: 'c8', name: 'Демо', is_demo: true, status: 'pending', paid_until: null, payment_overdue: false })]) },
    })
    renderRoute('/admin')
    expect(await screen.findByRole('link', { name: 'Балапан' })).toHaveAttribute('href', '/admin/centers/c9')
    expect(screen.getByText(/01\.10\.2026 · просрочено/)).toBeInTheDocument()
    expect(screen.getByText('Заявка')).toBeInTheDocument()
    expect(screen.getAllByText('Демо').length).toBeGreaterThan(0)
    await userEvent.selectOptions(screen.getByLabelText('Статус'), 'pending')
    await waitFor(() => expect(calls.some((r) => r.url.includes('status=pending'))).toBe(true))
  })

  it('карточка: одобрить заявку, приостановить с причиной, тариф и «оплачено до»', async () => {
    let current = card({ status: 'pending' })
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meAdmin },
      [`GET ${A}/centers/c9`]: () => ({ body: current }),
      [`POST ${A}/centers/c9/approve`]: () => {
        current = card({ status: 'active' })
        return { body: current }
      },
      [`POST ${A}/centers/c9/block`]: () => {
        current = card({ status: 'blocked', blocked_reason: 'Не оплачен' })
        return { body: current }
      },
      [`PATCH ${A}/centers/c9`]: () => ({ body: (current = card({ plan: 'center', paid_until: '2026-12-31', payment_overdue: false })) }),
      [`GET ${A}/centers`]: { body: list([]) },
    })
    renderRoute('/admin/centers/c9')
    expect(await screen.findByRole('heading', { name: 'Балапан' })).toBeInTheDocument()
    expect(screen.getByText('учатся 42, на паузе 2, ушли 5')).toBeInTheDocument()
    expect(screen.getByText(/только количество/)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Одобрить заявку' }))
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Одобрить заявку' })).not.toBeInTheDocument())

    await userEvent.click(screen.getByRole('button', { name: 'Приостановить доступ' }))
    const dialog = await screen.findByRole('dialog', { name: 'Приостановить доступ к центру' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Приостановить доступ' }))
    expect(await within(dialog).findByText('Заполните поле')).toBeInTheDocument()
    await userEvent.type(within(dialog).getByLabelText(/Причина/), 'Не оплачен')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Приостановить доступ' }))
    expect(await screen.findByRole('button', { name: 'Вернуть доступ' })).toBeInTheDocument()
    const block = calls.find((r) => r.url.endsWith('/block'))!
    expect(await bodyOf(block)).toEqual({ reason: 'Не оплачен' })

    await userEvent.selectOptions(screen.getByLabelText('Тариф'), 'center')
    const date = screen.getByLabelText(/Оплачено до/)
    await userEvent.clear(date)
    await userEvent.type(date, '2026-12-31')
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    await waitFor(() => expect(calls.some((r) => r.method === 'PATCH')).toBe(true))
    expect(await bodyOf(calls.find((r) => r.method === 'PATCH')!)).toEqual({ plan: 'center', paid_until: '2026-12-31' })
  })

  it('ссылка сброса пароля владельца — диалог со ссылкой', async () => {
    mockApi({
      'GET /api/v1/me': { body: meAdmin },
      [`GET ${A}/centers/c9`]: { body: card() },
      [`POST ${A}/centers/c9/owner-password-reset`]: { status: 201, body: { url: 'http://localhost:5173/reset/rst', user_name: 'Нургуль', expires_at: '2026-10-20T10:00:00Z' } },
    })
    renderRoute('/admin/centers/c9')
    await userEvent.click(await screen.findByRole('button', { name: 'Ссылка сброса пароля владельца' }))
    const dialog = await screen.findByRole('dialog', { name: 'Ссылка для смены пароля' })
    expect(within(dialog).getByDisplayValue('http://localhost:5173/reset/rst')).toBeInTheDocument()
  })

  it('ссылки регистрации: создать с пометкой — ссылка один раз; отозвать действующую', async () => {
    const links = [
      { id: 'l1', note: 'Билим, Айгуль', status: 'active', created_at: '2026-10-18T10:00:00Z', expires_at: '2026-11-01T10:00:00Z', used_at: null, revoked_at: null, center: null, created_by_name: 'Админ' },
      { id: 'l2', note: 'Садик', status: 'used', created_at: '2026-10-10T10:00:00Z', expires_at: '2026-10-24T10:00:00Z', used_at: '2026-10-11T10:00:00Z', revoked_at: null, center: { id: 'c9', name: 'Балапан' }, created_by_name: null },
    ]
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meAdmin },
      [`GET ${A}/registration-links`]: { body: list(links) },
      [`POST ${A}/registration-links`]: { status: 201, body: { id: 'l3', note: 'Новый центр', url: 'http://localhost:5173/register/tok', expires_at: '2026-11-06T10:00:00Z' } },
      [`DELETE ${A}/registration-links/l1`]: { status: 204 },
    })
    renderRoute('/admin/links')
    expect(await screen.findByText('Билим, Айгуль')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Балапан' })).toHaveAttribute('href', '/admin/centers/c9')
    expect(screen.getAllByRole('button', { name: 'Отозвать' })).toHaveLength(1) // только у действующей

    await userEvent.type(screen.getByLabelText(/Для кого/), 'Новый центр')
    await userEvent.click(screen.getByRole('button', { name: 'Создать ссылку' }))
    const dialog = await screen.findByRole('dialog', { name: 'Ссылка регистрации' })
    expect(within(dialog).getByDisplayValue('http://localhost:5173/register/tok')).toBeInTheDocument()
    expect(await bodyOf(calls.find((r) => r.method === 'POST')!)).toEqual({ note: 'Новый центр' })
    await userEvent.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Ссылка регистрации' })).not.toBeInTheDocument())

    await userEvent.click(screen.getByRole('button', { name: 'Отозвать' }))
    const confirm = await screen.findByRole('dialog', { name: 'Отозвать ссылку?' })
    await userEvent.click(within(confirm).getByRole('button', { name: 'Отозвать' }))
    await waitFor(() => expect(calls.some((r) => r.method === 'DELETE' && r.url.endsWith('/l1'))).toBe(true))
  })

  it('суперадмин без своих центров — с главной сразу в панель', async () => {
    mockApi({ 'GET /api/v1/me': { body: { ...meAdmin, centers: [] } }, [`GET ${A}/centers`]: { body: list([]) } })
    const { router } = renderRoute('/')
    await waitFor(() => expect(router.state.location.pathname).toBe('/admin'))
    expect(await screen.findByText('Центров не найдено')).toBeInTheDocument()
  })
})
