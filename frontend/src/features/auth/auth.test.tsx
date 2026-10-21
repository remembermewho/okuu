import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiError, bodyOf, mockApi, renderRoute, unauthorized } from '@/test/utils'

const me = { id: 'u1', name: 'Айгуль', phone: '+996700123456', centers: [{ id: 'c1', name: 'Билим', role: 'owner' }] }

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('вход', () => {
  it('ошибки полей до отправки', async () => {
    mockApi({ 'GET /api/v1/me': unauthorized })
    renderRoute('/login')
    await userEvent.click(await screen.findByRole('button', { name: 'Войти' }))
    expect(await screen.findAllByText('Заполните поле')).toHaveLength(2) // телефон и пароль
    expect(screen.getByLabelText('Телефон')).toHaveAttribute('aria-invalid', 'true')
    await userEvent.type(screen.getByLabelText('Телефон'), '12345')
    await userEvent.click(screen.getByRole('button', { name: 'Войти' }))
    expect(await screen.findByText('Телефон: +996XXXXXXXXX или 0XXXXXXXXX')).toBeInTheDocument()
  })

  it('invalid_credentials — сообщение под формой', async () => {
    mockApi({
      'GET /api/v1/me': unauthorized,
      'POST /api/v1/auth/login': apiError(401, 'invalid_credentials', 'Неверный телефон или пароль'),
    })
    const { router } = renderRoute('/login')
    await userEvent.type(await screen.findByLabelText('Телефон'), '0700 123 456')
    await userEvent.type(screen.getByLabelText('Пароль'), 'wrong-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Войти' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Неверный телефон или пароль.')
    expect(router.state.location.pathname).toBe('/login')
  })

  it('429 — через сколько минут повторить', async () => {
    mockApi({
      'GET /api/v1/me': unauthorized,
      'POST /api/v1/auth/login': { ...apiError(429, 'too_many_attempts', 'Слишком много попыток'), headers: { 'Retry-After': '540' } },
    })
    renderRoute('/login')
    await userEvent.type(await screen.findByLabelText('Телефон'), '0700123456')
    await userEvent.type(screen.getByLabelText('Пароль'), 'secret-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Войти' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Попробуйте через 9 мин.')
  })

  it('успешный вход — на страницу из next', async () => {
    let loggedIn = false
    const { calls } = mockApi({
      'GET /api/v1/me': () => (loggedIn ? { body: me } : unauthorized),
      'POST /api/v1/auth/login': () => {
        loggedIn = true
        return { body: me }
      },
    })
    const { router } = renderRoute('/login?next=%2Fc%2Fc1%2Fstudents')
    await userEvent.type(await screen.findByLabelText('Телефон'), '0700 123 456')
    await userEvent.type(screen.getByLabelText('Пароль'), 'secret-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Войти' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/c/c1/students'))
    const login = calls.find((r) => r.url.endsWith('/api/v1/auth/login'))
    expect(await bodyOf(login!)).toEqual({ phone: '0700 123 456', password: 'secret-pass' })
  })

  it('чужой next не используется (открытый редирект)', async () => {
    mockApi({ 'GET /api/v1/me': { body: me } })
    const { router } = renderRoute('/login?next=%2F%2Fevil.example')
    await waitFor(() => expect(router.state.location.pathname).toBe('/c/c1/debtors'))
  })
})

describe('регистрация', () => {
  it('ошибка поля с сервера — под полем', async () => {
    mockApi({
      'GET /api/v1/me': unauthorized,
      'POST /api/v1/auth/register': apiError(400, 'validation', 'Пароль — минимум 8 символов', 'password'),
    })
    renderRoute('/register')
    await userEvent.type(await screen.findByLabelText('Название центра'), 'Билим')
    await userEvent.type(screen.getByLabelText('Ваше имя'), 'Айгуль')
    await userEvent.type(screen.getByLabelText('Телефон'), '0700123456')
    await userEvent.type(screen.getByLabelText('Пароль'), 'longenough')
    await userEvent.click(screen.getByRole('button', { name: 'Создать центр' }))
    const field = screen.getByLabelText('Пароль')
    await waitFor(() => expect(field).toHaveAttribute('aria-invalid', 'true'))
    expect(screen.getByText('Пароль — минимум 8 символов')).toBeInTheDocument()
  })

  it('phone_taken — под телефоном', async () => {
    mockApi({
      'GET /api/v1/me': unauthorized,
      'POST /api/v1/auth/register': apiError(409, 'phone_taken', 'Этот телефон уже зарегистрирован'),
    })
    renderRoute('/register')
    await userEvent.type(await screen.findByLabelText('Название центра'), 'Билим')
    await userEvent.type(screen.getByLabelText('Ваше имя'), 'Айгуль')
    await userEvent.type(screen.getByLabelText('Телефон'), '0700123456')
    await userEvent.type(screen.getByLabelText('Пароль'), 'secret-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Создать центр' }))
    expect(await screen.findByText('Этот телефон уже зарегистрирован — войдите.')).toBeInTheDocument()
    expect(screen.getByLabelText('Телефон')).toHaveAttribute('aria-invalid', 'true')
  })

  it('короткий пароль — до отправки', async () => {
    const { calls } = mockApi({ 'GET /api/v1/me': unauthorized })
    renderRoute('/register')
    await userEvent.type(await screen.findByLabelText('Пароль'), 'short')
    await userEvent.click(screen.getByRole('button', { name: 'Создать центр' }))
    expect(await screen.findByText('Пароль — минимум 8 символов')).toBeInTheDocument()
    expect(calls.some((r) => r.url.includes('/auth/register'))).toBe(false)
  })
})

describe('защищённые маршруты', () => {
  it('без сессии — на вход с возвратом', async () => {
    mockApi({ 'GET /api/v1/me': unauthorized })
    const { router } = renderRoute('/c/c1/students')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe('?next=%2Fc%2Fc1%2Fstudents')
  })

  it('преподаватель — только «Сегодня» и «Группы»', async () => {
    mockApi({ 'GET /api/v1/me': { body: { ...me, centers: [{ id: 'c1', name: 'Билим', role: 'teacher' }] } } })
    const { router } = renderRoute('/')
    await waitFor(() => expect(router.state.location.pathname).toBe('/c/c1/today'))
    const navs = screen.getAllByRole('navigation', { name: 'Главное меню' })
    for (const nav of navs) {
      const links = Array.from(nav.querySelectorAll('a')).map((a) => a.textContent)
      expect(links).toEqual(['Сегодня', 'Группы'])
    }
  })

  it('преподаватель на чужом разделе — «нет доступа»', async () => {
    mockApi({ 'GET /api/v1/me': { body: { ...me, centers: [{ id: 'c1', name: 'Билим', role: 'teacher' }] } } })
    renderRoute('/c/c1/debtors')
    expect(await screen.findByText('Нет доступа')).toBeInTheDocument()
  })

  it('выход — на страницу входа', async () => {
    let loggedIn = true
    mockApi({
      'GET /api/v1/me': () => (loggedIn ? { body: me } : unauthorized),
      'POST /api/v1/auth/logout': () => {
        loggedIn = false
        return { status: 204 }
      },
    })
    const { router } = renderRoute('/c/c1/more')
    await userEvent.click(await screen.findAllByRole('button', { name: 'Выйти' }).then((b) => b[0]!))
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })
})
