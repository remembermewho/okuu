import {
  BarChart3,
  CalendarCheck,
  CircleDollarSign,
  FileSpreadsheet,
  History,
  LayoutGrid,
  Settings,
  Tag,
  UserCog,
  Users,
  type LucideIcon,
} from 'lucide-react'
import { ru } from '@/i18n/ru'
import { canSee, type Role, type Section } from './permissions'

export type NavItem = { section: Section; label: string; icon: LucideIcon }

/** Все разделы в порядке бокового меню (как в ТЗ: Долги · Сегодня · Ученики · Ещё). */
export const navItems: NavItem[] = [
  { section: 'debtors', label: ru.nav.debtors, icon: CircleDollarSign },
  { section: 'today', label: ru.nav.today, icon: CalendarCheck },
  { section: 'students', label: ru.nav.students, icon: Users },
  { section: 'groups', label: ru.nav.groups, icon: LayoutGrid },
  { section: 'tariffs', label: ru.nav.tariffs, icon: Tag },
  { section: 'staff', label: ru.nav.staff, icon: UserCog },
  { section: 'import', label: ru.nav.import, icon: FileSpreadsheet },
  { section: 'report', label: ru.nav.report, icon: BarChart3 },
  { section: 'settings', label: ru.nav.settings, icon: Settings },
  { section: 'audit', label: ru.nav.audit, icon: History },
]

export const visibleItems = (role: Role) => navItems.filter((i) => canSee(role, i.section))

/** Нижнее меню телефона: 3–4 пункта; остальное — в «Ещё». */
export function bottomItems(role: Role): { items: NavItem[]; more: boolean } {
  const visible = visibleItems(role)
  if (visible.length <= 4) return { items: visible, more: false }
  const main: Section[] = ['today', 'debtors', 'students']
  return { items: visible.filter((i) => main.includes(i.section)), more: true }
}

export const moreItems = (role: Role) => {
  const { items } = bottomItems(role)
  return visibleItems(role).filter((i) => !items.includes(i))
}
