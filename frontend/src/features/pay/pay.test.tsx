import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiError, mockApi, renderRoute, unauthorized } from '@/test/utils'
import { redirectTo } from '@/utils/redirect'

vi.mock('@/utils/redirect', () => ({ redirectTo: vi.fn() }))

const link = {
  center_name: 'Билим',
  student_name: 'Асанов Бекзат',
  amount: 300_000,
  provider: 'manual',
  pay_details: 'Mbank: 0700 123 456 (Айгуль А.)',
  status: 'pending',
  expires_at: '2026-10-21T10:00:00Z',
}

// Страница онлайн-оплаты работает только с ONLINE_PAYMENTS_ENABLED (GET /config).
const online = { 'GET /api/v1/config': { body: { online_payments: true } } }

afterEach(() => {
  vi.unstubAllGlobals()
  vi.mocked(redirectTo).mockReset()
})

describe('страница оплаты', () => {
  it('онлайн-оплата выключена — «не подключена», ссылку даже не запрашиваем', async () => {
    const { calls } = mockApi({ 'GET /api/v1/config': { body: { online_payments: false } } })
    renderRoute('/pay/tok')
    expect(await screen.findByRole('heading', { name: 'Онлайн-оплата не подключена' })).toBeInTheDocument()
    expect(screen.getByText('Онлайн-оплата не подключена, уточните способ оплаты у администратора центра.')).toBeInTheDocument()
    expect(calls.some((r) => r.url.includes('/pay/'))).toBe(false)
  })

  it('реквизиты: сумма, ученик, реквизиты и «Скопировать»; без меню и без входа', async () => {
    const { calls } = mockApi({ ...online, 'GET /api/v1/pay/tok': { body: link } })
    renderRoute('/pay/tok')
    expect(await screen.findByText('Асанов Бекзат')).toBeInTheDocument()
    expect(screen.getByText(/3\s000\sс/)).toBeInTheDocument()
    expect(screen.getByText('Mbank: 0700 123 456 (Айгуль А.)')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Скопировать реквизиты' })).toBeInTheDocument()
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
    expect(calls.some((r) => r.url.includes('/me'))).toBe(false)
  })

  it('онлайн: «Оплатить онлайн» — переход на страницу банка', async () => {
    const { calls } = mockApi({ ...online,
      'GET /api/v1/pay/tok': { body: { ...link, provider: 'finik', pay_details: undefined } },
      'POST /api/v1/pay/tok/start': { body: { provider: 'finik', amount: 300_000, pay_url: 'https://bank.example/pay/1' } },
    })
    renderRoute('/pay/tok')
    await userEvent.click(await screen.findByRole('button', { name: 'Оплатить онлайн' }))
    await waitFor(() => expect(redirectTo).toHaveBeenCalledWith('https://bank.example/pay/1'))
    expect(calls.filter((r) => r.method === 'POST')).toHaveLength(1)
  })

  it('онлайн-оплата недоступна — понятное сообщение', async () => {
    mockApi({ ...online,
      'GET /api/v1/pay/tok': { body: { ...link, provider: 'finik' } },
      'POST /api/v1/pay/tok/start': apiError(502, 'provider_unavailable', 'down'),
    })
    renderRoute('/pay/tok')
    await userEvent.click(await screen.findByRole('button', { name: 'Оплатить онлайн' }))
    expect(await screen.findByText(/Онлайн-оплата сейчас недоступна/)).toBeInTheDocument()
  })

  it('оплачено', async () => {
    mockApi({ ...online, 'GET /api/v1/pay/tok': { body: { ...link, status: 'paid' } } })
    renderRoute('/pay/tok')
    expect(await screen.findByRole('heading', { name: 'Оплата получена' })).toBeInTheDocument()
  })

  it('404 — ссылка устарела, уточнить способ оплаты у администратора', async () => {
    mockApi({ ...online, 'GET /api/v1/pay/old': apiError(404, 'not_found', 'Не найдено'), 'GET /api/v1/me': unauthorized })
    const { router } = renderRoute('/pay/old')
    expect(await screen.findByRole('heading', { name: 'Ссылка устарела' })).toBeInTheDocument()
    expect(screen.getByText(/Уточните способ оплаты у администратора/)).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/pay/old')
  })
})
