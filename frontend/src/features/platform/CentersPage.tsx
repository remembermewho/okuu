import { Building2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { Badge, Card, EmptyState, ErrorState, Input, PageHeader, Select, SkeletonList, type BadgeTone } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { formatDate, formatDateTime } from '@/utils/date'
import { errorMessage } from '@/utils/errors'
import { useAdminCenters, type AdminCenter, type CenterStatus } from './api'
import s from './Platform.module.css'

const STATUSES = ['', 'pending', 'active', 'blocked'] as const

export const statusTone: Record<string, BadgeTone> = { pending: 'excused', active: 'success', blocked: 'debt' }

/** /admin — таблица центров: поиск, фильтр по статусу; заявки сверху. Только агрегаты. */
export function CentersPage() {
  const [params, setParams] = useSearchParams()
  const status = (STATUSES as readonly string[]).includes(params.get('status') ?? '') ? ((params.get('status') ?? '') as CenterStatus | '') : ''
  const [q, setQ] = useState(params.get('q') ?? '')
  const [debounced, setDebounced] = useState(q)
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 300)
    return () => clearTimeout(t)
  }, [q])
  const centers = useAdminCenters({ q: debounced, status })

  const setStatus = (v: string) => {
    const next = new URLSearchParams(params)
    if (v) next.set('status', v)
    else next.delete('status')
    setParams(next, { replace: true })
  }

  return (
    <>
      <PageHeader title={ru.platform.centers} />
      <div className={s.filters}>
        <Input label={ru.platform.search} hint={ru.platform.searchHint} type="search" value={q} onChange={(e) => setQ(e.target.value)} />
        <Select
          label={ru.platform.statusLabel}
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          options={STATUSES.map((v) => ({ value: v, label: ru.platform.statusFilter[v] ?? v }))}
        />
      </div>
      {centers.isPending ? (
        <SkeletonList rows={4} />
      ) : centers.isError ? (
        <ErrorState message={errorMessage(centers.error)} onRetry={() => void centers.refetch()} />
      ) : centers.data.items.length === 0 ? (
        <EmptyState icon={<Building2 aria-hidden="true" />} title={ru.platform.empty} text={ru.platform.emptyText} />
      ) : (
        <>
          <p className={s.count} aria-live="polite">
            {ru.platform.found(centers.data.items.length)}
          </p>
          <Card flush>
            <table className={s.table}>
              <thead>
                <tr>
                  <th scope="col">{ru.platform.col.center}</th>
                  <th scope="col">{ru.platform.col.owner}</th>
                  <th scope="col">{ru.platform.col.plan}</th>
                  <th scope="col">{ru.platform.col.paidUntil}</th>
                  <th scope="col">{ru.platform.col.students}</th>
                  <th scope="col">{ru.platform.col.activity}</th>
                  <th scope="col">{ru.platform.col.created}</th>
                </tr>
              </thead>
              <tbody>
                {centers.data.items.map((c) => (
                  <CenterRow key={c.id} c={c} />
                ))}
              </tbody>
            </table>
          </Card>
        </>
      )}
    </>
  )
}

function CenterRow({ c }: { c: AdminCenter }) {
  return (
    <tr>
      <td>
        <Link to={`/admin/centers/${c.id}`} className={s.centerLink}>
          {c.name}
        </Link>
        <div className={s.badges}>
          <Badge tone={statusTone[c.status]}>{ru.platform.status[c.status]}</Badge>
          {c.is_demo && <Badge>{ru.platform.demo}</Badge>}
        </div>
      </td>
      <td data-label={ru.platform.col.owner}>
        <span>
          {c.owner.name}
          <br />
          <span className={`${s.muted} ${s.num}`}>{c.owner.phone}</span>
        </span>
      </td>
      <td data-label={ru.platform.col.plan}>{ru.platform.plans[c.plan] ?? c.plan}</td>
      <td data-label={ru.platform.col.paidUntil}>
        {c.paid_until ? (
          <span className={c.payment_overdue ? s.overdue : s.num}>
            {formatDate(c.paid_until)}
            {c.payment_overdue && ` · ${ru.platform.overdue}`}
          </span>
        ) : (
          <span className={s.muted}>{ru.platform.dash}</span>
        )}
      </td>
      <td data-label={ru.platform.col.students} className={s.num}>
        {c.active_students}
      </td>
      <td data-label={ru.platform.col.activity}>{c.last_activity ? formatDateTime(c.last_activity) : <span className={s.muted}>{ru.platform.never}</span>}</td>
      <td data-label={ru.platform.col.created} className={s.num}>
        {formatDate(c.created_at.slice(0, 10))}
      </td>
    </tr>
  )
}
