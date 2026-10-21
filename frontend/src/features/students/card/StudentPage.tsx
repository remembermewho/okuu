import * as Menu from '@radix-ui/react-dropdown-menu'
import { Bell, ChevronLeft, MoreVertical, Plus, Send, Wallet } from 'lucide-react'
import { useState } from 'react'
import { Link, useParams } from 'react-router'
import type { Schemas } from '@/api/client'
import { centerPath, useCenter } from '@/app/center'
import menu from '@/app/layout.module.css'
import { can } from '@/app/permissions'
import { MonthSwitcher, StudentStatusBadge, TgBadge } from '@/components/shared'
import { Badge, Button, Card, CardHeader, ConfirmDialog, EmptyState, ErrorState, Input, MoneyText, PageHeader, Skeleton, SkeletonList, layout, useToast } from '@/components/ui'
import { useLedger, useReminders } from '@/features/money/api'
import { ru } from '@/i18n/ru'
import { formatDate, formatDateTime, formatDayMonth, formatMonth } from '@/utils/date'
import { errorMessage } from '@/utils/errors'
import { formatSom } from '@/utils/money'
import { formatPhone } from '@/utils/phone'
import { useEndEnrollment, useParentInvite, usePatchStudent, useRenewEnrollment, useStudent, useStudentAttendance, type Enrollment, type Student } from '../api'
import { CancelOperationDialog, EnrollDialog, InviteParentDialog } from './dialogs'
import { OperationDialog, type OperationKind } from './OperationDialog'
import s from './StudentPage.module.css'

export function StudentPage() {
  const { studentId = '' } = useParams()
  const c = useCenter()
  const student = useStudent(c.id, studentId)
  const [op, setOp] = useState<OperationKind | null>(null)

  const back = (
    <Link to={centerPath(c.id, 'students')} className={s.back}>
      <ChevronLeft size={18} aria-hidden="true" />
      {ru.student.back}
    </Link>
  )

  if (student.isPending) {
    return (
      <>
        {back}
        <Skeleton height={32} width="60%" />
        <div className={`${s.grid} ${s.gridTop}`}>
          <SkeletonList rows={3} height={140} />
          <SkeletonList rows={2} height={200} />
        </div>
      </>
    )
  }
  if (student.isError) {
    return (
      <>
        {back}
        <ErrorState message={errorMessage(student.error)} onRetry={() => void student.refetch()} />
      </>
    )
  }
  const st = student.data

  return (
    <>
      {back}
      <PageHeader
        title={
          <span className={layout.row}>
            {st.full_name}
            <StudentStatusBadge status={st.status} />
          </span>
        }
        subtitle={[st.birth_date && ru.student.birth(formatDate(st.birth_date)), ru.student.created(formatDate(st.created_at.slice(0, 10)))].filter(Boolean).join(' · ')}
        actions={<StudentMenu student={st} onOperation={setOp} />}
      />
      <div className={s.grid}>
        <div className={s.col}>
          <BalanceCard student={st} onPay={() => setOp('payment')} />
          <ParentCard student={st} />
          <EnrollmentsCard student={st} />
        </div>
        <div className={s.col}>
          <HistoryCard studentId={st.id} />
          <AttendanceCard studentId={st.id} />
        </div>
      </div>
      <OperationDialog kind={op} studentId={st.id} balance={st.balance} onClose={() => setOp(null)} />
    </>
  )
}

