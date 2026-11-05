// Правки по ручному прогону 02.11: подсказка суммы со скидкой, найденный родитель, «Ушёл» с отменой будущих
// начислений, «В центре с», точное число учеников.
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { group, list, meOwner, operation, student, tariffMonth, tariffPackage } from '@/test/fixtures'
import { bodyOf, mockApi, renderRoute } from '@/test/utils'
import { discountBody } from './AddStudentDialog'
import { discountFits, discountedPrice, priceWithDiscount } from './enroll'

const C = '/api/v1/c/c1'

afterEach(() => {
  vi.unstubAllGlobals()
})

const addApi = (extra: Record<string, unknown> = {}) =>
  mockApi({
    'GET /api/v1/me': { body: meOwner },
    [`GET ${C}/students`]: { body: { ...list([]), total: 0 } },
    [`GET ${C}/groups`]: { body: list([group, { ...group, id: 'g2', name: 'Робототехника', default_tariff_id: 't2' }]) },
    [`GET ${C}/tariffs`]: { body: list([tariffMonth, tariffPackage]) },
    ...(extra as Record<string, never>),
  })

async function openAdd() {
  renderRoute('/c/c1/students')
  await userEvent.click((await screen.findAllByRole('button', { name: 'Добавить ученика' }))[0]!)
  return screen.findByRole('dialog')
}

