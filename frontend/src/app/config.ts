import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '@/api/client'

/** Публичные настройки сервера (GET /config): включена ли онлайн-оплата (ONLINE_PAYMENTS_ENABLED). */
export function useAppConfig() {
  return useQuery({
    queryKey: ['config'],
    queryFn: () => unwrap(api.GET('/api/v1/config')),
    staleTime: 10 * 60_000,
  })
}

/** Онлайн-оплата включена; пока настройки не загружены — считаем выключенной (ничего лишнего не показываем). */
export function useOnlinePayments(): boolean {
  return useAppConfig().data?.online_payments ?? false
}