function StudentMenu({ student, onOperation }: { student: Student; onOperation: (k: OperationKind) => void }) {
  const c = useCenter()
  const patch = usePatchStudent(c.id, student.id)
  const toast = useToast()
  const [confirmLeft, setConfirmLeft] = useState(false)
  const setStatus = (status: Schemas['StudentStatus']) =>
    patch.mutate({ status }, { onSuccess: () => (setConfirmLeft(false), toast({ message: ru.student.statusChanged, tone: 'success' })) })

  return (
    <>
      <Menu.Root>
        <Menu.Trigger asChild>
          <Button variant="secondary" icon={<MoreVertical size={18} aria-hidden="true" />}>
            {ru.student.more}
          </Button>
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Content className={menu.menu} align="end" sideOffset={6}>
            <Menu.Item className={menu.menuItem} onSelect={() => onOperation('refund')}>
              {ru.student.refund}
            </Menu.Item>
            <Menu.Item className={menu.menuItem} onSelect={() => onOperation('correction')}>
              {ru.student.correction}
            </Menu.Item>
            <Menu.Separator className={layout.muted} />
            {student.status !== 'active' && (
              <Menu.Item className={menu.menuItem} onSelect={() => setStatus('active')}>
                {ru.student.statusActive}
              </Menu.Item>
            )}
            {student.status === 'active' && (
              <Menu.Item className={menu.menuItem} onSelect={() => setStatus('paused')}>
                {ru.student.statusPause}
              </Menu.Item>
            )}
            {student.status !== 'left' && (
              <Menu.Item className={menu.menuItem} onSelect={() => setConfirmLeft(true)}>
                {ru.student.statusLeft}
              </Menu.Item>
            )}
          </Menu.Content>
        </Menu.Portal>
      </Menu.Root>
      <ConfirmDialog
        open={confirmLeft}
        onOpenChange={setConfirmLeft}
        title={ru.student.leftConfirm}
        text={ru.student.leftText}
        confirmLabel={ru.student.statusLeft}
        danger
        loading={patch.isPending}
        onConfirm={() => setStatus('left')}
      />
    </>
  )
}

function BalanceCard({ student, onPay }: { student: Student; onPay: () => void }) {
  const b = student.balance
  const caption = b < 0 ? ru.student.balanceDebt : b > 0 ? ru.student.balanceAdvance : ru.student.balanceZero
  const d = student.discount
  return (
    <Card className={`${s.balance} ${b < 0 ? s.balanceDebt : ''}`}>
      <span className={layout.muted}>{caption}</span>
      <MoneyText tyiyn={Math.abs(b)} tone={b < 0 ? 'debt' : undefined} className={s.balanceValue} />
      {d && <span className={layout.muted}>{ru.student.discount(d.kind === 'percent' ? `${d.value}%` : formatSom(d.value))}</span>}
      <div className={s.balanceActions}>
        <Button size="lg" icon={<Wallet size={20} aria-hidden="true" />} onClick={onPay}>
          {ru.student.pay}
        </Button>
      </div>
    </Card>
  )
}

function ParentCard({ student }: { student: Student }) {
  const c = useCenter()
  const invite = useParentInvite(c.id)
  const remind = useReminders(c.id)
  const toast = useToast()
  const p = student.parent
  const sendRemind = () =>
    remind.mutate(
      { student_ids: [student.id] },
      {
        onSuccess: (r) => {
          const sk = r.skipped[0]
          if (r.sent > 0 || !sk) toast({ message: ru.student.remindSent, tone: 'success' })
          else toast({ message: ru.student.remindSkipped(ru.debtors.reasons[sk.reason] ?? sk.reason), tone: 'error' })
        },
      },
    )
  return (
    <Card>
      <CardHeader title={ru.student.parent} />
      {!p ? (
        <p className={layout.muted}>{ru.student.noParent}</p>
      ) : (
        <>
          <div className={s.parentLine}>
            <strong>{p.name}</strong>
            <a className={s.phone} href={`tel:${p.phone}`}>
              {formatPhone(p.phone)}
            </a>
            <span>
              <TgBadge status={p.tg_status} />
            </span>
            {p.last_reminder_at && <span className={layout.muted}>{ru.student.lastReminder(formatDateTime(p.last_reminder_at))}</span>}
          </div>
          <div className={s.actions}>
            {student.balance < 0 && p.tg_status === 'active' && (
              <Button icon={<Bell size={18} aria-hidden="true" />} loading={remind.isPending} onClick={sendRemind}>
                {ru.student.remind}
              </Button>
            )}
            <Button variant="secondary" icon={<Send size={18} aria-hidden="true" />} loading={invite.isPending} onClick={() => invite.mutate(p.id)}>
              {p.tg_status === 'active' ? ru.student.reinviteParent : ru.student.inviteParent}
            </Button>
          </div>
          <InviteParentDialog invite={invite.data ?? null} onClose={() => invite.reset()} />
        </>
      )}
    </Card>
  )
}

