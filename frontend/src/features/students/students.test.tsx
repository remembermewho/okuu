import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { group, list, meOwner, meTeacher, operation, student, tariffMonth } from '@/test/fixtures'
import { apiError, bodyOf, mockApi, renderRoute } from '@/test/utils'

const C = '/api/v1/c/c1'

afterEach(() => {
  vi.unstubAllGlobals()
})

function cardApi(extra: Parameters<typeof mockApi>[0] = {}) {
  return mockApi({
    'GET /api/v1/me': { body: meOwner },
    [`GET ${C}/students/s1`]: { body: student() },
    [`GET ${C}/students/s1/ledger`]: { body: list([operation()]) },
    [`GET ${C}/students/s1/attendance`]: { body: list([]) },
    ...extra,
  })
}

describe('карточка ученика', () => {
  it('показывает долг, родителя и историю', async () => {
    cardApi()
    renderRoute('/c/c1/students/s1')
    expect(await screen.findByRole('heading', { name: /Асанов Бекзат/ })).toBeInTheDocument()
    expect(screen.getByText('Долг')).toBeInTheDocument()
    expect(screen.getAllByText(/3\s000\sс/).length).toBeGreaterThan(0)
    expect(screen.getByText('Гульнара')).toBeInTheDocument()
    expect(await screen.findByText('Начисление')).toBeInTheDocument()
  })

  it('оплата: один Idempotency-Key на открытие формы, повтор после ошибки — с тем же ключом', async () => {
    let attempt = 0
    const { calls } = cardApi({
      [`POST ${C}/students/s1/payments`]: () => (++attempt === 1 ? apiError(500, 'internal', 'Ошибка') : { status: 201, body: operation({ id: 'l2', kind: 'payment', amount: 300_000, method: 'cash' }) }),
    })
    renderRoute('/c/c1/students/s1')
    await userEvent.click(await screen.findByRole('button', { name: 'Отметить оплату' }))
    const dialog = await screen.findByRole('dialog')
    // Сумма подставлена из долга, в сомах.
    expect(within(dialog).getByLabelText('Сумма')).toHaveValue('3000')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отметить оплату' }))
    expect(await within(dialog).findByRole('alert')).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отметить оплату' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    const posts = calls.filter((r) => r.method === 'POST' && r.url.endsWith('/payments'))
    expect(posts).toHaveLength(2)
    const k1 = posts[0]!.headers.get('Idempotency-Key')
    expect(k1).toBeTruthy()
    expect(posts[1]!.headers.get('Idempotency-Key')).toBe(k1)
    expect(await bodyOf(posts[1]!)).toEqual({ kind: 'payment', amount: 300_000, method: 'cash', comment: null })

    // Новое открытие формы — новый ключ.
    await userEvent.click(screen.getByRole('button', { name: 'Отметить оплату' }))
    const d2 = await screen.findByRole('dialog')
    await userEvent.click(within(d2).getByRole('button', { name: 'Отметить оплату' }))
    await waitFor(() => expect(calls.filter((r) => r.method === 'POST' && r.url.endsWith('/payments'))).toHaveLength(3))
    const third = calls.filter((r) => r.method === 'POST' && r.url.endsWith('/payments'))[2]!
    expect(third.headers.get('Idempotency-Key')).not.toBe(k1)
  })

  it('поправка: «добавить к долгу» — отрицательная сумма, причина обязательна', async () => {
    const { calls } = cardApi({ [`POST ${C}/students/s1/payments`]: { status: 201, body: operation({ kind: 'correction' }) } })
    renderRoute('/c/c1/students/s1')
    await userEvent.click(await screen.findByRole('button', { name: 'Ещё действия' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Поправка' }))
    const dialog = await screen.findByRole('dialog')
    await userEvent.click(within(dialog).getByLabelText('Добавить к долгу'))
    await userEvent.type(within(dialog).getByLabelText('Сумма'), '150,50')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Сохранить поправку' }))
    expect(await within(dialog).findByText('Укажите причину поправки')).toBeInTheDocument()
    await userEvent.type(within(dialog).getByLabelText('Причина поправки'), 'Ошибка в начислении')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Сохранить поправку' }))
    await waitFor(() => expect(calls.some((r) => r.url.endsWith('/payments'))).toBe(true))
    const post = calls.find((r) => r.url.endsWith('/payments'))!
    expect(await bodyOf(post)).toEqual({ kind: 'correction', amount: -15_050, comment: 'Ошибка в начислении' })
  })

  it('отмена операции требует причину', async () => {
    const { calls } = cardApi({ [`POST ${C}/ledger/l1/cancel`]: { body: operation({ cancelled: true, cancel_reason: 'Ошибка' }) } })
    renderRoute('/c/c1/students/s1')
    await userEvent.click(await screen.findByRole('button', { name: /^Отменить: Начисление/ }))
    const dialog = await screen.findByRole('dialog')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отменить' }))
    expect(await within(dialog).findByText('Причина — от 3 символов')).toBeInTheDocument()
    expect(calls.some((r) => r.url.endsWith('/cancel'))).toBe(false)
    await userEvent.type(within(dialog).getByLabelText('Причина отмены'), 'Двойное начисление')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отменить' }))
    await waitFor(() => expect(calls.some((r) => r.url.endsWith('/cancel'))).toBe(true))
    expect(await bodyOf(calls.find((r) => r.url.endsWith('/cancel'))!)).toEqual({ reason: 'Двойное начисление' })
  })

  it('оплату Finik может отменить только владелец', async () => {
    mockApi({
      'GET /api/v1/me': { body: { ...meOwner, centers: [{ id: 'c1', name: 'Билим', role: 'admin' }] } },
      [`GET ${C}/students/s1`]: { body: student() },
      [`GET ${C}/students/s1/ledger`]: { body: list([operation({ id: 'l9', kind: 'payment', amount: 100_000, method: 'finik' }), operation()]) },
      [`GET ${C}/students/s1/attendance`]: { body: list([]) },
    })
    renderRoute('/c/c1/students/s1')
    expect(await screen.findByText(/Оплата · Finik/)).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /^Отменить:/ })).toHaveLength(1)
  })

  it('пригласить родителя — ссылка на бота и WhatsApp', async () => {
    cardApi({
      [`POST ${C}/parents/p1/invite`]: {
        status: 201,
        body: {
          id: 'i1',
          bot_url: 'https://t.me/okuu_bot?start=abc',
          expires_at: '2026-10-27T10:00:00Z',
          texts: { ru: 'Здравствуйте! Подключитесь', ky: 'Саламатсызбы!' },
          lang: 'ru',
          whatsapp_url: 'https://wa.me/996700111222?text=x',
          tg_status: 'invited',
        },
      },
    })
    renderRoute('/c/c1/students/s1')
    await userEvent.click(await screen.findByRole('button', { name: 'Отправить новую ссылку' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText('Ссылка на бота')).toHaveValue('https://t.me/okuu_bot?start=abc')
    expect(within(dialog).getByRole('link', { name: 'Отправить в WhatsApp' })).toHaveAttribute('href', 'https://wa.me/996700111222?text=x')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Кыргызча' }))
    expect(within(dialog).getByLabelText('Текст сообщения')).toHaveValue('Саламатсызбы!')
  })

  it('напомнить о долге — причина, если не отправлено', async () => {
    cardApi({ [`POST ${C}/reminders`]: { body: { sent: 0, messages: 0, skipped: [{ student_id: 's1', reason: 'rate_limited' }] } } })
    renderRoute('/c/c1/students/s1')
    await userEvent.click(await screen.findByRole('button', { name: 'Напомнить о долге' }))
    expect(await screen.findByText('Не отправлено: уже напоминали меньше часа назад')).toBeInTheDocument()
  })

  it('учителю карточка недоступна', async () => {
    mockApi({ 'GET /api/v1/me': { body: meTeacher } })
    renderRoute('/c/c1/students/s1')
    expect(await screen.findByText(/Нет доступа/)).toBeInTheDocument()
  })
})

describe('добавление ученика', () => {
  it('создаёт ученика и записывает в группу с выбором «за текущий месяц»', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/students`]: { body: list([]) },
      [`GET ${C}/groups`]: { body: list([group]) },
      [`GET ${C}/tariffs`]: { body: list([tariffMonth]) },
      [`POST ${C}/students`]: { status: 201, body: { ...student({ enrollments: [], balance: 0 }), parent_existing: false } },
      [`POST ${C}/students/s1/enrollments`]: { status: 201, body: student().enrollments[0] },
      [`GET ${C}/students/s1`]: { body: student() },
      [`GET ${C}/students/s1/ledger`]: { body: list([]) },
      [`GET ${C}/students/s1/attendance`]: { body: list([]) },
    })
    const { router } = renderRoute('/c/c1/students')
    expect(await screen.findByText('Пока нет учеников')).toBeInTheDocument()
    await userEvent.click(screen.getAllByRole('button', { name: 'Добавить ученика' })[0]!)
    const dialog = await screen.findByRole('dialog')
    await userEvent.type(within(dialog).getByLabelText('ФИО ученика'), 'Асанов Бекзат')
    await userEvent.type(within(dialog).getByLabelText('Имя родителя'), 'Гульнара')
    await userEvent.type(within(dialog).getByLabelText('Телефон родителя'), '0700111222')
    // «Записать в группу» включено по умолчанию.
    expect(within(dialog).getByLabelText(/Записать в группу/)).toBeChecked()
    await userEvent.selectOptions(await within(dialog).findByLabelText('Группа'), 'g1')
    await userEvent.click(within(dialog).getByLabelText('Не начислять'))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Добавить ученика' }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/c/c1/students/s1'))
    const create = calls.find((r) => r.method === 'POST' && r.url.endsWith('/students'))!
    expect(await bodyOf(create)).toMatchObject({ full_name: 'Асанов Бекзат', parent: { name: 'Гульнара', phone: '+996700111222' } })
    const enroll = calls.find((r) => r.url.endsWith('/enrollments'))!
    expect(await bodyOf(enroll)).toMatchObject({ group_id: 'g1', first_month: 'none' })
  })

  it('ошибка поля с сервера — под полем', async () => {
    mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/students`]: { body: list([]) },
      [`GET ${C}/groups`]: { body: list([group]) },
      [`GET ${C}/tariffs`]: { body: list([tariffMonth]) },
      [`POST ${C}/students`]: apiError(400, 'validation', 'Слишком длинное имя', 'full_name'),
    })
    renderRoute('/c/c1/students')
    await userEvent.click((await screen.findAllByRole('button', { name: 'Добавить ученика' }))[0]!)
    const dialog = await screen.findByRole('dialog')
    await userEvent.type(within(dialog).getByLabelText('ФИО ученика'), 'Асанов Бекзат')
    await userEvent.type(within(dialog).getByLabelText('Имя родителя'), 'Гульнара')
    await userEvent.type(within(dialog).getByLabelText('Телефон родителя'), '0700111222')
    await userEvent.click(within(dialog).getByLabelText(/Записать в группу/))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Добавить ученика' }))
    expect(await within(dialog).findByText('Слишком длинное имя')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('ФИО ученика')).toHaveAttribute('aria-invalid', 'true')
  })
})
