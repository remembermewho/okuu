import { BellRing, CheckCircle2, Send, Wallet } from 'lucide-react'
import { useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { centerPath, useCenter } from '@/app/center'
import { StudentStatusBadge, TgBadge } from '@/components/shared'
import shared from '@/components/shared.module.css'
import { Badge, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, MoneyText, PageHeader, Select, Skeleton, SkeletonList, useToast } from '@/components/ui'
import { PayDetailsBanner } from '@/features/admin/PayDetailsBanner'
import { groupOptions, useGroups } from '@/features/catalog/api'
import { OperationDialog } from '@/features/students/card/OperationDialog'
import { ru } from '@/i18n/ru'
import { cx } from '@/utils/cx'
import { formatDate, formatMonth } from '@/utils/date'
import { errorMessage } from '@/utils/errors'
import { formatSom } from '@/utils/money'
import { useDebtors, useReminders, type Debtor, type DebtorsStatus, type RemindersResult } from './api'
import s from './DebtorsPage.module.css'

/** API принимает до 500 учеников в одном «Напомнить». */
const MAX_REMIND = 500

export function DebtorsPage() {
  const c = useCenter()
  const [params, setParams] = useSearchParams()
  const tab: DebtorsStatus = params.get('tab') === 'pending' ? 'pending' : 'overdue'
  const [group, setGroup] = useState('')
  const groups = useGroups(c.id)
  // Обе вкладки грузим сразу — счётчики на вкладках учитывают фильтр по группе.
  const overdue = useDebtors(c.id, group, 'overdue')
  const pending = useDebtors(c.id, group, 'pending')
  const list = tab === 'pending' ? pending : overdue
  const remind = useReminders(c.id)
  const [confirmAll, setConfirmAll] = useState(false)
  const [result, setResult] = useState<RemindersResult>()
  const [pendingId, setPendingId] = useState<string>()
  const [paying, setPaying] = useState<Debtor>() // «Отметить оплату» — тот же диалог, что в карточке
  const toast = useToast()

  const names = new Map([...(overdue.data?.items ?? []), ...(pending.data?.items ?? [])].map((d) => [d.student_id, d.full_name]))

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
  // «Напомнить всем»: просроченные — все должники центра (all); ожидающие — ученики списка.
  const remindAll = () =>
    tab === 'overdue' && !group
      ? send({ all: true })
      : send({ student_ids: (list.data?.items ?? []).slice(0, MAX_REMIND).map((d) => d.student_id) })

  const setTab = (t: DebtorsStatus) => setParams(t === 'pending' ? { tab: 'pending' } : {}, { replace: true })
  const sum = overdue.data?.summary ?? pending.data?.summary
  const month = sum ? formatMonth(sum.period.slice(0, 7)) : ''
  const tabs: { id: DebtorsStatus; label: string; count?: number }[] = [
    { id: 'overdue', label: ru.debtors.tabOverdue, count: overdue.data?.items.length },
    { id: 'pending', label: ru.debtors.tabPending, count: pending.data?.items.length },
  ]

  return (
    <>
      <PageHeader
        title={ru.debtors.title}
        actions={
          (list.data?.items.length ?? 0) > 0 && (
            <Button icon={<Send size={18} aria-hidden="true" />} onClick={() => setConfirmAll(true)}>
              {ru.debtors.remindAll}
            </Button>
          )
        }
      />

      <PayDetailsBanner />

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
        <div className={s.tabs} role="tablist" aria-label={ru.debtors.tabs}>
          {tabs.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              id={`tab-${t.id}`}
              aria-selected={tab === t.id}
              aria-controls="debtors-panel"
              className={cx(s.tab, tab === t.id && s.tabOn)}
              onClick={() => setTab(t.id)}
            >
              {t.label}
              {t.count !== undefined && <span className={s.tabCount}>{t.count}</span>}
            </button>
          ))}
        </div>
        <Select label={ru.debtors.group} value={group} onChange={(e) => setGroup(e.target.value)} options={groupOptions(groups.data)} placeholder={ru.debtors.allGroups} />
      </div>

      <div id="debtors-panel" role="tabpanel" aria-labelledby={`tab-${tab}`}>
        {list.isPending ? (
          <SkeletonList rows={5} />
        ) : list.isError ? (
          <ErrorState message={errorMessage(list.error)} onRetry={() => void list.refetch()} />
        ) : list.data.items.length === 0 ? (
          <Card>
            {tab === 'overdue' ? (
              <EmptyState icon={<CheckCircle2 aria-hidden="true" />} title={group ? ru.debtors.emptyGroup : ru.debtors.emptyTitle} text={group ? undefined : ru.debtors.emptyText} />
            ) : (
              <EmptyState icon={<CheckCircle2 aria-hidden="true" />} title={group ? ru.debtors.pendingEmptyGroup : ru.debtors.pendingEmptyTitle} text={group ? undefined : ru.debtors.pendingEmptyText} />
            )}
          </Card>
        ) : (
          <Card flush>
            <ul className={shared.list} aria-label={tab === 'overdue' ? ru.debtors.tabOverdue : ru.debtors.tabPending}>
              {list.data.items.map((d) => (
                <li key={d.student_id} className={cx(shared.row, s.debtorRow)}>
                  <div className={shared.rowMain}>
                    <span className={shared.rowTitle}>
                      <Link to={centerPath(c.id, `students/${d.student_id}`)} className={s.name}>
                        {d.full_name}
                      </Link>
                      <StudentStatusBadge status={d.status} />
                    </span>
                    <span className={shared.rowMeta}>
                      {d.groups.map((g) => g.name).join(', ') || ru.students.noGroups}
                      {d.parent?.name ? ` · ${d.parent.name}` : ''}
                    </span>
                    <span className={s.badges}>
                      <TgBadge status={d.parent?.tg_status} short />
                      {d.reminded_today && (
                        <Badge tone="excused">
                          <BellRing size={12} aria-hidden="true" /> {ru.debtors.remindedToday}
                        </Badge>
                      )}
                    </span>
                  </div>
                  <div className={shared.rowSide}>
                    <MoneyText tyiyn={d.debt} tone={tab === 'overdue' ? 'debt' : undefined} className={shared.amount} />
                    {tab === 'overdue' ? (
                      <>
                        <span className={s.overdue}>{ru.debtors.overdue(d.days_overdue)}</span>
                        <span className={shared.rowMeta}>{ru.debtors.since(formatDate(d.oldest_due_date))}</span>
                      </>
                    ) : (
                      <>
                        <span className={s.dueIn}>{ru.debtors.dueIn(d.due_in_days)}</span>
                        <span className={shared.rowMeta}>{ru.debtors.dueOn(formatDate(d.oldest_due_date))}</span>
                      </>
                    )}
                  </div>
                  {/* Кнопки — отдельной полосой под строкой: на телефоне не теснят сумму. */}
                  <div className={s.rowActions}>
                    <Button icon={<Wallet size={16} aria-hidden="true" />} onClick={() => setPaying(d)} aria-label={`${ru.debtors.markPaid}: ${d.full_name}`}>
                      {ru.debtors.markPaid}
                    </Button>
                    <Button
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
      </div>

      {/* Сумма по умолчанию — текущий долг, способ — перевод; ключ идемпотентности — один на открытие. */}
      <OperationDialog
        kind={paying ? 'payment' : null}
        studentId={paying?.student_id ?? ''}
        balance={-(paying?.debt ?? 0)}
        defaultMethod="transfer"
        description={paying?.full_name}
        onDone={() =>
          toast({ message: paying?.parent?.tg_status === 'active' ? ru.debtors.paidNotified : ru.debtors.paidNoTelegram, tone: 'success' })
        }
        onClose={() => setPaying(undefined)}
      />

      <ConfirmDialog
        open={confirmAll}
        onOpenChange={setConfirmAll}
        title={tab === 'overdue' ? ru.debtors.remindAllConfirm : ru.debtors.remindPendingConfirm}
        text={tab === 'overdue' ? ru.debtors.remindAllText : ru.debtors.remindPendingText}
        confirmLabel={ru.debtors.remindAll}
        loading={remind.isPending && pendingId === undefined}
        onConfirm={remindAll}
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