function EnrollmentsCard({ student }: { student: Student }) {
  const c = useCenter()
  const [adding, setAdding] = useState(false)
  const [ending, setEnding] = useState<Enrollment | null>(null)
  const [renewing, setRenewing] = useState<Enrollment | null>(null)
  const [endDate, setEndDate] = useState(c.today)
  const end = useEndEnrollment(c.id, student.id)
  const renew = useRenewEnrollment(c.id, student.id)
  const toast = useToast()
  const enrollments = [...student.enrollments].sort((a, b) => Number(b.active) - Number(a.active))

  return (
    <Card>
      <CardHeader
        title={ru.student.enrollments}
        actions={
          student.status !== 'left' && (
            <Button size="sm" variant="ghost" icon={<Plus size={16} aria-hidden="true" />} onClick={() => setAdding(true)}>
              {ru.student.addEnrollment}
            </Button>
          )
        }
      />
      {enrollments.length === 0 ? (
        <p className={layout.muted}>{ru.student.enrollmentsEmpty}</p>
      ) : (
        enrollments.map((e) => (
          <div key={e.id} className={`${s.enrollment} ${e.active ? '' : s.enrollmentEnded}`}>
            <div className={s.enrollmentHead}>
              <span>{e.group.name}</span>
              {!e.active && <Badge>{ru.student.ended}</Badge>}
            </div>
            <span className={layout.muted}>
              {e.tariff.name} · {ru.student.tariffKinds[e.tariff.kind]} · {formatSom(e.tariff.price)}
            </span>
            <span className={layout.muted}>
              {ru.student.since(formatDate(e.start_date))}
              {e.end_date ? ` ${ru.student.until(formatDate(e.end_date))}` : ''}
              {e.billed_through && e.active ? ` · ${ru.student.billedThrough(formatMonth(e.billed_through.slice(0, 7)))}` : ''}
            </span>
            {e.lessons_left !== null && <span className={e.lessons_left <= 1 ? s.warn : layout.muted}>{ru.student.lessonsLeft(e.lessons_left)}</span>}
            {e.active && (
              <div className={s.actions}>
                {e.tariff.kind === 'package' && (
                  <Button size="sm" variant="secondary" onClick={() => setRenewing(e)}>
                    {ru.student.renew}
                  </Button>
                )}
                <Button size="sm" variant="ghost" onClick={() => (setEndDate(c.today), setEnding(e))}>
                  {ru.student.end}
                </Button>
              </div>
            )}
          </div>
        ))
      )}

      <EnrollDialog studentId={student.id} open={adding} onClose={() => setAdding(false)} />
      <ConfirmDialog
        open={ending !== null}
        onOpenChange={(o) => !o && setEnding(null)}
        title={ru.student.endTitle}
        text={ru.student.endText}
        confirmLabel={ru.student.end}
        danger
        loading={end.isPending}
        onConfirm={() =>
          ending &&
          end.mutate(
            { enrollmentId: ending.id, endDate },
            { onSuccess: () => (setEnding(null), toast({ message: ru.student.endDone, tone: 'success' })), onError: (err) => toast({ message: errorMessage(err), tone: 'error' }) },
          )
        }
      >
        <Input label={ru.student.endDate} type="date" value={endDate} onChange={(ev) => setEndDate(ev.target.value)} />
      </ConfirmDialog>
      <ConfirmDialog
        open={renewing !== null}
        onOpenChange={(o) => !o && setRenewing(null)}
        title={ru.student.renewTitle}
        text={renewing ? ru.student.renewText(formatSom(renewing.tariff.price), null) : undefined}
        confirmLabel={ru.student.renew}
        loading={renew.isPending}
        onConfirm={() => renewing && renew.mutate(renewing.id, { onSuccess: () => (setRenewing(null), toast({ message: ru.student.renewDone, tone: 'success' })) })}
      />
    </Card>
  )
}

