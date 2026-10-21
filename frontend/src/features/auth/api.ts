import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/api/client'
import { meKey } from '@/app/queryClient'

export type Me = Schemas['Me']

/** Профиль и центры. 401 — не вошёл (ошибка остаётся в query, без повторов). */
export function useMe() {
  return useQuery({
    queryKey: meKey,
    queryFn: () => unwrap(api.GET('/api/v1/me')),
    retry: false,
    staleTime: 5 * 60_000,
  })
}

export function useLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['LoginRequest']) => unwrap(api.POST('/api/v1/auth/login', { body })),
    onSuccess: (me) => {
      qc.clear()
      qc.setQueryData(meKey, me)
    },
    meta: { silent: true },
  })
}

export function useRegister() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['RegisterRequest']) => unwrap(api.POST('/api/v1/auth/register', { body })),
    onSuccess: (me) => {
      qc.clear()
      qc.setQueryData(meKey, me)
    },
    meta: { silent: true },
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => unwrap(api.POST('/api/v1/auth/logout')),
    onSettled: () => {
      qc.clear()
    },
  })
}
