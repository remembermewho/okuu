import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/api/client'
import { qk } from '@/api/keys'
import { meKey } from '@/app/queryClient'

export type Lesson = Schemas['Lesson']
export type LessonStudent = Lesson['students'][number]
export type Mark = Schemas['AttendanceStatus']

/**
 * Занятия на дату (бэкенд создаёт их по расписанию при первом открытии, R11). Ответ несёт «сегодня»
 * центра — им освежаем /me (вкладка могла простоять открытой за полночь).
 */
export function useLessons(centerId: string, date: string) {
  const qc = useQueryClient()
  return useQuery({
    queryKey: qk.lessons(centerId, date),
    queryFn: async () => {
      const res = await unwrap(api.GET('/api/v1/c/{centerId}/lessons', { params: { path: { centerId }, query: { date } } }))
      qc.setQueryData<Schemas['Me']>(meKey, (me) =>
        me && me.centers.some((c) => c.id === centerId && c.today !== res.today)
          ? { ...me, centers: me.centers.map((c) => (c.id === centerId ? { ...c, today: res.today } : c)) }
          : me,
      )
      return res
    },
    select: (d) => d.items,
  })
}

function useReplaceLesson(centerId: string) {
  const qc = useQueryClient()
  return (lesson: Lesson) => {
    qc.setQueryData<Schemas['LessonList']>(qk.lessons(centerId, lesson.date), (old) =>
      old ? { ...old, items: old.items.map((l) => (l.id === lesson.id ? lesson : l)) } : old,
    )
    // Отметки меняют остаток абонемента и посещения в карточке ученика.
    void qc.invalidateQueries({ queryKey: qk.students(centerId) })
  }
}

export function useSaveMarks(centerId: string) {
  const replace = useReplaceLesson(centerId)
  return useMutation({
    mutationFn: ({ lessonId, marks }: { lessonId: string; marks: Schemas['MarkRequest']['marks'] }) =>
      unwrap(api.PUT('/api/v1/c/{centerId}/lessons/{lessonId}/attendance', { params: { path: { centerId, lessonId } }, body: { marks } })),
    onSuccess: replace,
    meta: { silent: true },
  })
}

export function useSetLessonCancelled(centerId: string) {
  const replace = useReplaceLesson(centerId)
  return useMutation({
    mutationFn: ({ lessonId, cancelled }: { lessonId: string; cancelled: boolean }) =>
      unwrap(api.PATCH('/api/v1/c/{centerId}/lessons/{lessonId}', { params: { path: { centerId, lessonId } }, body: { cancelled } })),
    onSuccess: replace,
  })
}

export function useCreateLesson(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['CreateLessonRequest']) => unwrap(api.POST('/api/v1/c/{centerId}/lessons', { params: { path: { centerId } }, body })),
    onSuccess: (l) => qc.invalidateQueries({ queryKey: qk.lessons(centerId, l.date) }),
    meta: { silent: true },
  })
}
