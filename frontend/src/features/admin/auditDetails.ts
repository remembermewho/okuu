// «Подробнее» в журнале — человекочитаемо: поля по-русски, суммы в сомах, даты «03.11.2026», без id.
// Сырой before/after — только в «Технических данных» (владелец).
import { ru } from '@/i18n/ru'
import { dowShort, formatDate, formatDateTime, formatMonth } from '@/utils/date'
import { formatSom } from '@/utils/money'
import type { AuditItem } from './api'

const t = ru.audit
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const DATE = /^\d{4}-\d{2}-\d{2}$/
const MOMENT = /^\d{4}-\d{2}-\d{2}T/
/** Поля в тыйынах. */
const MONEY = new Set(['amount', 'list_price', 'price', 'manual_amount', 'debt_total', 'link_amount', 'paid_amount'])

/** Строка расшифровки: before/after — undefined, если поля не было в этой части записи. */
export type AuditField = { key: string; label: string; before?: string; after?: string }

type Obj = Record<string, unknown>
const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v)
const hiddenKey = (k: string) => k === 'id' || k.endsWith('_id') || UUID.test(k)

export function fieldLabel(key: string, entity: string): string {
  return key === 'name' ? t.nameOf(entity) : (t.fields[key] ?? key)
}

/** Значение поля для экрана: «1 500 с», «03.11.2026», «Ноябрь 2026», «да», «Оплата»… */
export function formatValue(key: string, v: unknown): string {
  if (v === null || v === undefined || v === '') return t.empty
  if (typeof v === 'boolean') return v ? t.yes : t.no
  if (key === 'discount' && isObj(v)) return v.kind === 'percent' ? `${String(v.value)}%` : formatSom(Number(v.value))
  if (typeof v === 'number') return MONEY.has(key) || key === 'discount' ? formatSom(v) : String(v)
  if (typeof v === 'string') {
    const coded = t.values[key]?.[v]
    if (coded) return coded
    if (key === 'period' && DATE.test(v)) return formatMonth(v.slice(0, 7))
    if (DATE.test(v)) return formatDate(v)
    if (MOMENT.test(v)) return formatDateTime(v)
    if (key === 'start_time') return v.slice(0, 5)
    return v
  }
  if (Array.isArray(v)) {
    if (key === 'schedule') {
      return v.filter(isObj).map((s) => `${dowShort(Number(s.dow))} ${String(s.start).slice(0, 5)}–${String(s.end).slice(0, 5)}`).join(', ') || t.empty
    }
    // «Напомнили» — id учеников: показываем, сколько их.
    if (v.every((x) => typeof x === 'string' && UUID.test(x))) return t.studentsCount(v.length)
    return v.map((x) => formatValue(key, x)).join(', ')
  }
  if (isObj(v)) {
    const entries = Object.entries(v)
    // Отметки отменённого занятия — по id учеников: сколько было отмечено.
    if (entries.length > 0 && entries.every(([k]) => UUID.test(k))) return t.studentsCount(entries.length)
    return entries.filter(([k]) => !hiddenKey(k)).map(([k, x]) => `${fieldLabel(k, '')}: ${formatValue(k, x)}`).join(', ')
  }
  return String(v)
}

const markLabel = (v: unknown) => (v == null ? t.notMarked : (t.values.mark?.[String(v)] ?? String(v)))

/** Отметки посещаемости: ключи before/after — id учеников, подпись — ФИО из item.students. */
function attendanceFields(item: AuditItem): AuditField[] {
  const b = isObj(item.before) ? item.before : {}
  const a = isObj(item.after) ? item.after : {}
  return item.students
    .filter((s) => s.id in b || s.id in a)
    .map((s) => ({
      key: s.id,
      label: s.full_name,
      before: isObj(item.before) ? markLabel(b[s.id]) : undefined,
      after: s.id in a ? markLabel(a[s.id]) : undefined,
    }))
    .filter((f) => f.before !== f.after)
}

/**
 * Расшифровка записи журнала. Если есть и «было», и «стало» — только изменившиеся поля
 * (снимки ученика и настроек хранятся целиком).
 */
export function auditFields(item: AuditItem): AuditField[] {
  if (item.action.startsWith('attendance.')) return attendanceFields(item)
  const b = isObj(item.before) ? item.before : {}
  const a = isObj(item.after) ? item.after : {}
  const both = isObj(item.before) && isObj(item.after)
  const keys = [...new Set([...Object.keys(b), ...Object.keys(a)])].filter((k) => !hiddenKey(k))
  const out: AuditField[] = []
  for (const key of keys) {
    const before = key in b ? formatValue(key, b[key]) : undefined
    const after = key in a ? formatValue(key, a[key]) : undefined
    if (both && before === after) continue
    out.push({ key, label: fieldLabel(key, item.entity), before, after })
  }
  return out
}
