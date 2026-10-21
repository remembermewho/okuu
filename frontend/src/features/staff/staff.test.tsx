import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { list, meAdmin, meOwner } from '@/test/fixtures'
import { apiError, bodyOf, mockApi, renderRoute, unauthorized } from '@/test/utils'

const C = '/api/v1/c/c1'
const staff = list([
  { user_id: 'u1', name: 'Айгуль', phone: '+996700123456', role: 'owner' },
  { user_id: 'u3', name: 'Нурлан', phone: '+996700333333', role: 'teacher' },
])
const created = {
  id: 'i1',
  role: 'teacher',
  name: 'Нурлан',
  expires_at: '2026-10-27T10:00:00Z',
  created_at: '2026-10-20T10:00:00Z',
  url: 'http://localhost:5173/invite/tok123',
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('сотрудники', () => {
  it('владелец: приглашение — ссылка, копирование и WhatsApp', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/staff`]: { body: staff },
      [`GET ${C}/staff/invites`]: { body: list([]) },
      [`POST ${C}/staff/invites`]: { status: 201, body: created },
    })
    renderRoute('/c/c1/staff')
    expect(await screen.findByText('Нурлан')).toBeInTheDocument()
    expect(screen.getByText('(вы)')).toBeInTheDocument()
    expect(await screen.findByText('Действующих приглашений нет')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Пригласить' }))
    const dialog = await screen.findByRole('dialog', { name: 'Пригласить сотрудника' })
    await userEvent.type(within(dialog).getByLabelText(/Кого приглашаете/), 'Нурлан')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Создать ссылку' }))
    const link = await screen.findByRole('dialog', { name: 'Ссылка-приглашение' })
    expect(within(link).getByLabelText('Ссылка')).toHaveValue(created.url)
    expect(within(link).getByRole('link', { name: 'Отправить в WhatsApp' }).getAttribute('href')).toContain(encodeURIComponent(created.url))
    expect(await bodyOf(calls.find((r) => r.method === 'POST')!)).toEqual({ role: 'teacher', name: 'Нурлан' })
  })

  it('смена роли и удаление (teacher_has_groups — тост)', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/staff`]: { body: staff },
      [`GET ${C}/staff/invites`]: { body: list([]) },
      [`PATCH ${C}/staff/u3`]: { body: { ...staff.items[1], role: 'admin' } },
      [`DELETE ${C}/staff/u3`]: apiError(409, 'teacher_has_groups', 'groups'),
    })
    renderRoute('/c/c1/staff')
    await userEvent.click(await screen.findByRole('button', { name: 'Действия: Нурлан' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Сделать: администратор' }))
    await waitFor(() => expect(calls.some((r) => r.method === 'PATCH')).toBe(true))
    expect(await bodyOf(calls.find((r) => r.method === 'PATCH')!)).toEqual({ role: 'admin' })

    await userEvent.click(screen.getByRole('button', { name: 'Действия: Нурлан' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Удалить из центра' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Удалить из центра' }))
    expect(await screen.findByText(/сначала назначьте им другого преподавателя/)).toBeInTheDocument()
  })

  it('админ видит список без управления; приглашения не запрашиваются', async () => {
    const { calls } = mockApi({ 'GET /api/v1/me': { body: meAdmin }, [`GET ${C}/staff`]: { body: staff } })
    renderRoute('/c/c1/staff')
    expect(await screen.findByText('Нурлан')).toBeInTheDocument()
    expect(screen.getByText('Приглашать и менять роли может владелец.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Пригласить' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Действия:/ })).not.toBeInTheDocument()
    expect(calls.some((r) => r.url.includes('/staff/invites'))).toBe(false)
  })
})

describe('страница приглашения', () => {
  const pub = { center_name: 'Билим', role: 'teacher', name: 'Нурлан', expires_at: '2026-10-27T10:00:00Z' }

  it('недействительная ссылка', async () => {
    mockApi({ 'GET /api/v1/me': unauthorized, 'GET /api/v1/invites/bad': apiError(404, 'invite_invalid', 'no') })
    renderRoute('/invite/bad')
    expect(await screen.findByRole('heading', { name: 'Приглашение недействительно' })).toBeInTheDocument()
  })

  it('новый сотрудник: регистрация и переход в центр', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': unauthorized,
      'GET /api/v1/invites/tok': { body: pub },
      'POST /api/v1/invites/tok/accept': { body: { id: 'u3', name: 'Нурлан', phone: '+996700333333', centers: [{ id: 'c1', name: 'Билим', role: 'teacher' }] } },
      [`GET ${C}/lessons`]: { body: list([]) },
    })
    const { router } = renderRoute('/invite/tok')
    expect(await screen.findByText('Вас приглашают в «Билим» — преподаватель.')).toBeInTheDocument()
    expect(screen.getByLabelText('Ваше имя')).toHaveValue('Нурлан')
    await userEvent.type(screen.getByLabelText('Телефон'), '0700 333 333')
    await userEvent.type(screen.getByLabelText('Пароль'), 'secret-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Принять приглашение' }))
    await waitFor(() => expect(router.state.location.pathname).toMatch(/^\/c\/c1/))
    expect(await bodyOf(calls.find((r) => r.method === 'POST')!)).toEqual({ name: 'Нурлан', phone: '+996700333333', password: 'secret-pass' })
  })

  it('телефон уже зарегистрирован — предлагаем войти', async () => {
    mockApi({
      'GET /api/v1/me': unauthorized,
      'GET /api/v1/invites/tok': { body: pub },
      'POST /api/v1/invites/tok/accept': apiError(409, 'login_required', 'login'),
    })
    renderRoute('/invite/tok')
    await userEvent.type(await screen.findByLabelText('Телефон'), '0700 333 333')
    await userEvent.type(screen.getByLabelText('Пароль'), 'secret-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Принять приглашение' }))
    expect(await screen.findByText('Этот телефон уже зарегистрирован. Войдите — и приглашение примется.')).toBeInTheDocument()
    const links = screen.getAllByRole('link', { name: 'Войти и принять' })
    expect(links[0]).toHaveAttribute('href', '/login?next=%2Finvite%2Ftok')
  })

  it('вошедший: принять без формы — в новый центр', async () => {
    const before = { ...meOwner, centers: [{ id: 'c9', name: 'Свой центр', role: 'owner' }] }
    const { calls } = mockApi({
      'GET /api/v1/me': { body: before },
      'GET /api/v1/invites/tok': { body: pub },
      'POST /api/v1/invites/tok/accept': { body: { ...before, centers: [...before.centers, { id: 'c1', name: 'Билим', role: 'teacher' }] } },
      [`GET ${C}/lessons`]: { body: list([]) },
    })
    const { router } = renderRoute('/invite/tok')
    expect(await screen.findByText('Вы вошли как Айгуль.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Принять приглашение' }))
    await waitFor(() => expect(router.state.location.pathname).toMatch(/^\/c\/c1/))
    const post = calls.find((r) => r.method === 'POST')!
    expect(await post.text()).toBe('')
  })
})
