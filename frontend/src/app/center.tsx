import { createContext, useContext } from 'react'
import type { Role } from './permissions'

export type CurrentCenter = { id: string; name: string; role: Role; centersCount: number }

export const CenterContext = createContext<CurrentCenter | null>(null)

/** Текущий центр (из пути /c/:centerId и /me). */
export function useCenter(): CurrentCenter {
  const c = useContext(CenterContext)
  if (!c) throw new Error('useCenter вне /c/:centerId')
  return c
}

/** Путь внутри текущего центра: centerPath(id, 'students') → /c/<id>/students. */
export function centerPath(centerId: string, rest = ''): string {
  return `/c/${centerId}${rest ? '/' + rest.replace(/^\//, '') : ''}`
}