function HistoryCard({ studentId }: { studentId: string }) {
  const c = useCenter()
  const ledger = useLedger(c.id, studentId)
  const [cancelId, setCancelId] = useState<string | null>(null)
  const ops = ledger.data?.pages.flatMap((p) => p.items) ?? []

  return (
    <Card>
      <CardHeader title={ru.student.history} />
      {ledger.isPending ? (
        <SkeletonList rows={4} height={48} />
      ) : ledger.isError ? (
        <ErrorState message={errorMessage(ledger.error)} onRetry={() => void ledger.refetch()} />
      ) : ops.length === 0 ? (
        <EmptyState title={ru.student.historyEmpty} />
      ) : (
        <>
          <ul className={s.ops}>
            {ops.map((o) => {
              // R15: отменить можно любую операцию; оплату из Finik — только владелец.
              const canCancel = !o.cancelled && (o.method !== 'finik' || can.cancelFinik(c.role))
              return (
                <li key={o.id} className={`${s.op} ${o.cancelled ? s.opCancelled : ''}`}>
                  <div className={s.opMain}>
                    <strong>
                      {ru.student.kinds[o.kind]}
                      {o.method ? ` · ${ru.student.methods[o.method]}` : ''}
                    </strong>
                    <span className={layout.muted}>
                      {formatDateTime(o.created_at)}
                      {o.period ? ` · ${ru.student.period(formatMonth(o.period.slice(0, 7)))}` : ''}
                      {o.kind === 'charge' && o.due_date ? ` · ${ru.student.due(formatDayMonth(o.due_date))}` : ''}
                    </span>
                    {o.comment && <span className={layout.muted}>{o.comment}</span>}
                    {o.cancelled && <span className={s.warn}>{ru.student.cancelledReason(o.cancel_reason ?? '')}</span>}
                  </div>
                  <div className={s.opSide}>
                    <MoneyText tyiyn={o.amount} tone="balance" sign className={s.opAmount} />
                    {canCancel && (
                      <Button size="sm" variant="ghost" onClick={() => setCancelId(o.id)} aria-label={`${ru.student.cancel}: ${ru.student.kinds[o.kind]} ${formatSom(o.amount)}`}>
                        {ru.student.cancel}
                      </Button>
                    )}
                  </div>
                </li>
              )
            })}
          </ul>
          {ledger.hasNextPage && (
            <Button variant="secondary" block loading={ledger.isFetchingNextPage} onClick={() => void ledger.fetchNextPage()}>
              {ru.common.loadMore}
            </Button>
          )}
        </>
      )}
      <CancelOperationDialog ledgerId={cancelId} onClose={() => setCancelId(null)} />
    </Card>
  )
}

function AttendanceCard({ studentId }: { studentId: string }) {
  const c = useCenter()
  const thisMonth = c.today.slice(0, 7)
  const [month, setMonth] = useState(thisMonth)
  const att = useStudentAttendance(c.id, studentId, month)
  const items = att.data ?? []
  const count = (st: string) => items.filter((i) => i.status === st).length
  const tone = { present: 'success', absent: 'debt', excused: 'excused' } as const

  return (
    <Card>
      <CardHeader title={ru.student.attendance} actions={<MonthSwitcher month={month} onChange={setMonth} max={thisMonth} />} />
      {att.isPending ? (
        <SkeletonList rows={3} height={40} />
      ) : att.isError ? (
        <ErrorState message={errorMessage(att.error)} onRetry={() => void att.refetch()} />
      ) : items.length === 0 ? (
        <p className={layout.muted}>{ru.student.attendanceEmpty}</p>
      ) : (
        <>
          <p className={layout.muted}>{ru.student.attendanceSummary(count('present'), count('absent'), count('excused'))}</p>
          {items.map((i) => (
            <div key={i.lesson_id} className={s.mark}>
              <span>
                {formatDayMonth(i.date)}
                {', '}
                {i.start_time} · {i.group.name}
                {i.deducted && <span className={layout.muted}>{` · ${ru.student.deducted}`}</span>}
              </span>
              <Badge tone={tone[i.status]}>{ru.student.marks[i.status]}</Badge>
            </div>
          ))}
        </>
      )}
    </Card>
  )
}
