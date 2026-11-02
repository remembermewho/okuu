import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { list, meAdmin, tariffMonth, tariffPackage } from '@/test/fixtures'
import { apiError, bodyOf, mockApi, renderRoute } from '@/test/utils'

const C = '/api/v1/c/c1'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('тарифы', () => {
  it('список: тип, занятия, цена', async () => {
    mockApi({ 'GET /api/v1/me': { body: meAdmin }, [`GET ${C}/tariffs`]: { body: list([tariffMonth, tariffPackage]) } })
    renderRoute('/c/c1/tariffs')
    expect(await screen.findByText('Абонемент · за 8 занятий')).toBeInTheDocument()
    expect(screen.getByText('В месяц')).toBeInTheDocument()
    expect(screen.queryByText(/в месяц · в месяц/i)).not.toBeInTheDocument()
    expect(screen.getByText(/2\s400\sс/)).toBeInTheDocument()
  })

  it('абонемент: число занятий обязательно, цена уходит в тыйынах', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meAdmin },
      [`GET ${C}/tariffs`]: { body: list([]) },
      [`POST ${C}/tariffs`]: { status: 201, body: tariffPackage },
    })
    renderRoute('/c/c1/tariffs')
    await userEvent.click((await screen.findAllByRole('button', { name: 'Создать тариф' }))[0]!)
    const dialog = await screen.findByRole('dialog', { name: 'Новый тариф' })
    await userEvent.type(within(dialog).getByLabelText('Название'), '8 занятий')
    await userEvent.click(within(dialog).getByLabelText(/Абонемент/))
    await userEvent.type(within(dialog).getByLabelText('Цена'), '2 400,50')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Создать' }))
    expect(await within(dialog).findByText('От 1 до 1000')).toBeInTheDocument()
    await userEvent.type(within(dialog).getByLabelText('Занятий в абонементе'), '8')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Создать' }))
    await waitFor(() => expect(calls.some((r) => r.method === 'POST')).toBe(true))
    expect(await bodyOf(calls.find((r) => r.method === 'POST')!)).toEqual({ name: '8 занятий', kind: 'package', price: 240_050, lessons_count: 8 })
  })

  it('изменение: tariff_in_use — сообщение в форме', async () => {
    mockApi({
      'GET /api/v1/me': { body: meAdmin },
      [`GET ${C}/tariffs`]: { body: list([tariffMonth]) },
      [`PATCH ${C}/tariffs/t1`]: apiError(409, 'tariff_in_use', 'in use'),
    })
    renderRoute('/c/c1/tariffs')
    await userEvent.click(await screen.findByRole('button', { name: 'Изменить: Месяц' }))
    const dialog = await screen.findByRole('dialog', { name: 'Тариф' })
    expect(within(dialog).getByLabelText('Цена')).toHaveValue('3000')
    await userEvent.click(within(dialog).getByLabelText(/Абонемент/))
    await userEvent.type(within(dialog).getByLabelText('Занятий в абонементе'), '8')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Сохранить' }))
    expect(await within(dialog).findByText(/тип тарифа менять нельзя/)).toBeInTheDocument()
  })
})
