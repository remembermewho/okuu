import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/api/client'
import { qk } from '@/api/keys'

export type WhatsAppReminder = Schemas['WhatsAppReminder']

/** Текст и ссылка без записи: «Скопировать текст» и демо-центр. */
export function fetchWhatsAppReminder(centerId: string, studentId: string): Promise<WhatsAppReminder> {
  return unwrap(api.GET('/api/v1/c/{centerId}/students/{studentId}/whatsapp-reminder', { params: { path: { centerId, studentId } } }))
}

/** Кнопка WhatsApp: сервер отдаёт ссылку и записывает, что её открыли. */
export function useOpenWhatsApp(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (studentId: string) =>
      unwrap(api.POST('/api/v1/c/{centerId}/students/{studentId}/whatsapp-reminder', { params: { path: { centerId, studentId } } })),
    onSuccess: () => {
      // «Открыто в WhatsApp» в должниках и карточке.
      void qc.invalidateQueries({ queryKey: qk.debtors(centerId) })
      void qc.invalidateQueries({ queryKey: qk.students(centerId) })
    },
    meta: { silent: true },
  })
}