describe('сумма со скидкой', () => {
  it('считается как на бэкенде', () => {
    expect(discountedPrice(350_000, { kind: 'percent', value: 10 })).toBe(315_000)
    expect(discountedPrice(400_000, { kind: 'fixed', value: 50_000 })).toBe(350_000)
    expect(discountedPrice(300_000, { kind: 'fixed', value: 500_000 })).toBe(0)
    expect(discountedPrice(333_333, { kind: 'percent', value: 10 })).toBe(300_000) // скидка 33 333,3 → вниз
    expect(priceWithDiscount(350_000, null)).toMatch(/^3\s500\sс$/)
    expect(priceWithDiscount(350_000, { kind: 'percent', value: 10 })).toMatch(/^3\s150\sс \(скидка 10%\)$/)
  })

  it('форма: «Начислить полностью — 3 150 с (скидка 10%)», абонемент — «3 500 с (скидка 500 с)»', async () => {
    addApi()
    const dialog = await openAdd()
    await userEvent.selectOptions(within(dialog).getByLabelText('Скидка'), 'percent')
    await userEvent.type(within(dialog).getByLabelText('Размер скидки'), '10')
    await userEvent.selectOptions(await within(dialog).findByLabelText('Группа'), 'g1')
    expect(within(dialog).getByText(/2\s700\sс \(скидка 10%\)/)).toBeInTheDocument() // 3 000 − 10%

    await userEvent.selectOptions(within(dialog).getByLabelText('Скидка'), 'fixed')
    await userEvent.clear(within(dialog).getByLabelText('Размер скидки'))
    await userEvent.type(within(dialog).getByLabelText('Размер скидки'), '500')
    await userEvent.selectOptions(within(dialog).getByLabelText('Группа'), 'g2')
    expect(within(dialog).getByText(/Абонемент на 8 занятий: начислится 1\s900\sс \(скидка 500\sс\)/)).toBeInTheDocument()
  })

  it('невалидная скидка (> 100%, > цены, не число) — суммы нет', () => {
    expect(discountFits(300_000, null)).toBe(true)
    expect(discountFits(300_000, { kind: 'percent', value: 100 })).toBe(true)
    expect(discountFits(300_000, { kind: 'percent', value: 0 })).toBe(true)
    expect(discountFits(300_000, { kind: 'percent', value: 150 })).toBe(false)
    expect(discountFits(300_000, { kind: 'percent', value: 10.5 })).toBe(false)
    expect(discountFits(300_000, { kind: 'fixed', value: 300_000 })).toBe(true)
    expect(discountFits(300_000, { kind: 'fixed', value: 500_000 })).toBe(false)
    expect(discountFits(300_000, { kind: 'fixed', value: NaN })).toBe(false)
    expect(discountBody('percent', 'abc')?.value).toBeNaN()
    expect(discountBody('fixed', '5 000')).toEqual({ kind: 'fixed', value: 500_000 })
  })

  it('форма: процент 150 и фиксированная больше цены — без подсказки суммы, только ошибка под полем', async () => {
    const { calls } = addApi()
    const dialog = await openAdd()
    await userEvent.type(within(dialog).getByLabelText('ФИО ученика'), 'Элина Ким')
    await userEvent.type(within(dialog).getByLabelText('Имя родителя'), 'Асель')
    await userEvent.type(within(dialog).getByLabelText('Телефон родителя'), '0700 333 102')
    await userEvent.selectOptions(within(dialog).getByLabelText('Скидка'), 'percent')
    await userEvent.type(within(dialog).getByLabelText('Размер скидки'), '150')
    await userEvent.selectOptions(await within(dialog).findByLabelText('Группа'), 'g1')
    const full = within(dialog).getByRole('radio', { name: /Начислить полностью/ })
    expect(full).toHaveAccessibleName('Начислить полностью') // без «0 с (скидка 100%)»
    expect(within(dialog).queryByText(/скидка 100%/)).not.toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Добавить ученика' }))
    expect(await within(dialog).findByText('От 0 до 100')).toBeInTheDocument()
    expect(within(dialog).queryByText(/\(скидка/)).not.toBeInTheDocument()

    // Фиксированная 5 000 при абонементе 2 400 — «Абонемент на 8 занятий» без суммы, ошибка про цену.
    await userEvent.selectOptions(within(dialog).getByLabelText('Скидка'), 'fixed')
    await userEvent.clear(within(dialog).getByLabelText('Размер скидки'))
    await userEvent.type(within(dialog).getByLabelText('Размер скидки'), '5000')
    await userEvent.selectOptions(within(dialog).getByLabelText('Группа'), 'g2')
    expect(await within(dialog).findByText(/Не больше цены тарифа/)).toBeInTheDocument()
    expect(within(dialog).getByText('Абонемент на 8 занятий')).toBeInTheDocument()
    expect(within(dialog).queryByText(/начислится/)).not.toBeInTheDocument()

    // Исправили скидку — сумма вернулась.
    await userEvent.clear(within(dialog).getByLabelText('Размер скидки'))
    await userEvent.type(within(dialog).getByLabelText('Размер скидки'), '500')
    expect(await within(dialog).findByText(/начислится 1\s900\sс \(скидка 500\sс\)/)).toBeInTheDocument()
    expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/students'))).toBe(false)
  })
})

describe('существующий родитель', () => {
  it('найден по телефону: подсказка, имя подставлено и не обязательно', async () => {
    const { calls } = addApi({
      [`GET ${C}/parents/lookup`]: { body: { parent: { id: 'p1', name: 'Гульнара', children: ['Айдана Токтосунова'] } } },
      [`POST ${C}/students`]: { status: 201, body: { ...student({ enrollments: [], balance: 0 }), parent_existing: true } },
      [`GET ${C}/students/s1`]: { body: student() },
      [`GET ${C}/students/s1/ledger`]: { body: list([]) },
      [`GET ${C}/students/s1/attendance`]: { body: list([]) },
    })
    const dialog = await openAdd()
    expect(within(dialog).queryByText(/Родитель с этим телефоном уже есть/)).not.toBeInTheDocument()
    await userEvent.type(within(dialog).getByLabelText('ФИО ученика'), 'София Токтосунова')
    await userEvent.type(within(dialog).getByLabelText('Телефон родителя'), '0700 111 222')
    await userEvent.tab()
    expect(await within(dialog).findByText('Родитель найден: Гульнара (дети: Айдана Токтосунова)')).toBeInTheDocument()
    const lookup = calls.find((r) => r.url.includes('/parents/lookup'))!
    expect(new URL(lookup.url).searchParams.get('phone')).toBe('+996700111222')
    expect(within(dialog).getByLabelText(/Имя родителя/)).toHaveValue('Гульнара')

    // Имя можно стереть — уйдёт имя найденного родителя.
    await userEvent.clear(within(dialog).getByLabelText(/Имя родителя/))
    await userEvent.click(within(dialog).getByLabelText(/Записать в группу/))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Добавить ученика' }))
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/students'))).toBe(true))
    const create = calls.find((r) => r.method === 'POST' && r.url.endsWith('/students'))!
    expect(await bodyOf(create)).toMatchObject({ parent: { name: 'Гульнара', phone: '+996700111222' } })
  })

  it('не найден — подсказки нет, имя обязательно', async () => {
    addApi({ [`GET ${C}/parents/lookup`]: { body: { parent: null } } })
    const dialog = await openAdd()
    await userEvent.type(within(dialog).getByLabelText('ФИО ученика'), 'София Токтосунова')
    await userEvent.type(within(dialog).getByLabelText('Телефон родителя'), '0700 999 222')
    await userEvent.tab()
    await new Promise((r) => setTimeout(r, 400))
    expect(within(dialog).queryByText(/Родитель найден/)).not.toBeInTheDocument()
    await userEvent.click(within(dialog).getByLabelText(/Записать в группу/))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Добавить ученика' }))
    expect(await within(dialog).findByText('Заполните поле')).toBeInTheDocument()
  })
})

describe('карточка ученика', () => {
  const cardApi = (st: ReturnType<typeof student>, ledger: unknown[] = []) =>
    mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/students/s1`]: { body: st },
      [`GET ${C}/students/s1/ledger`]: { body: list(ledger) },
      [`GET ${C}/students/s1/attendance`]: { body: list([]) },
      [`PATCH ${C}/students/s1`]: { body: { ...st, status: 'left' } },
    })

  it('«В центре с» — начало самой ранней записи, а не дата карточки', async () => {
    const st = student({ created_at: '2026-11-02T10:00:00Z' })
    st.enrollments = [
      { ...st.enrollments[0]!, id: 'e1', start_date: '2026-10-01' },
      { ...st.enrollments[0]!, id: 'e0', start_date: '2025-09-01', active: false, end_date: '2026-05-31' },
    ]
    cardApi(st)
    renderRoute('/c/c1/students/s1')
    expect(await screen.findByText(/В центре с 01\.09\.2025/)).toBeInTheDocument()
  })

  it('«Ушёл»: галочка отмены начислений за месяцы после последнего дня — включена, уходит в запрос', async () => {
    const st = student()
    const dec = operation({ id: 'l2', kind: 'charge', amount: -350_000, period: '2026-12-01', due_date: '2026-12-01' })
    const oct = operation({ id: 'l1', kind: 'charge', amount: -350_000, period: '2026-10-01', due_date: '2026-10-01' })
    const { calls } = cardApi(st, [dec, oct])
    renderRoute('/c/c1/students/s1')
    await userEvent.click(await screen.findByRole('button', { name: 'Ещё действия' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Ученик ушёл' }))
    const dialog = await screen.findByRole('dialog')
    const box = await within(dialog).findByLabelText(/Отменить начисления за месяцы после последнего дня \(декабрь 2026 — 3\s500\sс\)/)
    expect(box).toBeChecked()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Ученик ушёл' }))
    await waitFor(() => expect(calls.some((r) => r.method === 'PATCH')).toBe(true))
    expect(await bodyOf(calls.find((r) => r.method === 'PATCH')!)).toMatchObject({ status: 'left', cancel_future_charges: true })
  })

  it('«Ушёл» без будущих начислений — галочки нет', async () => {
    const { calls } = cardApi(student(), [operation({ id: 'l1', kind: 'charge', amount: -350_000, period: '2026-10-01' })])
    renderRoute('/c/c1/students/s1')
    await userEvent.click(await screen.findByRole('button', { name: 'Ещё действия' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Ученик ушёл' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).queryByLabelText(/Отменить начисления/)).not.toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Ученик ушёл' }))
    await waitFor(() => expect(calls.some((r) => r.method === 'PATCH')).toBe(true))
    expect(await bodyOf(calls.find((r) => r.method === 'PATCH')!)).not.toHaveProperty('cancel_future_charges')
  })
})

describe('список учеников', () => {
  it('точное число из total, без «30+»', async () => {
    mockApi({
      'GET /api/v1/me': { body: meOwner },
      [`GET ${C}/students`]: { body: { items: [{ ...student(), groups: [] }], next_cursor: 'x', total: 38 } },
      [`GET ${C}/groups`]: { body: list([group]) },
    })
    renderRoute('/c/c1/students')
    expect(await screen.findByText('38 учеников')).toBeInTheDocument()
    expect(screen.queryByText(/\+ учени/)).not.toBeInTheDocument()
  })
})
