import { History } from 'lucide-react'
import { Fragment, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { centerPath, useCenter } from '@/app/center'
import { can } from '@/app/permissions'
import shared from '@/components/shared.module.css'
import { Button, Card, EmptyState, ErrorState, Input, PageHeader, Select, SkeletonList } from '@/components/ui'
import { useStaff } from '@/features/staff/api'
import { ru } from '@/i18n/ru'
import { formatDate, formatDateTime } from '@/utils/date'
import { errorMessage } from '@/utils/errors'
import { formatSom } from '@/utils/money'
import { useAudit, type AuditFilter, type AuditItem } from './api'
import s from './Audit.module.css'
import { auditFields } from './auditDetails'

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

/** Ученики записи — ссылками на карточку; у ledger.* — сумма, у отмены — вид отменённой операции. */
function AuditSubject({ item }: { item: AuditItem }) {
  const c = useCenter()
  if (item.students.length === 0 && !item.lesson) return null
  let amount = item.amount != null ? formatSom(item.amount) : null
  const before = item.before as { kind?: string } | null
  if (amount && item.action === 'ledger.cancel' && before?.kind) amount = ru.audit.cancelledOp(ru.audit.values.kind?.[before.kind] ?? before.kind, amount)
  return (
    <span className={s.subject} data-testid="audit-subject">
      {item.lesson && <span>{ru.audit.lessonLine(item.lesson.group_name, formatDate(item.lesson.date))}</span>}
      {item.students.length > 0 && (
        <span>
          {item.students.map((st, i) => (
            <Fragment key={st.id}>
              {i > 0 && ru.audit.sep}
              <Link to={centerPath(c.id, `students/${st.id}`)}>{st.full_name}</Link>
            </Fragment>
          ))}
        </span>
      )}
      {amount && <span className={s.amount}>{amount}</span>}
    </span>
  )
}

function AuditDetails({ item }: { item: AuditItem }) {
  const c = useCenter()
  const [raw, setRaw] = useState(false)
  const fields = auditFields(item)
  const diff = item.before != null && item.after != null
  return (
    <details className={s.details}>
      <summary>{ru.audit.details}</summary>
      {fields.length === 0 ? (
        <p className={s.none}>{ru.audit.noChanges}</p>
      ) : (
        <table className={s.fields}>
          <thead>
            <tr>
              <th scope="col">{ru.audit.field}</th>
              {diff ? (
                <>
                  <th scope="col">{ru.audit.before}</th>
                  <th scope="col">{ru.audit.after}</th>
                </>
              ) : (
                <th scope="col">{ru.audit.value}</th>
              )}
            </tr>
          </thead>
          <tbody>
            {fields.map((f) => (
              <tr key={f.key}>
                <th scope="row">{f.label}</th>
                {diff ? (
                  <>
                    <td>{f.before ?? ru.audit.empty}</td>
                    <td>{f.after ?? ru.audit.empty}</td>
                  </>
                ) : (
                  <td>{f.after ?? f.before}</td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {can.auditRaw(c.role) && (
        <>
          <Button variant="ghost" size="sm" aria-expanded={raw} onClick={() => setRaw((v) => !v)}>
            {ru.audit.techData}
          </Button>
          {raw && (
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
          )}
        </>
      )}
    </details>
  )
}

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
      <AuditSubject item={item} />
      <span className={s.who}>{item.user ? item.user_name : ru.audit.system}</span>
      {hasDetails && <AuditDetails item={item} />}
    </li>
  )
}
