import { CheckCircle2, Send } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { centerPath, useCenter } from '@/app/center'
import { TgBadge } from '@/components/shared'
import shared from '@/components/shared.module.css'
import { Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, MoneyText, PageHeader, Select, Skeleton, SkeletonList } from '@/components/ui'
import { groupOptions, useGroups } from '@/features/catalog/api'
import { ru } from '@/i18n/ru'
import { formatDate, formatMonth } from '@/utils/date'
import { errorMessage } from '@/utils/errors'
import { formatSom } from '@/utils/money'
import { useDebtors, useReminders, type RemindersResult } from './api'
import s from './DebtorsPage.module.css'

export function DebtorsPage() {
  const c = useCenter()
  const [group, setGroup] = useState('')
  const groups = useGroups(c.id)
  const debtors = useDebtors(c.id, group)
  const remind = useReminders(c.id)
  const [confirmAll, setConfirmAll] = useState(false)
  const [result, setResult] = useState<RemindersResult>()
  const [pendingId, setPendingId] = useState<string>()

  const names = new Map((debtors.data?.items ?? []).map((d) => [d.student_id, d.full_name]))

  const send = (body: { student_ids?: string[]; all?: boolean }, id?: string) => {
    setPendingId(id)
    remind.mutate(body, {
      onSuccess: (r) => {
        setConfirmAll(false)
        setResult(r)
      },
      onSettled: () => setPendingId(undefined),
    })
  }

  const sum = debtors.data?.summary
  const month = sum ? formatMonth(sum.period.slice(0, 7)) : ''

  return (
    <>
      <PageHeader
        title={ru.debtors.title}
        actions={
          (debtors.data?.items.length ?? 0) > 0 && (
            <Button icon={<Send size={18} aria-hidden="true" />} onClick={() => setConfirmAll(true)}>
              {ru.debtors.remindAll}
            </Button>
          )
        }
      />

      <div className={s.summary}>
        {sum ? (
          <>
            <Card className={s.stat}>
              <span className={s.statLabel}>
                {ru.debtors.collected} {ru.debtors.monthOf(month)}
              </span>
              <MoneyText tyiyn={sum.collected} className={s.statValue} />
              <span className={s.statNote}>{ru.debtors.charged(formatSom(sum.charged))}</span>
            </Card>
            <Card className={`${s.stat} ${s.statDebt}`}>
              <span className={s.statLabel}>{ru.debtors.debt}</span>
              <MoneyText tyiyn={sum.debt_total} className={s.statValue} />
              <span className={s.statNote}>{ru.debtors.debtorsCount(sum.debtors_count)}</span>
            </Card>
            <Card className={s.stat}>
              <span className={s.statLabel}>{ru.debtors.pending}</span>
              <MoneyText tyiyn={sum.pending_total} className={s.statValue} />
              <span className={s.statNote}>{ru.debtors.pendingCount(sum.pending_count)}</span>
            </Card>
          </>
        ) : (
          [0, 1, 2].map((i) => <Skeleton key={i} height={112} />)
        )}
      </div>

      <div className={s.toolbar}>
        <Select label={ru.debtors.group} value={group} onChange={(e) => setGroup(e.target.value)} options={groupOptions(groups.data)} placeholder={ru.debtors.allGroups} />
      </div>

      {debtors.isPending ? (
        <SkeletonList rows={5} />
      ) : debtors.isError ? (
        <ErrorState message={errorMessage(debtors.error)} onRetry={() => void debtors.refetch()} />
      ) : debtors.data.items.length === 0 ? (
        <Card>
          <EmptyState icon={<CheckCircle2 aria-hidden="true" />} title={group ? ru.debtors.emptyGroup : ru.debtors.emptyTitle} text={group ? undefined : ru.debtors.emptyText} />
        </Card>
      ) : (
        <Card flush>
          <ul className={shared.list} aria-label={ru.debtors.title}>
            {debtors.data.items.map((d) => (
              <li key={d.student_id} className={shared.row}>
                <div className={shared.rowMain}>
                  <Link to={centerPath(c.id, `students/${d.student_id}`)} className={shared.rowTitle}>
                    {d.full_name}
                  </Link>
                  <span className={shared.rowMeta}>
                    {d.groups.map((g) => g.name).join(', ') || ru.students.noGroups}
                    {d.parent?.name ? ` · ${d.parent.name}` : ''}
                  </span>
                  <span>
                    <TgBadge status={d.parent?.tg_status} short />
                  </span>
                </div>
                <div className={shared.rowSide}>
                  <MoneyText tyiyn={d.debt} tone="debt" className={shared.amount} />
                  <span className={s.overdue}>{ru.debtors.overdue(d.days_overdue)}</span>
                  <span className={shared.rowMeta}>{ru.debtors.since(formatDate(d.oldest_due_date))}</span>
                  <Button
                    size="sm"
                    variant="secondary"
                    icon={<Send size={16} aria-hidden="true" />}
                    loading={pendingId === d.student_id}
                    disabled={remind.isPending}
                    onClick={() => send({ student_ids: [d.student_id] }, d.student_id)}
                    aria-label={`${ru.debtors.remind}: ${d.full_name}`}
                  >
                    {ru.debtors.remind}
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        </Card>
      )}

      <ConfirmDialog
        open={confirmAll}
        onOpenChange={setConfirmAll}
        title={ru.debtors.remindAllConfirm}
        text={ru.debtors.remindAllText}
        confirmLabel={ru.debtors.remindAll}
        loading={remind.isPending && pendingId === undefined}
        onConfirm={() => send({ all: true })}
      />

      <Dialog
        open={result !== undefined}
        onOpenChange={(o) => !o && setResult(undefined)}
        title={ru.debtors.resultTitle}
        footer={<Button onClick={() => setResult(undefined)}>{ru.common.close}</Button>}
      >
        {result && (
          <>
            <p className={s.sent}>
              {result.sent > 0 && <CheckCircle2 size={20} aria-hidden="true" />}
              {result.sent > 0 ? ru.debtors.resultSent(result.sent, result.messages) : ru.debtors.resultNone}
            </p>
            {result.skipped.length > 0 && (
              <>
                <h3 className={s.skippedTitle}>{ru.debtors.resultSkipped}</h3>
                <ul className={s.skipped}>
                  {result.skipped.map((sk) => (
                    <li key={sk.student_id}>
                      <strong>{names.get(sk.student_id) ?? ru.common.none}</strong>
                      <span className={shared.rowMeta}>{ru.debtors.reasons[sk.reason] ?? sk.reason}</span>
                    </li>
                  ))}
                </ul>
              </>
            )}
          </>
        )}
      </Dialog>
    </>
  )
}
