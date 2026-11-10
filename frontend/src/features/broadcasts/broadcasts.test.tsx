import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { center, group, list, meAdmin, meOwner, meTeacher } from '@/test/fixtures'
import { apiError, bodyOf, mockApi, renderRoute } from '@/test/utils'

const C = '/api/v1/c/c1'
const counts = { telegram_sent: 2, telegram_queued: 0, whatsapp_total: 2, whatsapp_opened: 0, no_contact: 1 }
const broadcast = { id: 'b1', created_at: '2026-10-19T05:00:00Z', author: 'Айгуль', audience: 'center', group_names: [], text: 'Завтра занятий нет', counts }
const detail = {
  ...broadcast,
  links: [
    { parent_id: 'p1', parent_name: 'Нургуль', students: ['Бекова Айжан'], text: '*Центр*', url: 'https://wa.me/996700333444?text=x', opened: false },
    { parent_id: 'p2', parent_name: 'Асель', students: ['Ким Элина'], text: '*Центр*', url: 'https://wa.me/996700333445?text=x', opened: true },
    { parent_id: 'p3', parent_name: 'Без Номера', students: ['Сыдыков'], text: '*Центр*', url: null, opened: false },
  ],
}
const preview = { telegram: 2, whatsapp: 2, no_contact: 1, telegram_text: '🏫 Центр «Билим»\n\nЗавтра занятий нет', whatsapp_text: '*🏫 Центр «Билим»*\n\nЗавтра занятий нет' }

const base = (me: object = meOwner) => ({
  'GET /api/v1/me': { body: me },
  [`GET ${C}/groups`]: { body: list([group]) },
  [`GET ${C}/broadcasts`]: { body: list([]) },
})

const fillAndPreview = async () => {
  await userEvent.type(await screen.findByLabelText('Текст объявления'), 'Завтра занятий нет')
  await userEvent.click(screen.getByRole('button', { name: 'Предпросмотр' }))
  return screen.findByRole('dialog', { name: 'Проверьте перед отправкой' })
}

