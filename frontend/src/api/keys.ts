// Ключи кэша TanStack Query. Всё про центр начинается с ['c', centerId] — при смене пользователя
// кэш очищается целиком (useLogin/useLogout).

export const qk = {
  center: (c: string) => ['c', c] as const,
  students: (c: string) => ['c', c, 'students'] as const,
  student: (c: string, id: string) => ['c', c, 'students', 'one', id] as const,
  studentList: (c: string, f: object) => ['c', c, 'students', 'list', f] as const,
  studentAttendance: (c: string, id: string, month: string) => ['c', c, 'students', 'attendance', id, month] as const,
  parentLookup: (c: string, phone: string) => ['c', c, 'students', 'parent-lookup', phone] as const,
  ledger: (c: string, id?: string) => (id ? (['c', c, 'ledger', id] as const) : (['c', c, 'ledger'] as const)),
  debtors: (c: string, group?: string, status?: string) =>
    group === undefined ? (['c', c, 'debtors'] as const) : (['c', c, 'debtors', group, status ?? 'overdue'] as const),
  groups: (c: string, archived?: boolean) => (archived === undefined ? (['c', c, 'groups'] as const) : (['c', c, 'groups', archived] as const)),
  group: (c: string, id: string) => ['c', c, 'groups', 'one', id] as const,
  tariffs: (c: string, archived?: boolean) => (archived === undefined ? (['c', c, 'tariffs'] as const) : (['c', c, 'tariffs', archived] as const)),
  staff: (c: string) => ['c', c, 'staff'] as const,
  invites: (c: string) => ['c', c, 'invites'] as const,
  lessons: (c: string, date?: string) => (date ? (['c', c, 'lessons', date] as const) : (['c', c, 'lessons'] as const)),
  lesson: (c: string, id: string) => ['c', c, 'lessons', 'one', id] as const,
  settings: (c: string) => ['c', c, 'settings'] as const,
  report: (c: string, month: string) => ['c', c, 'report', month] as const,
  audit: (c: string, f: object) => ['c', c, 'audit', f] as const,

  // Панель суперадмина — всё под ['platform'].
  platform: ['platform'] as const,
  platformCenters: (f: object) => ['platform', 'centers', f] as const,
  platformCenter: (id: string) => ['platform', 'center', id] as const,
  platformLinks: ['platform', 'links'] as const,
}
