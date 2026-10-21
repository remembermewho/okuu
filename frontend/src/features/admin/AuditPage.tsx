import { History } from 'lucide-react'
import { useSearchParams } from 'react-router'
import { useCenter } from '@/app/center'
import shared from '@/components/shared.module.css'
import { Button, Card, EmptyState, ErrorState, Input, PageHeader, Select, SkeletonList } from '@/components/ui'
import { useStaff } from '@/features/staff/api'
import { ru } from '@/i18n/ru'
import { formatDateTime } from '@/utils/date'
import { errorMessage } from '@/utils/errors'
import { useAudit, type AuditFilter, type AuditItem } from './api'
import s from './Audit.module.css'

const ISO = /^\d{4}-\d{2}-\d{2}$/

/** «ledger.payment» → «Оплата»; неизвестное действие — как есть. */
export function actionLabel(action: string): string {
  return ru.audit.actions[action] ?? action
}

export function AuditPage() {
  const c = useCenter()
  const [params, setParams] = useSearchParams()
  const staff = useStaff(c.id)
  const get = (k: string) => params.get(k) ?? ''
  const filter: AuditFilter = {
    ...(get('user') ? { user: get('user') } : {}),
    ...(get('action') ? { action: get('action') } : {}),
    ...(ISO.test(get('from')) ? { from: get('from') } : {}),
    ...(ISO.test(get('to')) ? { to: get('to') } : {}),
  }
  const audit = useAudit(c.id, filter)
  const items = audit.data?.pages.flatMap((p) => p.items) ?? []

  const set = (k: string, v: string) => {
    const next = new URLSearchParams(params)
    if (v) next.set(k, v)
    else next.delete(k)
    setParams(next, { replace: true })
  }

  return (
    <>
      <PageHeader title={ru.audit.title} />
      <div className={shared.filters} role="search">
        <Select
          label={ru.audit.who}
          value={filter.user ?? ''}
          onChange={(e) => set('user', e.target.value)}
          placeholder={ru.audit.allStaff}
          options={(staff.data ?? []).map((m) => ({ value: m.user_id, label: m.name }))}
        />
        <Select
          label={ru.audit.type}
          value={filter.action ?? ''}
          onChange={(e) => set('action', e.target.value)}
          placeholder={ru.audit.allTypes}
          options={Object.entries(ru.audit.types).map(([value, label]) => ({ value, label }))}
        />
        <Input label={ru.audit.from} type="date" value={filter.from ?? ''} max={filter.to} onChange={(e) => set('from', e.target.value)} />
        <Input label={ru.audit.to} type="date" value={filter.to ?? ''} min={filter.from} onChange={(e) => set('to', e.target.value)} />
      </div>

      {audit.isPending ? (
        <SkeletonList rows={6} height={56} />
      ) : audit.isError ? (
        <ErrorState message={errorMessage(audit.error)} onRetry={() => void audit.refetch()} />
      ) : items.length === 0 ? (
        <Card>
          <EmptyState icon={<History aria-hidden="true" />} title={ru.audit.emptyTitle} text={ru.audit.emptyText} />
        </Card>
      ) : (
        <Card flush>
          <ul className={shared.list} aria-label={ru.audit.title}>
            {items.map((it) => (
              <AuditRow key={it.id} item={it} />
            ))}
          </ul>
          {audit.hasNextPage && (
            <div className={shared.loadMore}>
              <Button variant="secondary" loading={audit.isFetchingNextPage} onClick={() => void audit.fetchNextPage()}>
                {ru.common.loadMore}
              </Button>
            </div>
          )}
        </Card>
      )}
    </>
  )
}

const json = (v: unknown) => JSON.stringify(v, null, 2)

function AuditRow({ item }: { item: AuditItem }) {
  const hasDetails = item.before != null || item.after != null
  return (
    <li className={s.item}>
      <div className={s.head}>
        <span className={s.action}>{actionLabel(item.action)}</span>
        <time className={s.time} dateTime={item.at}>
          {formatDateTime(item.at)}
        </time>
      </div>
      <span className={s.who}>{item.user ? item.user_name : ru.audit.system}</span>
      {hasDetails && (
        <details className={s.details}>
          <summary>{ru.audit.details}</summary>
          <div className={s.diff}>
            {item.before != null && (
              <div>
                <span className={s.diffLabel}>{ru.audit.before}</span>
                <pre>{json(item.before)}</pre>
              </div>
            )}
            {item.after != null && (
              <div>
                <span className={s.diffLabel}>{ru.audit.after}</span>
                <pre>{json(item.after)}</pre>
              </div>
            )}
          </div>
        </details>
      )}
    </li>
  )
}