describe('Рассылка', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('меню и доступ: владелец и админ видят, преподаватель — нет', async () => {
    mockApi(base(meAdmin))
    const admin = renderRoute('/c/c1/broadcasts')
    expect(await screen.findByRole('heading', { name: 'Рассылка' })).toBeInTheDocument()
    admin.unmount()
    mockApi(base(meTeacher))
    renderRoute('/c/c1/broadcasts')
    await waitFor(() => expect(screen.queryByLabelText('Текст объявления')).not.toBeInTheDocument())
    expect(screen.queryByRole('link', { name: 'Рассылка' })).not.toBeInTheDocument()
  })

  it('предпросмотр, отправка с одним ключом, итоги и отметка ссылки', async () => {
    const { calls } = mockApi({
      ...base(),
      [`POST ${C}/broadcasts/preview`]: { body: preview },
      [`POST ${C}/broadcasts`]: { status: 201, body: detail },
      [`GET ${C}/broadcasts/b1`]: { body: detail },
      [`POST ${C}/broadcasts/b1/recipients/p1/whatsapp-opened`]: { status: 204 },
    })
    const { router } = renderRoute('/c/c1/broadcasts')
    const dialog = await fillAndPreview()
    expect(within(dialog).getByText(/🏫 Центр «Билим»/)).toBeInTheDocument()
    const stats = within(dialog).getByTestId('broadcast-preview')
    expect(stats).toHaveTextContent('В Telegram — автоматически2')
    expect(stats).toHaveTextContent('Без контакта — не получат1')

    await userEvent.click(within(dialog).getByRole('button', { name: 'Отправить' }))
    const result = await screen.findByRole('dialog', { name: 'Итоги рассылки' })
    expect(router.state.location.search).toBe('?open=b1')
    const send = calls.find((r) => r.method === 'POST' && r.url.endsWith('/broadcasts'))!
    expect(send.headers.get('Idempotency-Key')).toBeTruthy()
    expect(await bodyOf(send)).toEqual({ audience: 'center', text: 'Завтра занятий нет' })

    const summary = await within(result).findByTestId('broadcast-stats')
    expect(summary).toHaveTextContent('Ушло в Telegram2')
    expect(summary).toHaveTextContent('Ссылок WhatsApp открыто1 из 2')
    expect(summary).toHaveTextContent('Без контакта1')
    expect(within(result).getByText('нет телефона')).toBeInTheDocument()

    const row = within(result).getByText('Нургуль').closest('li')!
    const link = within(row).getByRole('link', { name: 'WhatsApp' })
    link.addEventListener('click', (e) => e.preventDefault())
    await userEvent.click(link)
    expect(await within(row).findByText('открыто')).toBeInTheDocument()
    await waitFor(() => expect(summary).toHaveTextContent('Ссылок WhatsApp открыто2 из 2'))
    await waitFor(() => expect(calls.some((r) => r.url.endsWith('/recipients/p1/whatsapp-opened'))).toBe(true))
  })

  it('выбранные группы: без группы не отправить, с группой — group_ids', async () => {
    const { calls } = mockApi({ ...base(), [`POST ${C}/broadcasts/preview`]: { body: preview } })
    renderRoute('/c/c1/broadcasts')
    await userEvent.type(await screen.findByLabelText('Текст объявления'), 'Группе')
    await userEvent.click(screen.getByRole('radio', { name: /Выбранные группы/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Предпросмотр' }))
    expect(await screen.findByText('Выберите хотя бы одну группу')).toBeInTheDocument()
    expect(calls.some((r) => r.url.endsWith('/preview'))).toBe(false)
    await userEvent.click(await screen.findByRole('checkbox', { name: group.name }))
    await userEvent.click(screen.getByRole('button', { name: 'Предпросмотр' }))
    await screen.findByRole('dialog', { name: 'Проверьте перед отправкой' })
    expect(await bodyOf(calls.find((r) => r.url.endsWith('/preview'))!)).toEqual({ audience: 'groups', group_ids: [group.id], text: 'Группе' })
  })

  it('чаще раза в 5 минут — понятное сообщение', async () => {
    mockApi({
      ...base(),
      [`POST ${C}/broadcasts/preview`]: { body: preview },
      [`POST ${C}/broadcasts`]: apiError(429, 'broadcast_too_soon', 'x'),
    })
    renderRoute('/c/c1/broadcasts')
    const dialog = await fillAndPreview()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отправить' }))
    expect(await screen.findByText('Рассылка уже была меньше 5 минут назад. Подождите и отправьте снова.')).toBeInTheDocument()
  })

  it('демо-центр: предпросмотр есть, отправка отключена с объяснением', async () => {
    const demo = { ...meOwner, centers: [{ ...center('owner'), is_demo: true }] }
    mockApi({ ...base(demo), [`POST ${C}/broadcasts/preview`]: { body: preview } })
    renderRoute('/c/c1/broadcasts')
    const dialog = await fillAndPreview()
    expect(within(dialog).getByRole('button', { name: 'Отправить' })).toBeDisabled()
    expect(within(dialog).getByText(/отправка отключена/)).toBeInTheDocument()
  })

  it('история: дата, автор, кому, текст, итоги; «Открыть» показывает список', async () => {
    mockApi({
      ...base(),
      [`GET ${C}/broadcasts`]: { body: list([broadcast, { ...broadcast, id: 'b2', audience: 'groups', group_names: ['Английский A1'] }]) },
      [`GET ${C}/broadcasts/b1`]: { body: detail },
    })
    renderRoute('/c/c1/broadcasts')
    const history = await screen.findByRole('list', { name: 'История рассылок' })
    const rows = within(history).getAllByRole('listitem')
    expect(within(rows[0]!).getByText('Весь центр')).toBeInTheDocument()
    expect(within(rows[0]!).getByText(/Айгуль/)).toBeInTheDocument()
    expect(within(rows[0]!).getByText('Завтра занятий нет')).toBeInTheDocument()
    expect(within(rows[0]!).getByText('Telegram: 2 · WhatsApp открыто: 0 из 2 · без контакта: 1')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('Английский A1')).toBeInTheDocument()
    await userEvent.click(within(rows[0]!).getByRole('button', { name: /Открыть/ }))
    expect(await screen.findByRole('dialog', { name: 'Итоги рассылки' })).toBeInTheDocument()
  })
})
