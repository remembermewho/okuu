import { useCallback, useState } from 'react'
import type { RemindersResult } from './api'

type Saved = { result: RemindersResult; opened: string[] }

const keyOf = (centerId: string) => `okuu:reminders-result:${centerId}`

function load(centerId: string): Saved | undefined {
  try {
    const raw = sessionStorage.getItem(keyOf(centerId))
    return raw ? (JSON.parse(raw) as Saved) : undefined
  } catch {
    return undefined
  }
}

function save(centerId: string, value: Saved | undefined) {
  try {
    if (value) sessionStorage.setItem(keyOf(centerId), JSON.stringify(value))
    else sessionStorage.removeItem(keyOf(centerId))
  } catch {
    // Приватный режим или переполнение: список живёт, пока открыта страница.
  }
}

/**
 * Итог «Напомнить» со списком WhatsApp. На телефоне браузер может выгрузить страницу, пока открыт WhatsApp,
 * поэтому пока сотрудник не закрыл окно, итог лежит в sessionStorage этой вкладки.
 */
export function useRemindersResult(centerId: string) {
  const [saved, setSaved] = useState<Saved | undefined>(() => load(centerId))

  const update = useCallback(
    (next: Saved | undefined) => {
      setSaved(next)
      save(centerId, next && (next.result.whatsapp?.length ?? 0) > 0 ? next : undefined) // без списка WhatsApp хранить нечего
    },
    [centerId],
  )

  return {
    result: saved?.result,
    openedParents: saved?.opened ?? [],
    setResult: (result: RemindersResult) => update({ result, opened: [] }),
    markOpened: (parentId: string) => {
      if (saved) update({ ...saved, opened: [...saved.opened, parentId] })
    },
    closeResult: () => update(undefined),
  }
}
