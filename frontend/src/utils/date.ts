// Даты: в API — 'YYYY-MM-DD' (дата в поясе центра) и RFC 3339 (моменты), на экране —
// «19 октября» и «19.10.2026». Даты без времени считаем в UTC, чтобы пояс браузера их не сдвигал.

const MONTHS_GEN = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря']
const MONTHS_NOM = ['Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь', 'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь']
const WEEKDAYS = ['Вс', 'Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб']
const WEEKDAYS_FULL = ['воскресенье', 'понедельник', 'вторник', 'среда', 'четверг', 'пятница', 'суббота']

const pad = (n: number) => String(n).padStart(2, '0')

function parseISODate(iso: string): Date {
  const [y, m, d] = iso.split('-').map(Number)
  return new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1))
}

function toISODate(d: Date): string {
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`
}

/** «19 октября». */
export function formatDayMonth(iso: string): string {
  const d = parseISODate(iso)
  return `${d.getUTCDate()} ${MONTHS_GEN[d.getUTCMonth()]}`
}

/** «19.10.2026». */
export function formatDate(iso: string): string {
  const d = parseISODate(iso)
  return `${pad(d.getUTCDate())}.${pad(d.getUTCMonth() + 1)}.${d.getUTCFullYear()}`
}

/** Момент времени из API в поясе браузера: «19.10.2026, 14:05». */
export function formatDateTime(ts: string): string {
  const d = new Date(ts)
  return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${d.getFullYear()}, ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** «Пн» — день недели даты. */
export function weekdayShort(iso: string): string {
  return WEEKDAYS[parseISODate(iso).getUTCDay()] ?? ''
}

/** «понедельник». */
export function weekdayLong(iso: string): string {
  return WEEKDAYS_FULL[parseISODate(iso).getUTCDay()] ?? ''
}

/** Название дня недели по номеру из расписания (1 — понедельник … 7 — воскресенье). */
export function dowShort(dow: number): string {
  return WEEKDAYS[dow % 7] ?? ''
}

export function addDays(iso: string, n: number): string {
  const d = parseISODate(iso)
  d.setUTCDate(d.getUTCDate() + n)
  return toISODate(d)
}

/** Сегодня по часам браузера, 'YYYY-MM-DD'. */
export function todayISO(now: Date = new Date()): string {
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}

/** Текущий месяц 'YYYY-MM'. */
export function currentMonth(now: Date = new Date()): string {
  return todayISO(now).slice(0, 7)
}

/** '2026-10' → «Октябрь 2026». */
export function formatMonth(month: string): string {
  const [y, m] = month.split('-').map(Number)
  return `${MONTHS_NOM[(m ?? 1) - 1]} ${y}`
}

/** '2026-10' ± n месяцев. */
export function addMonths(month: string, n: number): string {
  const [y = 1970, m = 1] = month.split('-').map(Number)
  const total = y * 12 + (m - 1) + n
  return `${Math.floor(total / 12)}-${pad((total % 12) + 1)}`
}

/** Разница в днях b − a. */
export function daysBetween(a: string, b: string): number {
  return Math.round((parseISODate(b).getTime() - parseISODate(a).getTime()) / 86_400_000)
}
