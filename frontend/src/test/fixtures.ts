import type { Schemas } from '@/api/client'

/** «Сегодня» центра в тестах — нарочно не по часам машины: интерфейс должен брать дату с сервера. */
export const TODAY = '2026-10-19'
export const center = (role: 'owner' | 'admin' | 'teacher', id = 'c1', name = 'Билим', status: 'pending' | 'active' | 'blocked' = 'active') => ({
  id,
  name,
  role,
  status,
  today: TODAY,
  is_demo: false,
})

export const meOwner = { id: 'u1', name: 'Айгуль', phone: '+996700123456', centers: [center('owner')], is_platform_admin: false }
export const meAdmin = { ...meOwner, centers: [center('admin')] }
export const meTeacher = { ...meOwner, centers: [center('teacher')] }

export const group: Schemas['Group'] = {
  id: 'g1',
  name: 'Английский A1',
  teacher_id: null,
  teacher_name: null,
  schedule: [{ dow: 1, start: '15:00', end: '16:30' }],
  default_tariff_id: 't1',
  archived: false,
  students_count: 3,
} as Schemas['Group']

export const tariffMonth: Schemas['Tariff'] = { id: 't1', name: 'Месяц', kind: 'monthly', price: 300_000, lessons_count: null, archived: false } as Schemas['Tariff']
export const tariffPackage: Schemas['Tariff'] = { id: 't2', name: '8 занятий', kind: 'package', price: 240_000, lessons_count: 8, archived: false } as Schemas['Tariff']

export const parent: Schemas['Parent'] = { id: 'p1', name: 'Гульнара', phone: '+996700111222', tg_status: 'active', last_reminder_at: null, last_whatsapp_at: null, whatsapp_phone: '996700111222' }

export function student(over: Partial<Schemas['Student']> = {}): Schemas['Student'] {
  return {
    id: 's1',
    full_name: 'Асанов Бекзат',
    birth_date: null,
    status: 'active',
    discount: null,
    created_at: '2026-09-01T09:00:00Z',
    parent,
    enrollments: [
      {
        id: 'e1',
        group: { id: 'g1', name: 'Английский A1' },
        tariff: { id: 't1', name: 'Месяц', kind: 'monthly', price: 300_000 },
        start_date: '2026-09-01',
        end_date: null,
        lessons_left: null,
        billed_through: '2026-10-01',
        active: true,
      },
    ],
    balance: -300_000,
    ...over,
  }
}

export function operation(over: Partial<Schemas['Operation']> = {}): Schemas['Operation'] {
  return {
    id: 'l1',
    student_id: 's1',
    kind: 'charge',
    amount: -300_000,
    method: null,
    comment: null,
    enrollment_id: 'e1',
    period: '2026-10-01',
    due_date: '2026-10-05',
    list_price: 300_000,
    discount: 0,
    created_at: '2026-10-01T00:05:00Z',
    created_by: null,
    cancelled: false,
    cancelled_at: null,
    cancelled_by: null,
    cancel_reason: null,
    ...over,
  } as Schemas['Operation']
}

export function lesson(over: Partial<Schemas['Lesson']> = {}): Schemas['Lesson'] {
  return {
    id: 'les1',
    group: { id: 'g1', name: 'Английский A1' },
    date: '2026-10-19',
    start_time: '15:00',
    end_time: '16:30',
    cancelled: false,
    students_count: 2,
    marked_count: 0,
    students: [
      { student_id: 's1', full_name: 'Асанов Бекзат', status: null, lessons_left: null },
      { student_id: 's2', full_name: 'Бекова Айжан', status: 'present', lessons_left: 1 },
    ],
    ...over,
  }
}

export const list = <T>(items: T[]) => ({ items, next_cursor: null })
