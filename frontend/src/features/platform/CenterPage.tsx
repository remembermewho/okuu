import { ChevronLeft, KeyRound, Lock, LockOpen, ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { useParams } from 'react-router'
import { ShareLinkDialog } from '@/components/ShareLinkDialog'
import { Badge, Button, ButtonLink, Card, CardHeader, ConfirmDialog, ErrorState, FormError, Input, PageHeader, Select, SkeletonList, useToast } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { formatDate, formatDateTime } from '@/utils/date'
import { errorMessage, isApiError } from '@/utils/errors'
import { useAdminCenter, useApproveCenter, useBlockCenter, useOwnerPasswordReset, usePatchAdminCenter, useUnblockCenter, type AdminCenterCard, type Plan } from './api'
import { statusTone } from './CentersPage'
import s from './Platform.module.css'

const PLANS: Plan[] = ['free', 'start', 'center', 'network', 'founder']

/** /admin/centers/:id — карточка центра: агрегаты, действия, тариф, журнал платформы по центру. */
export function CenterPage() {
  const { id = '' } = useParams()
  const center = useAdminCenter(id)
  const back = (
    <ButtonLink to="/admin" variant="ghost" icon={<ChevronLeft size={18} aria-hidden="true" />}>
      {ru.platform.back}
    </ButtonLink>
  )
  if (center.isPending) return <SkeletonList rows={4} />
  if (center.isError) {
    return (
      <>
        {back}
        <ErrorState message={errorMessage(center.error)} onRetry={isApiError(center.error, 'not_found') ? undefined : () => void center.refetch()} />
      </>
    )
  }
  const c = center.data
  return (
    <>
      {back}
      <PageHeader
        title={c.name}
        subtitle={
          <span className={s.badges}>
            <Badge tone={statusTone[c.status]}>{ru.platform.status[c.status]}</Badge>
            {c.is_demo && <Badge>{ru.platform.demo}</Badge>}
          </span>
        }
      />
      <div className={s.grid}>
        <Facts c={c} />
        <Actions c={c} />
        <PlanForm key={`${c.plan}-${c.paid_until}`} c={c} />
        <Audit c={c} />
      </div>
    </>
  )
}

function Facts({ c }: { c: AdminCenterCard }) {
  return (
    <Card>
      <CardHeader title={ru.platform.about} />
      <dl className={s.facts}>
        <dt>{ru.platform.owner}</dt>
        <dd>
          {c.owner.name}
          <br />
          <span className={s.num}>{c.owner.phone}</span>
        </dd>
        <dt>{ru.platform.students}</dt>
        <dd className={s.num}>{ru.platform.studentsLine(c.students.active, c.students.paused, c.students.left)}</dd>
        <dt>{ru.platform.groups}</dt>
        <dd className={s.num}>{c.groups}</dd>
        <dt>{ru.platform.staff}</dt>
        <dd className={s.num}>{c.staff}</dd>
        <dt>{ru.platform.telegram}</dt>
        <dd className={s.num}>{c.telegram_parents}</dd>
        <dt>{ru.platform.activity}</dt>
        <dd>{c.last_activity ? formatDateTime(c.last_activity) : ru.platform.never}</dd>
        <dt>{ru.platform.created}</dt>
        <dd>{formatDate(c.created_at.slice(0, 10))}</dd>
        <dt>{ru.platform.timezone}</dt>
        <dd>{c.timezone}</dd>
        {c.blocked_reason && (
          <>
            <dt>{ru.platform.blockedReason}</dt>
            <dd>{c.blocked_reason}</dd>
          </>
        )}
      </dl>
      <p className={s.note}>{ru.platform.privacyNote}</p>
    </Card>
  )
}

function Actions({ c }: { c: AdminCenterCard }) {
  const toast = useToast()
  const approve = useApproveCenter(c.id)
  const unblock = useUnblockCenter(c.id)
  const reset = useOwnerPasswordReset(c.id)
  const [blocking, setBlocking] = useState(false)
  const [link, setLink] = useState<{ url: string; name: string }>()
  return (
    <Card>
      <CardHeader title={ru.platform.actions} />
      <div className={s.actions}>
        {c.status === 'pending' && (
          <Button icon={<ShieldCheck size={18} aria-hidden="true" />} loading={approve.isPending} onClick={() => approve.mutate(undefined, { onSuccess: () => toast({ message: ru.platform.approved, tone: 'success' }) })}>
            {ru.platform.approve}
          </Button>
        )}
        {c.status === 'blocked' ? (
          <Button icon={<LockOpen size={18} aria-hidden="true" />} loading={unblock.isPending} onClick={() => unblock.mutate(undefined, { onSuccess: () => toast({ message: ru.platform.unblocked, tone: 'success' }) })}>
            {ru.platform.unblock}
          </Button>
        ) : (
          <Button variant="danger" icon={<Lock size={18} aria-hidden="true" />} onClick={() => setBlocking(true)}>
            {ru.platform.block}
          </Button>
        )}
        <Button
          variant="secondary"
          icon={<KeyRound size={18} aria-hidden="true" />}
          loading={reset.isPending}
          onClick={() => reset.mutate(undefined, { onSuccess: (r) => setLink({ url: r.url, name: r.user_name }) })}
        >
          {ru.platform.ownerReset}
        </Button>
      </div>
      {blocking && <BlockDialog id={c.id} onClose={() => setBlocking(false)} />}
      {link && (
        <ShareLinkDialog
          title={ru.platform.ownerResetTitle}
          lead={ru.platform.ownerResetLead(link.name)}
          url={link.url}
          whatsappText={ru.platform.ownerResetWa(link.url)}
          onClose={() => setLink(undefined)}
        />
      )}
    </Card>
  )
}

function BlockDialog({ id, onClose }: { id: string; onClose: () => void }) {
  const toast = useToast()
  const block = useBlockCenter(id)
  const [reason, setReason] = useState('')
  const [error, setError] = useState<string>()
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={ru.platform.blockTitle}
      text={ru.platform.blockText}
      confirmLabel={ru.platform.block}
      danger
      loading={block.isPending}
      onConfirm={() => {
        if (!reason.trim()) {
          setError(ru.errors.required)
          return
        }
        block.mutate(reason.trim(), {
          onSuccess: () => {
            toast({ message: ru.platform.blocked, tone: 'success' })
            onClose()
          },
          onError: (err) => setError(errorMessage(err)),
        })
      }}
    >
      <Input label={ru.platform.reason} hint={ru.platform.reasonHint} value={reason} error={error} maxLength={500} onChange={(e) => setReason(e.target.value)} />
    </ConfirmDialog>
  )
}

