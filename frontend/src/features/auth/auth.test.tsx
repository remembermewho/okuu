import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { center } from '@/test/fixtures'
import { apiError, bodyOf, mockApi, renderRoute, unauthorized } from '@/test/utils'

const me = { id: 'u1', name: 'Айгуль', phone: '+996700123456', centers: [center('owner')], is_platform_admin: false }

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

const openReg = { body: { mode: 'open', support_whatsapp: '996555000100' } }
const inviteReg = { body: { mode: 'invite', support_whatsapp: '996555000100' } }

describe('регистрация', () => {
  it('ошибка поля с сервера — под полем', async () => {
    mockApi({
      'GET /api/v1/me': unauthorized,
      'GET /api/v1/auth/registration': openReg,
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
      'GET /api/v1/auth/registration': openReg,
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
    const { calls } = mockApi({ 'GET /api/v1/me': unauthorized, 'GET /api/v1/auth/registration': openReg })
    renderRoute('/register')
    await userEvent.type(await screen.findByLabelText('Пароль'), 'short')
    await userEvent.click(screen.getByRole('button', { name: 'Создать центр' }))
    expect(await screen.findByText('Пароль — минимум 8 символов')).toBeInTheDocument()
    expect(calls.some((r) => r.url.includes('/auth/register'))).toBe(false)
  })

  it('режим invite без ссылки — «по приглашению» и WhatsApp поддержки, формы нет', async () => {
    mockApi({ 'GET /api/v1/me': unauthorized, 'GET /api/v1/auth/registration': inviteReg })
    renderRoute('/register')
    expect(await screen.findByRole('heading', { name: 'Регистрация по приглашению' })).toBeInTheDocument()
    const wa = screen.getByRole('link', { name: 'Написать в WhatsApp' })
    expect(wa.getAttribute('href')).toMatch(/^https:\/\/wa\.me\/996555000100\?text=/)
    expect(screen.queryByLabelText('Название центра')).not.toBeInTheDocument()
  })

  it('по ссылке /register/:token — форма и токен в запросе', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': unauthorized,
      'GET /api/v1/auth/registration': inviteReg,
      'GET /api/v1/auth/registration/tok123': { body: { valid: true } },
      'POST /api/v1/auth/register': { status: 201, body: me },
    })
    const { router } = renderRoute('/register/tok123')
    await userEvent.type(await screen.findByLabelText('Название центра'), 'Билим')
    await userEvent.type(screen.getByLabelText('Ваше имя'), 'Айгуль')
    await userEvent.type(screen.getByLabelText('Телефон'), '0700123456')
    await userEvent.type(screen.getByLabelText('Пароль'), 'secret-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Создать центр' }))
    await waitFor(() => expect(router.state.location.pathname).not.toBe('/register/tok123'))
    const req = calls.find((r) => r.url.endsWith('/auth/register'))!
    expect(await bodyOf(req)).toMatchObject({ center_name: 'Билим', registration_token: 'tok123' })
  })

  it('недействительная ссылка — «Ссылка недействительна»', async () => {
    mockApi({
      'GET /api/v1/me': unauthorized,
      'GET /api/v1/auth/registration': inviteReg,
      'GET /api/v1/auth/registration/old': apiError(404, 'registration_invalid', 'Ссылка регистрации недействительна'),
    })
    renderRoute('/register/old')
    expect(await screen.findByRole('heading', { name: 'Ссылка недействительна' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Написать в WhatsApp' })).toBeInTheDocument()
  })

  it('режим approval — форма с пометкой и кнопкой «Отправить заявку»', async () => {
    mockApi({ 'GET /api/v1/me': unauthorized, 'GET /api/v1/auth/registration': { body: { mode: 'approval', support_whatsapp: null } } })
    renderRoute('/register')
    expect(await screen.findByText(/Заявку проверит администратор сервиса/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Отправить заявку' })).toBeInTheDocument()
  })
})

describe('статус центра', () => {
  it('заявка — экран «Заявка на рассмотрении», данных центра нет', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: { ...me, centers: [center('owner', 'c1', 'Билим', 'pending')] } },
      'GET /api/v1/auth/registration': openReg,
    })
    renderRoute('/c/c1/debtors')
    expect(await screen.findByRole('heading', { name: 'Заявка на рассмотрении' })).toBeInTheDocument()
    expect(screen.getByText(/Центр «Билим» создан/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Выйти' })).toBeInTheDocument()
    expect(calls.some((r) => r.url.includes('/c/c1/'))).toBe(false)
  })

  it('приостановлен — «Доступ приостановлен» и WhatsApp поддержки', async () => {
    mockApi({
      'GET /api/v1/me': { body: { ...me, centers: [center('owner', 'c1', 'Билим', 'blocked')] } },
      'GET /api/v1/auth/registration': openReg,
    })
    renderRoute('/c/c1/students')
    expect(await screen.findByRole('heading', { name: 'Доступ приостановлен' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('link', { name: 'Написать в WhatsApp' }).getAttribute('href')).toMatch(/^https:\/\/wa\.me\/996555000100\?/))
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
    mockApi({ 'GET /api/v1/me': { body: { ...me, centers: [center('teacher')] } } })
    const { router } = renderRoute('/')
    await waitFor(() => expect(router.state.location.pathname).toBe('/c/c1/today'))
    const navs = screen.getAllByRole('navigation', { name: 'Главное меню' })
    for (const nav of navs) {
      const links = Array.from(nav.querySelectorAll('a')).map((a) => a.textContent)
      expect(links).toEqual(['Сегодня', 'Группы'])
    }
  })

  it('преподаватель на чужом разделе — «нет доступа»', async () => {
    mockApi({ 'GET /api/v1/me': { body: { ...me, centers: [center('teacher')] } } })
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
