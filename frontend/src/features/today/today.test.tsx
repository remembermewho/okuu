import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { group, lesson, list, meAdmin, meTeacher } from '@/test/fixtures'
import { apiError, bodyOf, mockApi, renderRoute } from '@/test/utils'
import { addDays, todayISO } from '@/utils/date'
import { dateWindow } from './TodayPage'

const C = '/api/v1/c/c1'
const today = todayISO()

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('окно дат', () => {
  it('преподаватель — 7 дней назад и не дальше сегодня; админ — до +30 без ограничения назад', () => {
    expect(dateWindow(true, '2026-10-19')).toEqual({ min: '2026-10-12', max: '2026-10-19' })
    expect(dateWindow(false, '2026-10-19')).toEqual({ max: '2026-11-18' })
  })
})

describe('Сегодня', () => {
  it('отметки: в запрос уходят только изменения; повторное нажатие снимает отметку', async () => {
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meTeacher },
      [`GET ${C}/lessons`]: { body: list([lesson({ date: today })]) },
      [`PUT ${C}/lessons/les1/attendance`]: async (req) => {
        const b = (await req.clone().json()) as { marks: { student_id: string; status: string | null }[] }
        const l = lesson({ date: today })
        return { body: { ...l, students: l.students.map((st) => ({ ...st, status: b.marks.find((m) => m.student_id === st.student_id)?.status ?? st.status })) } }
      },
    })
    renderRoute('/c/c1/today')
    expect(await screen.findByText('Отмечено 1 из 2')).toBeInTheDocument()
    const save = screen.getByRole('button', { name: 'Сохранить' })
    expect(save).toBeDisabled()

    const s1 = screen.getByRole('group', { name: 'Асанов Бекзат' })
    await userEvent.click(within(s1).getByRole('button', { name: 'Не был' }))
    expect(within(s1).getByRole('button', { name: 'Не был' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText('Отмечено 2 из 2')).toBeInTheDocument()
    // Вернули как было — изменений нет.
    const s2 = screen.getByRole('group', { name: 'Бекова Айжан' })
    await userEvent.click(within(s2).getByRole('button', { name: 'Уважит.' }))
    await userEvent.click(within(s2).getByRole('button', { name: 'Был' }))
    expect(screen.getByText('1 изменение не сохранено')).toBeInTheDocument()

    await userEvent.click(save)
    expect(await screen.findByText('Отметки сохранены')).toBeInTheDocument()
    const put = calls.find((r) => r.method === 'PUT')!
    expect(await bodyOf(put)).toEqual({ marks: [{ student_id: 's1', status: 'absent' }] })
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeDisabled()
  })

  it('«Все были» отмечает только неотмеченных; учителю не показываем долг и действия админа', async () => {
    mockApi({ 'GET /api/v1/me': { body: meTeacher }, [`GET ${C}/lessons`]: { body: list([lesson({ date: today })]) } })
    renderRoute('/c/c1/today')
    await userEvent.click(await screen.findByRole('button', { name: 'Все были' }))
    expect(within(screen.getByRole('group', { name: 'Асанов Бекзат' })).getByRole('button', { name: 'Был' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText('1 изменение не сохранено')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Внеплановое занятие' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Действия с занятием/ })).not.toBeInTheDocument()
    expect(screen.getByText('Отмечать можно за последние 7 дней')).toBeInTheDocument()
  })

  it('преподаватель: вперёд нельзя, назад — до 7 дней; дата вне окна сводится к краю', async () => {
    const { calls } = mockApi({ 'GET /api/v1/me': { body: meTeacher }, [`GET ${C}/lessons`]: { body: list([]) } })
    renderRoute(`/c/c1/today?date=${addDays(today, -30)}`)
    expect(await screen.findByText('Занятий нет')).toBeInTheDocument()
    expect(calls.some((r) => r.url.includes(`date=${addDays(today, -7)}`))).toBe(true)
    expect(screen.getByRole('button', { name: 'Предыдущий день' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Следующий день' })).toBeEnabled()
  })

  it('несохранённые отметки — подтверждение при смене дня', async () => {
    const { calls } = mockApi({ 'GET /api/v1/me': { body: meTeacher }, [`GET ${C}/lessons`]: { body: list([lesson({ date: today })]) } })
    renderRoute('/c/c1/today')
    const s1 = await screen.findByRole('group', { name: 'Асанов Бекзат' })
    await userEvent.click(within(s1).getByRole('button', { name: 'Был' }))
    await userEvent.click(screen.getByRole('button', { name: 'Предыдущий день' }))
    const dialog = await screen.findByRole('dialog', { name: 'Отметки не сохранены' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Перейти без сохранения' }))
    await waitFor(() => expect(calls.some((r) => r.url.includes(`date=${addDays(today, -1)}`))).toBe(true))
  })

  it('админ: долг у ученика, отмена занятия, внеплановое занятие (lesson_exists — под временем)', async () => {
    const l = lesson({ date: today, students: [{ student_id: 's1', full_name: 'Асанов Бекзат', status: null, lessons_left: null, has_debt: true }] })
    const { calls } = mockApi({
      'GET /api/v1/me': { body: meAdmin },
      [`GET ${C}/lessons`]: { body: list([l]) },
      [`GET ${C}/groups`]: { body: list([group]) },
      [`PATCH ${C}/lessons/les1`]: { body: { ...l, cancelled: true } },
      [`POST ${C}/lessons`]: apiError(409, 'lesson_exists', 'exists'),
    })
    renderRoute('/c/c1/today')
    expect(await screen.findByText('долг')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Асанов Бекзат' })).toHaveAttribute('href', '/c/c1/students/s1')

    await userEvent.click(screen.getByRole('button', { name: /Действия с занятием/ }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Отменить занятие' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Отменить занятие' }))
    expect(await screen.findByText(/Занятие отменено ·/)).toBeInTheDocument()
    expect(await bodyOf(calls.find((r) => r.method === 'PATCH')!)).toEqual({ cancelled: true })

    await userEvent.click(screen.getByRole('button', { name: 'Внеплановое занятие' }))
    const dialog = await screen.findByRole('dialog', { name: 'Внеплановое занятие' })
    await userEvent.selectOptions(within(dialog).getByLabelText('Группы'), 'g1')
    await userEvent.type(within(dialog).getByLabelText('Начало'), '18:00')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Добавить' }))
    expect(await within(dialog).findByText('У группы уже есть занятие в это время.')).toBeInTheDocument()
    expect(await bodyOf(calls.find((r) => r.method === 'POST')!)).toEqual({ group_id: 'g1', date: today, start_time: '18:00' })
  })
})
