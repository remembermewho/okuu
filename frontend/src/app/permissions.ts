// Права по ролям — как в роутере бэкенда (backend/internal/httpapi/router.go и описания в openapi.yaml).
// Бэкенд проверяет сам; здесь — чтобы не показывать меню и кнопки, которые ответят 403.
import type { Schemas } from '@/api/client'

export type Role = Schemas['Role']

export type Section = 'today' | 'debtors' | 'students' | 'broadcasts' | 'groups' | 'tariffs' | 'staff' | 'import' | 'report' | 'settings' | 'audit'

const sections: Record<Section, Role[]> = {
  today: ['owner', 'admin', 'teacher'],
  groups: ['owner', 'admin', 'teacher'], // преподаватель — только свои, без изменений
  debtors: ['owner', 'admin'],
  students: ['owner', 'admin'],
  broadcasts: ['owner', 'admin'],
  tariffs: ['owner', 'admin'],
  staff: ['owner', 'admin'],
  import: ['owner', 'admin'],
  settings: ['owner'], // S8 — владелец (админ читает, но меню ему не показываем)
  report: ['owner'],
  audit: ['owner'],
}

export function canSee(role: Role, section: Section): boolean {
  return sections[section].includes(role)
}

/** Действия внутри экранов. */
export const can = {
  manageCatalog: (r: Role) => r === 'owner' || r === 'admin', // группы, тарифы, занятия вне расписания
  money: (r: Role) => r === 'owner' || r === 'admin',
  manageStaff: (r: Role) => r === 'owner', // приглашения, роли, удаление
  cancelFinik: (r: Role) => r === 'owner',
  auditRaw: (r: Role) => r === 'owner', // «Технические данные» в журнале — сырой before/after
}

/** Первый экран после входа в центр. */
export function homeSection(role: Role): Section {
  return role === 'teacher' ? 'today' : 'debtors'
}
