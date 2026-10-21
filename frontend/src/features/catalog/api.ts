import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/api/client'
import { qk } from '@/api/keys'

export type Group = Schemas['Group']
export type Tariff = Schemas['Tariff']
export type Slot = Schemas['Slot']

export function useGroups(centerId: string, archived = false) {
  return useQuery({
    queryKey: qk.groups(centerId, archived),
    queryFn: () => unwrap(api.GET('/api/v1/c/{centerId}/groups', { params: { path: { centerId }, query: { archived } } })),
    select: (d) => d.items,
  })
}

export function useGroup(centerId: string, groupId: string) {
  return useQuery({
    queryKey: qk.group(centerId, groupId),
    queryFn: () => unwrap(api.GET('/api/v1/c/{centerId}/groups/{groupId}', { params: { path: { centerId, groupId } } })),
  })
}

export function useTariffs(centerId: string, archived = false, enabled = true) {
  return useQuery({
    queryKey: qk.tariffs(centerId, archived),
    queryFn: () => unwrap(api.GET('/api/v1/c/{centerId}/tariffs', { params: { path: { centerId }, query: { archived } } })),
    select: (d) => d.items,
    enabled,
  })
}

export function useCreateTariff(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['CreateTariffRequest']) => unwrap(api.POST('/api/v1/c/{centerId}/tariffs', { params: { path: { centerId } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.tariffs(centerId) }),
    meta: { silent: true },
  })
}

export function usePatchTariff(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: Schemas['PatchTariffRequest'] }) =>
      unwrap(api.PATCH('/api/v1/c/{centerId}/tariffs/{tariffId}', { params: { path: { centerId, tariffId: id } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.tariffs(centerId) }),
    meta: { silent: true },
  })
}

export function useArchiveTariff(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (tariffId: string) => unwrap(api.DELETE('/api/v1/c/{centerId}/tariffs/{tariffId}', { params: { path: { centerId, tariffId } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.tariffs(centerId) }),
  })
}

export function useSaveGroup(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    // Тело подходит и для создания, и для изменения (все поля сразу).
    mutationFn: ({ id, body }: { id?: string; body: Schemas['CreateGroupRequest'] & Schemas['PatchGroupRequest'] }) =>
      id
        ? unwrap(api.PATCH('/api/v1/c/{centerId}/groups/{groupId}', { params: { path: { centerId, groupId: id } }, body }))
        : unwrap(api.POST('/api/v1/c/{centerId}/groups', { params: { path: { centerId } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.groups(centerId) }),
    meta: { silent: true },
  })
}

export function useArchiveGroup(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (groupId: string) => unwrap(api.DELETE('/api/v1/c/{centerId}/groups/{groupId}', { params: { path: { centerId, groupId } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.groups(centerId) }),
  })
}

/** Подписи для селектов. */
export const groupOptions = (groups: Group[] | undefined) => (groups ?? []).map((g) => ({ value: g.id, label: g.name }))
