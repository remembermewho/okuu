import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/api/client'
import { qk } from '@/api/keys'

export type Debtor = Schemas['Debtor']
export type Operation = Schemas['Operation']
export type RemindersResult = Schemas['RemindersResponse']

export function useDebtors(centerId: string, group: string) {
  return useQuery({
    queryKey: qk.debtors(centerId, group),
    queryFn: () => unwrap(api.GET('/api/v1/c/{centerId}/debtors', { params: { path: { centerId }, query: group ? { group } : {} } })),
    placeholderData: keepPreviousData,
  })
}

export function useLedger(centerId: string, studentId: string) {
  return useInfiniteQuery({
    queryKey: qk.ledger(centerId, studentId),
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/api/v1/c/{centerId}/students/{studentId}/ledger', {
          params: { path: { centerId, studentId }, query: { limit: 20, cursor: pageParam } },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })
}

function invalidateMoney(qc: ReturnType<typeof useQueryClient>, centerId: string) {
  void qc.invalidateQueries({ queryKey: qk.students(centerId) })
  void qc.invalidateQueries({ queryKey: qk.debtors(centerId) })
  void qc.invalidateQueries({ queryKey: qk.ledger(centerId) })
}

/** Оплата / возврат / поправка. key — Idempotency-Key, один на открытие формы. */
export function useCreateOperation(centerId: string, studentId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ body, key }: { body: Schemas['OperationRequest']; key: string }) =>
      unwrap(
        api.POST('/api/v1/c/{centerId}/students/{studentId}/payments', {
          params: { path: { centerId, studentId }, header: { 'Idempotency-Key': key } },
          body,
        }),
      ),
    onSuccess: () => invalidateMoney(qc, centerId),
    meta: { silent: true },
  })
}

export function useCancelOperation(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ ledgerId, reason }: { ledgerId: string; reason: string }) =>
      unwrap(api.POST('/api/v1/c/{centerId}/ledger/{ledgerId}/cancel', { params: { path: { centerId, ledgerId } }, body: { reason } })),
    onSuccess: () => invalidateMoney(qc, centerId),
    meta: { silent: true },
  })
}

export function useReminders(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['RemindersRequest']) => unwrap(api.POST('/api/v1/c/{centerId}/reminders', { params: { path: { centerId } }, body })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.students(centerId) })
    },
  })
}
