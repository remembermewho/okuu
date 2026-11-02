import { useMutation, useQuery } from '@tanstack/react-query'
import { CheckCircle2, Clock, ExternalLink, Info } from 'lucide-react'
import { useParams } from 'react-router'
import { api, unwrap } from '@/api/client'
import { useAppConfig } from '@/app/config'
import { CopyButton } from '@/components/shared'
import { Button, FormError, MoneyText, Skeleton } from '@/components/ui'
import { AuthLayout } from '@/features/auth/AuthLayout'
import { ru } from '@/i18n/ru'
import { formatDateTime } from '@/utils/date'
import { errorMessage, isApiError } from '@/utils/errors'
import { redirectTo } from '@/utils/redirect'
import s from './PayPage.module.css'

function usePayLink(token: string, enabled: boolean) {
  return useQuery({
    queryKey: ['pay', token],
    queryFn: () => unwrap(api.GET('/api/v1/pay/{token}', { params: { path: { token } } })),
    retry: false,
    enabled,
  })
}

function useStartPay(token: string) {
  return useMutation({
    mutationFn: () => unwrap(api.POST('/api/v1/pay/{token}/start', { params: { path: { token } } })),
    meta: { silent: true },
  })
}

/**
 * B2: страница оплаты родителя — по ссылке, без входа и без меню. Пока онлайн-оплата выключена
 * (ONLINE_PAYMENTS_ENABLED=false, GET /config) — только «Онлайн-оплата не подключена»: оплата ручная.
 */
export function PayPage() {
  const { token = '' } = useParams()
  const config = useAppConfig()
  const online = config.data?.online_payments === true
  const link = usePayLink(token, online)
  const start = useStartPay(token)

  if (config.isError) {
    return (
      <AuthLayout title={ru.pay.title}>
        <FormError>{errorMessage(config.error)}</FormError>
        <Button block onClick={() => void config.refetch()}>
          {ru.common.retry}
        </Button>
      </AuthLayout>
    )
  }
  if (config.data && !online) {
    return (
      <AuthLayout title={ru.pay.offlineTitle}>
        <div className={s.state}>
          <Info size={40} aria-hidden="true" className={s.stateIcon} />
          <p>{ru.pay.offlineText}</p>
        </div>
      </AuthLayout>
    )
  }

  if (config.isPending || link.isPending) {
    return (
      <AuthLayout title={ru.pay.title}>
        <div className={s.stack} aria-busy="true" aria-label={ru.pay.loading}>
          <Skeleton height={20} width="60%" />
          <Skeleton height={48} width="40%" />
          <Skeleton height={52} />
        </div>
      </AuthLayout>
    )
  }

  if (link.isError) {
    // 404 — ссылки нет или истекла (бэкенд отвечает одинаково).
    if (link.error.status === 404) {
      return (
        <AuthLayout title={ru.pay.expiredTitle}>
          <div className={s.state}>
            <Clock size={40} aria-hidden="true" className={s.stateIcon} />
            <p>{ru.pay.expiredText}</p>
          </div>
        </AuthLayout>
      )
    }
    return (
      <AuthLayout title={ru.pay.title}>
        <FormError>{errorMessage(link.error)}</FormError>
        <Button block onClick={() => void link.refetch()}>
          {ru.common.retry}
        </Button>
      </AuthLayout>
    )
  }

  const p = link.data
  if (p.status === 'paid') {
    return (
      <AuthLayout title={ru.pay.paidTitle}>
        <div className={s.state}>
          <CheckCircle2 size={48} aria-hidden="true" className={s.paidIcon} />
          <p className={s.center}>{p.center_name}</p>
          <p>
            {p.student_name}
            {' · '}
            <MoneyText tyiyn={p.amount} />
          </p>
          <p className={s.muted}>{ru.pay.paidText}</p>
        </div>
      </AuthLayout>
    )
  }

  const onPay = () =>
    start.mutate(undefined, {
      onSuccess: (r) => {
        if (r.pay_url) redirectTo(r.pay_url)
      },
      onError: (err) => {
        if (isApiError(err, 'already_paid')) void link.refetch()
      },
    })

  const startError = start.error
  const startMessage = startError ? (isApiError(startError, 'provider_unavailable') ? ru.pay.providerDown : errorMessage(startError)) : undefined

  return (
    <AuthLayout title={ru.pay.title} lead={p.center_name}>
      <dl className={s.summary}>
        <div>
          <dt>{ru.pay.student}</dt>
          <dd>{p.student_name}</dd>
        </div>
        <div>
          <dt>{ru.pay.amount}</dt>
          <dd>
            <MoneyText tyiyn={p.amount} className={s.amount} />
          </dd>
        </div>
      </dl>

      {p.provider === 'finik' ? (
        <div className={s.stack}>
          <Button size="lg" block loading={start.isPending} icon={<ExternalLink size={20} aria-hidden="true" />} onClick={onPay}>
            {start.isPending ? ru.pay.opening : ru.pay.payOnline}
          </Button>
          {start.data?.pay_url && (
            <a className={s.link} href={start.data.pay_url}>
              {ru.pay.openBank}
            </a>
          )}
          <FormError>{startMessage}</FormError>
          <Button variant="ghost" block loading={link.isFetching} onClick={() => void link.refetch()}>
            {ru.pay.refresh}
          </Button>
        </div>
      ) : (
        <div className={s.stack}>
          <h2 className={s.subtitle}>{ru.pay.manualTitle}</h2>
          <p className={s.muted}>{ru.pay.manualText}</p>
          {p.pay_details ? (
            <>
              <div className={s.details} aria-label={ru.pay.details}>
                {p.pay_details}
              </div>
              <CopyButton text={p.pay_details} label={ru.pay.copyDetails} variant="primary" block />
            </>
          ) : (
            <p>{ru.pay.noDetails}</p>
          )}
        </div>
      )}
      <p className={s.expires}>{ru.pay.expires(formatDateTime(p.expires_at))}</p>
    </AuthLayout>
  )
}
