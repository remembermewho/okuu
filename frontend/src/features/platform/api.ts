import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/api/client'
import { qk } from '@/api/keys'

export type AdminCenter = Schemas['AdminCenter']
export type AdminCenterCard = Schemas['AdminCenterCard']
export type RegistrationLink = Schemas['RegistrationLink']
export type CenterStatus = Schemas['CenterStatus']
export type Plan = Schemas['Plan']

export function useAdminCenters(f: { q: string; status: CenterStatus | '' }) {
  return useQuery({
    queryKey: qk.platformCenters(f),
    queryFn: () =>
      unwrap(api.GET('/api/v1/admin/centers', { params: { query: { q: f.q || undefined, status: f.status || undefined } } })),
    placeholderData: keepPreviousData,
  })
}

export function useAdminCenter(id: string) {
  return useQuery({
    queryKey: qk.platformCenter(id),
    queryFn: () => unwrap(api.GET('/api/v1/admin/centers/{id}', { params: { path: { id } } })),
  })
}

/** Ответ действий с центром — обновлённая карточка: кладём её в кэш, список перечитываем. */
function useCenterMutation<V>(id: string, fn: (v: V) => Promise<AdminCenterCard>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: (card) => {
      qc.setQueryData(qk.platformCenter(id), card)
      void qc.invalidateQueries({ queryKey: ['platform', 'centers'] })
    },
  })
}

export function useApproveCenter(id: string) {
  return useCenterMutation(id, () => unwrap(api.POST('/api/v1/admin/centers/{id}/approve', { params: { path: { id } } })))
}

export function useBlockCenter(id: string) {
  return useCenterMutation(id, (reason: string) => unwrap(api.POST('/api/v1/admin/centers/{id}/block', { params: { path: { id } }, body: { reason } })))
}

export function useUnblockCenter(id: string) {
  return useCenterMutation(id, () => unwrap(api.POST('/api/v1/admin/centers/{id}/unblock', { params: { path: { id } } })))
}

export function usePatchAdminCenter(id: string) {
  return useCenterMutation(id, (body: Schemas['AdminPatchCenterRequest']) => unwrap(api.PATCH('/api/v1/admin/centers/{id}', { params: { path: { id } }, body })))
}

export function useOwnerPasswordReset(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => unwrap(api.POST('/api/v1/admin/centers/{id}/owner-password-reset', { params: { path: { id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.platformCenter(id) }), // запись в журнале
  })
}

export function useRegistrationLinks() {
  return useQuery({ queryKey: qk.platformLinks, queryFn: () => unwrap(api.GET('/api/v1/admin/registration-links')) })
}

export function useCreateRegistrationLink() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (note: string) => unwrap(api.POST('/api/v1/admin/registration-links', { body: { note } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.platformLinks }),
    meta: { silent: true },
  })
}

export function useRevokeRegistrationLink() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => unwrap(api.DELETE('/api/v1/admin/registration-links/{id}', { params: { path: { id } } })),
    onSettled: () => void qc.invalidateQueries({ queryKey: qk.platformLinks }),
  })
}
