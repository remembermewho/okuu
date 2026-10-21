import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'
import { ApiError, NETWORK } from '@/api/client'
import { showToast } from '@/components/ui/Toast'
import { ru } from '@/i18n/ru'
import { errorMessage } from '@/utils/errors'

/** Куда уйти при 401 (подставляет router, чтобы не тянуть его сюда). */
let onUnauthorized: () => void = () => {}
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

export const meKey = ['me'] as const

function handle401(err: unknown, isMe: boolean) {
  if (err instanceof ApiError && err.status === 401 && !isMe) onUnauthorized()
}

export function createQueryClient() {
  const qc: QueryClient = new QueryClient({
    queryCache: new QueryCache({
      onError: (err, query) => {
        const isMe = query.queryKey[0] === meKey[0]
        handle401(err, isMe)
        // Нет сети — тост с «Повторить»; остальные ошибки показывает сам экран (ErrorState).
        if (err instanceof ApiError && err.code === NETWORK && query.state.data === undefined) {
          showToast({ message: ru.errors.network, tone: 'error', action: { label: ru.common.retry, onClick: () => void qc.refetchQueries({ queryKey: query.queryKey }) } })
        }
      },
    }),
    mutationCache: new MutationCache({
      onError: (err, _vars, _ctx, mutation) => {
        handle401(err, false)
        // Формы показывают ошибки сами (meta.silent); остальное — тостом.
        if (mutation.meta?.silent) return
        showToast({ message: errorMessage(err), tone: 'error' })
      },
    }),
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        refetchOnWindowFocus: false,
        retry: (count, err) => err instanceof ApiError && err.code === NETWORK && count < 2,
      },
      mutations: { retry: false },
    },
  })
  return qc
}

declare module '@tanstack/react-query' {
  interface Register {
    mutationMeta: { silent?: boolean }
    defaultError: ApiError
  }
}
