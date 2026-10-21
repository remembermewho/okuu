import { describe, expect, it } from 'vitest'
import { addDays, addMonths, currentMonth, daysBetween, dowShort, formatDate, formatDayMonth, formatMonth, todayISO, weekdayShort } from './date'

describe('date', () => {
  it('формат дат', () => {
    expect(formatDayMonth('2026-10-19')).toBe('19 октября')
    expect(formatDayMonth('2026-03-01')).toBe('1 марта')
    expect(formatDate('2026-10-09')).toBe('09.10.2026')
    expect(formatMonth('2026-10')).toBe('Октябрь 2026')
    expect(weekdayShort('2026-10-19')).toBe('Пн')
    expect(dowShort(1)).toBe('Пн')
    expect(dowShort(7)).toBe('Вс')
  })

  it('арифметика', () => {
    expect(addDays('2026-10-31', 1)).toBe('2026-11-01')
    expect(addDays('2026-03-01', -1)).toBe('2026-02-28')
    expect(addDays('2024-03-01', -1)).toBe('2024-02-29')
    expect(addMonths('2026-12', 1)).toBe('2027-01')
    expect(addMonths('2026-01', -1)).toBe('2025-12')
    expect(daysBetween('2026-10-12', '2026-10-19')).toBe(7)
  })

  it('сегодня по часам браузера', () => {
    const d = new Date(2026, 9, 5, 23, 30)
    expect(todayISO(d)).toBe('2026-10-05')
    expect(currentMonth(d)).toBe('2026-10')
  })
})
