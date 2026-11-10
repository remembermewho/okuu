import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/api/client'
import { qk } from '@/api/keys'

export type Broadcast = Schemas['Broadcast']
export type BroadcastDetail = Schemas['BroadcastDetail']
export type BroadcastRequest = Schemas['BroadcastRequest']
export type BroadcastPreview = Schemas['BroadcastPreview']

export function useBroadcasts(centerId: string) {
  return useQuery({
    queryKey: qk.broadcasts(centerId),
    queryFn: () => unwrap(api.GET('/api/v1/c/{centerId}/broadcasts', { params: { path: { centerId } } })),
  })
}

/** Пока бот отправляет — итоги обновляются сами. */
export function useBroadcast(centerId: string, id: string | null) {
  return useQuery({
    queryKey: qk.broadcast(centerId, id ?? ''),
    enabled: id !== null,
    queryFn: () => unwrap(api.GET('/api/v1/c/{centerId}/broadcasts/{broadcastId}', { params: { path: { centerId, broadcastId: id ?? '' } } })),
    refetchInterval: (q) => ((q.state.data?.counts.telegram_queued ?? 0) > 0 ? 2000 : false),
  })
}

export function usePreviewBroadcast(centerId: string) {
  return useMutation({
    mutationFn: (body: BroadcastRequest) => unwrap(api.POST('/api/v1/c/{centerId}/broadcasts/preview', { params: { path: { centerId } }, body })),
    meta: { silent: true },
  })
}

/** key — Idempotency-Key, один на открытие предпросмотра: двойное нажатие не создаст вторую рассылку. */
export function useCreateBroadcast(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ body, key }: { body: BroadcastRequest; key: string }) =>
      unwrap(api.POST('/api/v1/c/{centerId}/broadcasts', { params: { path: { centerId }, header: { 'Idempotency-Key': key } }, body })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.broadcasts(centerId) }),
    meta: { silent: true },
  })
}

export function useOpenBroadcastLink(centerId: string, broadcastId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (parentId: string) =>
      unwrap(
        api.POST('/api/v1/c/{centerId}/broadcasts/{broadcastId}/recipients/{parentId}/whatsapp-opened', {
          params: { path: { centerId, broadcastId, parentId } },
        }),
      ),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.broadcasts(centerId) }),
  })
}
