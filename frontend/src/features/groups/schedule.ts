import type { Slot } from '@/features/catalog/api'
import { dowShort } from '@/utils/date'

/**
 * Расписание одной строкой: дни с одинаковым временем вместе.
 * [{1,15:00,16:30},{3,15:00,16:30},{5,10:00,11:00}] → «Пн, Ср 15:00–16:30 · Пт 10:00–11:00».
 */
export function scheduleText(slots: Slot[]): string {
  const byTime = new Map<string, number[]>()
  for (const sl of [...slots].sort((a, b) => a.dow - b.dow)) {
    const key = `${sl.start}–${sl.end}`
    byTime.set(key, [...(byTime.get(key) ?? []), sl.dow])
  }
  return [...byTime.entries()]
    .sort((a, b) => (a[1][0] ?? 0) - (b[1][0] ?? 0))
    .map(([time, days]) => `${days.map(dowShort).join(', ')} ${time}`)
    .join(' · ')
}
