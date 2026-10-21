import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { center, list, meOwner } from '@/test/fixtures'
import { apiError, bodyOf, mockApi, renderRoute, unauthorized } from '@/test/utils'

afterEach(() => {
  vi.unstubAllGlobals()
})

const me = { ...meOwner, centers: [center('owner'), center('teacher', 'c2', 'Второй центр')] }

describe('профиль', () => {
  it('имя, телефон только для чтения, центры и роли; сохранение имени', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: me },
      'PATCH /api/v1/me': async (req) => ({ body: { ...me, ...((await req.clone().json()) as object) } }),
    })
    renderRoute('/account')
    const name = await screen.findByLabelText('Имя')
    expect(name).toHaveValue('Айгуль')
    expect(screen.getByLabelText('Телефон')).toHaveValue('+996 700 123 456')
    expect(screen.getByLabelText('Телефон')).toHaveAttribute('readonly')
    expect(screen.getByRole('link', { name: /Второй центр/ })).toHaveAttribute('href', '/c/c2')
    expect(screen.getByText('Преподаватель')).toBeInTheDocument()

    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeDisabled()
    await userEvent.clear(name)
    await userEvent.type(name, 'Айгуль Асанова')
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('Имя сохранено')).toBeInTheDocument()
    expect(await bodyOf(calls.find((r) => r.method === 'PATCH')!)).toEqual({ name: 'Айгуль Асанова' })
  })

  it('смена пароля: повтор должен совпасть, неверный текущий — под полем', async () => {
    let attempt = 0
    const { calls } = mockApi({
      'GET /api/v1/me': { body: me },
      'POST /api/v1/me/password': () =>
        ++attempt === 1 ? apiError(400, 'validation', 'Текущий пароль неверный', 'current_password') : { status: 204 },
    })
    renderRoute('/account')
    await userEvent.click(await screen.findByRole('button', { name: 'Сменить пароль' }))
    const dialog = await screen.findByRole('dialog', { name: 'Сменить пароль' })
    await userEvent.type(within(dialog).getByLabelText('Текущий пароль'), 'old-secret')
    await userEvent.type(within(dialog).getByLabelText('Новый пароль'), 'new-secret-1')
    await userEvent.type(within(dialog).getByLabelText('Повторите новый пароль'), 'new-secret-2')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Сохранить' }))
    expect(await within(dialog).findByText('Пароли не совпадают')).toBeInTheDocument()
    expect(calls.some((r) => r.url.endsWith('/me/password'))).toBe(false)

    // Глаз есть у каждого поля пароля.
    expect(within(dialog).getAllByRole('button', { name: 'Показать пароль' })).toHaveLength(3)

    await userEvent.clear(within(dialog).getByLabelText('Повторите новый пароль'))
    await userEvent.type(within(dialog).getByLabelText('Повторите новый пароль'), 'new-secret-1')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Сохранить' }))
    expect(await within(dialog).findByText('Текущий пароль неверный')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('Текущий пароль')).toHaveAttribute('aria-invalid', 'true')

    await userEvent.click(within(dialog).getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText(/Пароль изменён/)).toBeInTheDocument()
    expect(await bodyOf(calls.filter((r) => r.url.endsWith('/me/password'))[1]!)).toEqual({ current_password: 'old-secret', new_password: 'new-secret-1' })
  })

  it('«Выйти на всех устройствах» — подтверждение и переход на вход', async () => {
    let loggedIn = true
    mockApi({
      'GET /api/v1/me': () => (loggedIn ? { body: me } : unauthorized),
      'POST /api/v1/auth/logout-all': () => ((loggedIn = false), { status: 204 }),
    })
    const { router } = renderRoute('/account')
    await userEvent.click(await screen.findByRole('button', { name: 'Выйти на всех устройствах' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Выйти на всех устройствах' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('«Профиль» есть на странице «Ещё»', async () => {
    mockApi({ 'GET /api/v1/me': { body: me } })
    renderRoute('/c/c1/more')
    expect(await screen.findByRole('link', { name: 'Профиль' })).toHaveAttribute('href', '/account')
  })

  it('пункт «Профиль» в меню аккаунта', async () => {
    mockApi({ 'GET /api/v1/me': { body: me }, 'GET /api/v1/c/c1/debtors': { body: { ...list([]), summary: { period: '2026-10-01', charged: 0, collected: 0, debt_total: 0, debtors_count: 0, pending_total: 0, pending_count: 0 } } }, 'GET /api/v1/c/c1/groups': { body: list([]) } })
    const { router } = renderRoute('/c/c1/debtors')
    await userEvent.click(await screen.findByRole('button', { name: 'Аккаунт' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Профиль' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/account'))
    expect(await screen.findByRole('link', { name: 'Назад' })).toHaveAttribute('href', '/c/c1/debtors')
  })
})

describe('сброс пароля по ссылке', () => {
  it('новый пароль → «Пароль изменён» и вход (ссылка после этого уже недействительна)', async () => {
    let used = false
    const { calls } = mockApi({
      'GET /api/v1/password-reset/tok': () => (used ? apiError(404, 'reset_invalid', 'no') : { body: { name: 'Нурлан', center_name: 'Билим' } }),
      'POST /api/v1/password-reset/tok': () => ((used = true), { status: 204 }),
    })
    renderRoute('/reset/tok')
    expect(await screen.findByText('Нурлан, центр «Билим».')).toBeInTheDocument()
    await userEvent.type(screen.getByLabelText('Новый пароль'), 'short')
    await userEvent.type(screen.getByLabelText('Повторите новый пароль'), 'short')
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить пароль' }))
    expect(await screen.findByText('Пароль — минимум 8 символов')).toBeInTheDocument()

    await userEvent.clear(screen.getByLabelText('Новый пароль'))
    await userEvent.type(screen.getByLabelText('Новый пароль'), 'reset-pass-1')
    await userEvent.clear(screen.getByLabelText('Повторите новый пароль'))
    await userEvent.type(screen.getByLabelText('Повторите новый пароль'), 'reset-pass-1')
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить пароль' }))
    expect(await screen.findByRole('heading', { name: 'Пароль изменён' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Войти' })).toHaveAttribute('href', '/login')
    expect(await bodyOf(calls.find((r) => r.method === 'POST')!)).toEqual({ new_password: 'reset-pass-1' })
  })

  it('недействительная ссылка', async () => {
    mockApi({ 'GET /api/v1/password-reset/old': apiError(404, 'reset_invalid', 'no') })
    renderRoute('/reset/old')
    expect(await screen.findByRole('heading', { name: 'Ссылка недействительна' })).toBeInTheDocument()
  })
})
