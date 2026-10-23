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

/** Режим регистрации (invite | approval | open) и номер поддержки для WhatsApp. */
export function useRegistrationInfo() {
  return useQuery({
    queryKey: ['registration'],
    queryFn: () => unwrap(api.GET('/api/v1/auth/registration')),
    staleTime: 10 * 60_000,
  })
}

/** Действует ли ссылка регистрации /register/:token (404 — нет). */
export function useRegistrationLink(token: string) {
  return useQuery({
    queryKey: ['registration', token],
    queryFn: () => unwrap(api.GET('/api/v1/auth/registration/{token}', { params: { path: { token } } })),
    enabled: token !== '',
    retry: false,
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

export function usePatchMe() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['PatchMeRequest']) => unwrap(api.PATCH('/api/v1/me', { body })),
    onSuccess: (me) => qc.setQueryData(meKey, me),
    meta: { silent: true },
  })
}

/** Смена пароля: другие сессии закрываются на сервере, текущая остаётся. */
export function useChangePassword() {
  return useMutation({
    mutationFn: (body: Schemas['ChangePasswordRequest']) => unwrap(api.POST('/api/v1/me/password', { body })),
    meta: { silent: true },
  })
}

/** Выйти на всех устройствах: все сессии, включая текущую. */
export function useLogoutAll() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => unwrap(api.POST('/api/v1/auth/logout-all')),
    onSuccess: () => qc.clear(),
  })
}

export function usePasswordReset(token: string) {
  return useQuery({
    queryKey: ['password-reset', token],
    queryFn: () => unwrap(api.GET('/api/v1/password-reset/{token}', { params: { path: { token } } })),
    retry: false,
  })
}

export function useCompletePasswordReset(token: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['NewPasswordRequest']) => unwrap(api.POST('/api/v1/password-reset/{token}', { params: { path: { token } }, body })),
    // Все сессии пользователя закрыты — кэш (в том числе /me) больше не верен. Саму ссылку не трогаем:
    // иначе страница перезапросит уже использованную ссылку и покажет «недействительна».
    onSuccess: () => qc.removeQueries({ predicate: (q) => q.queryKey[0] !== 'password-reset' }),
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