function PlanForm({ c }: { c: AdminCenterCard }) {
  const toast = useToast()
  const patch = usePatchAdminCenter(c.id)
  const [plan, setPlan] = useState<Plan>(c.plan)
  const [paidUntil, setPaidUntil] = useState(c.paid_until ?? '')
  const [formError, setFormError] = useState<string>()
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  return (
    <Card>
      <CardHeader title={ru.platform.planTitle} />
      <form
        className={s.form}
        noValidate
        onSubmit={(e) => {
          e.preventDefault()
          setFormError(undefined)
          setFieldErrors({})
          patch.mutate(
            { plan, paid_until: paidUntil || null },
            {
              onSuccess: () => toast({ message: ru.platform.saved, tone: 'success' }),
              onError: (err) => {
                if (err.field === 'plan' || err.field === 'paid_until') setFieldErrors({ [err.field]: errorMessage(err) })
                else setFormError(errorMessage(err))
              },
            },
          )
        }}
      >
        <Select label={ru.platform.plan} value={plan} error={fieldErrors.plan} onChange={(e) => setPlan(e.target.value as Plan)} options={PLANS.map((p) => ({ value: p, label: ru.platform.plans[p] ?? p }))} />
        <Input
          label={ru.platform.paidUntil}
          type="date"
          value={paidUntil}
          optional
          hint={c.payment_overdue ? `${ru.platform.overdue}. ${ru.platform.paidUntilHint}` : ru.platform.paidUntilHint}
          error={fieldErrors.paid_until}
          onChange={(e) => setPaidUntil(e.target.value)}
        />
        <FormError>{formError}</FormError>
        <div>
          <Button type="submit" loading={patch.isPending}>
            {ru.platform.save}
          </Button>
        </div>
      </form>
    </Card>
  )
}

function Audit({ c }: { c: AdminCenterCard }) {
  return (
    <Card className={s.wide}>
      <CardHeader title={ru.platform.auditTitle} />
      {c.audit.length === 0 ? (
        <p className={s.muted}>{ru.platform.auditEmpty}</p>
      ) : (
        <ul className={s.audit}>
          {c.audit.map((a) => (
            <li key={a.id}>
              <div>
                {ru.platform.actionNames[a.action] ?? a.action}
                {a.action === 'center.block' && a.details && typeof a.details.reason === 'string' && `: ${a.details.reason}`}
              </div>
              <div className={s.auditMeta}>
                {formatDateTime(a.at)}
                {' · '}
                {a.admin_name ?? ru.platform.console}
              </div>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}
