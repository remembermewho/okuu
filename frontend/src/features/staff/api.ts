import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/api/client'
import { qk } from '@/api/keys'
import { meKey } from '@/app/queryClient'

export type StaffMember = Schemas['StaffMember']
export type Invite = Schemas['Invite']
export type StaffRole = Schemas['ChangeRoleRequest']['role']

export function useStaff(centerId: string, enabled = true) {
  return useQuery({
    queryKey: qk.staff(centerId),
    queryFn: () => unwrap(api.GET('/api/v1/c/{centerId}/staff', { params: { path: { centerId } } })),
    select: (d) => d.items,
    enabled,
  })
}

export function useInvites(centerId: string, enabled: boolean) {
  return useQuery({
    queryKey: qk.invites(centerId),
    queryFn: () => unwrap(api.GET('/api/v1/c/{centerId}/staff/invites', { params: { path: { centerId } } })),
    select: (d) => d.items,
    enabled,
  })
}

export function useCreateInvite(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['CreateInviteRequest']) => unwrap(api.POST('/api/v1/c/{centerId}/staff/invites', { params: { path: { centerId } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.invites(centerId) }),
    meta: { silent: true },
  })
}

export function useRevokeInvite(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (inviteId: string) => unwrap(api.DELETE('/api/v1/c/{centerId}/staff/invites/{inviteId}', { params: { path: { centerId, inviteId } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.invites(centerId) }),
  })
}

export function useChangeRole(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: StaffRole }) =>
      unwrap(api.PATCH('/api/v1/c/{centerId}/staff/{userId}', { params: { path: { centerId, userId } }, body: { role } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.staff(centerId) }),
  })
}

export function useRemoveStaff(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (userId: string) => unwrap(api.DELETE('/api/v1/c/{centerId}/staff/{userId}', { params: { path: { centerId, userId } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.staff(centerId) })
      void qc.invalidateQueries({ queryKey: qk.groups(centerId) })
    },
  })
}

// ---- публичная страница приглашения

export function usePublicInvite(token: string) {
  return useQuery({
    queryKey: ['invite', token],
    queryFn: () => unwrap(api.GET('/api/v1/invites/{token}', { params: { path: { token } } })),
    retry: false,
  })
}

export function useAcceptInvite(token: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body?: Schemas['AcceptInviteRequest']) => unwrap(api.POST('/api/v1/invites/{token}/accept', { params: { path: { token } }, body })),
    onSuccess: (me) => qc.setQueryData(meKey, me),
    meta: { silent: true },
  })
}
