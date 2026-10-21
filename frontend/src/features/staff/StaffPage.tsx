import * as Menu from '@radix-ui/react-dropdown-menu'
import { MessageCircle, MoreVertical, UserPlus } from 'lucide-react'
import { useState } from 'react'
import type { Schemas } from '@/api/client'
import { useCenter } from '@/app/center'
import menu from '@/app/layout.module.css'
import { can } from '@/app/permissions'
import { CopyButton } from '@/components/shared'
import shared from '@/components/shared.module.css'
import { Badge, Button, ButtonAnchor, Card, CardHeader, ConfirmDialog, Dialog, EmptyState, ErrorState, FormError, Input, PageHeader, RadioGroup, SkeletonList, layout, useToast } from '@/components/ui'
import { useMe } from '@/features/auth/api'
import { ru } from '@/i18n/ru'
import { formatDateTime } from '@/utils/date'
import { errorMessage } from '@/utils/errors'
import { formatPhone } from '@/utils/phone'
import { useChangeRole, useCreateInvite, useInvites, useRemoveStaff, useRevokeInvite, useStaff, type Invite, type StaffMember, type StaffRole } from './api'
import s from './Staff.module.css'

const ROLES: StaffRole[] = ['admin', 'teacher']
const roleTone = { owner: 'success', admin: 'excused', teacher: 'neutral' } as const

export function StaffPage() {
  const c = useCenter()
  const owner = can.manageStaff(c.role)
  const me = useMe()
  const staff = useStaff(c.id)
  const invites = useInvites(c.id, owner)
  const changeRole = useChangeRole(c.id)
  const remove = useRemoveStaff(c.id)
  const revoke = useRevokeInvite(c.id)
  const toast = useToast()
  const [inviting, setInviting] = useState(false)
  const [removing, setRemoving] = useState<StaffMember | null>(null)
  const [revoking, setRevoking] = useState<Invite | null>(null)

  return (
    <>
      <PageHeader
        title={ru.staff.title}
        subtitle={owner ? undefined : ru.staff.readOnly}
        actions={
          owner && (
            <Button icon={<UserPlus size={18} aria-hidden="true" />} onClick={() => setInviting(true)}>
              {ru.staff.invite}
            </Button>
          )
        }
      />
      <div className={layout.stack}>
        {staff.isPending ? (
          <SkeletonList rows={3} />
        ) : staff.isError ? (
          <ErrorState message={errorMessage(staff.error)} onRetry={() => void staff.refetch()} />
        ) : (
          <Card flush>
            <ul className={shared.list} aria-label={ru.staff.title}>
              {staff.data.map((m) => (
                <li key={m.user_id} className={s.row}>
                  <div className={shared.rowMain}>
                    <span className={shared.rowTitle}>
                      {m.name}
                      {m.user_id === me.data?.id && <span className={layout.muted}>{`(${ru.staff.you})`}</span>}
                    </span>
                    <span className={shared.rowMeta}>{formatPhone(m.phone)}</span>
                  </div>
                  <Badge tone={roleTone[m.role]}>{ru.roles[m.role]}</Badge>
                  {owner && m.role !== 'owner' && (
                    <Menu.Root>
                      <Menu.Trigger asChild>
                        <Button variant="ghost" iconOnly aria-label={ru.staff.menu(m.name)}>
                          <MoreVertical size={20} aria-hidden="true" />
                        </Button>
                      </Menu.Trigger>
                      <Menu.Portal>
                        <Menu.Content className={menu.menu} align="end" sideOffset={6}>
                          {ROLES.filter((r) => r !== m.role).map((r) => (
                            <Menu.Item
                              key={r}
                              className={menu.menuItem}
                              onSelect={() =>
                                changeRole.mutate({ userId: m.user_id, role: r }, { onSuccess: () => toast({ message: ru.staff.roleChanged, tone: 'success' }) })
                              }
                            >
                              {ru.staff.makeRole(ru.roles[r] ?? r)}
                            </Menu.Item>
                          ))}
                          <Menu.Item className={menu.menuItem} onSelect={() => setRemoving(m)}>
                            {ru.staff.remove}
                          </Menu.Item>
                        </Menu.Content>
                      </Menu.Portal>
                    </Menu.Root>
                  )}
                </li>
              ))}
            </ul>
          </Card>
        )}

        {owner && (
          <Card>
            <CardHeader title={ru.staff.invites} />
            {invites.isPending ? (
              <SkeletonList rows={2} height={48} />
            ) : invites.isError ? (
              <ErrorState message={errorMessage(invites.error)} onRetry={() => void invites.refetch()} />
            ) : invites.data.length === 0 ? (
              <EmptyState title={ru.staff.invitesEmpty} />
            ) : (
              <ul className={shared.list}>
                {invites.data.map((i) => (
                  <li key={i.id} className={s.inviteRow}>
                    <div className={shared.rowMain}>
                      <span className={shared.rowTitle}>{ru.staff.inviteFor(ru.roles[i.role] ?? i.role, i.name)}</span>
                      <span className={shared.rowMeta}>{ru.staff.expires(formatDateTime(i.expires_at))}</span>
                    </div>
                    <Button size="sm" variant="ghost" onClick={() => setRevoking(i)}>
                      {ru.staff.revoke}
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </Card>
        )}
      </div>

      {inviting && <InviteDialog onClose={() => setInviting(false)} />}
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={removing ? ru.staff.removeTitle(removing.name) : ''}
        text={ru.staff.removeText}
        confirmLabel={ru.staff.remove}
        danger
        loading={remove.isPending}
        onConfirm={() =>
          removing &&
          remove.mutate(removing.user_id, {
            onSuccess: () => toast({ message: ru.staff.removed, tone: 'success' }),
            onSettled: () => setRemoving(null),
          })
        }
      />
      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(o) => !o && setRevoking(null)}
        title={ru.staff.revokeTitle}
        text={ru.staff.revokeText}
        confirmLabel={ru.staff.revoke}
        danger
        loading={revoke.isPending}
        onConfirm={() => revoking && revoke.mutate(revoking.id, { onSuccess: () => (setRevoking(null), toast({ message: ru.staff.revoked, tone: 'success' })) })}
      />
    </>
  )
}

function InviteDialog({ onClose }: { onClose: () => void }) {
  const c = useCenter()
  const create = useCreateInvite(c.id)
  const [role, setRole] = useState<StaffRole>('teacher')
  const [name, setName] = useState('')
  const [error, setError] = useState<{ name?: string; form?: string }>({})
  const [created, setCreated] = useState<Schemas['CreatedInvite'] | null>(null)

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (name.trim().length > 100) {
      setError({ name: ru.errors.tooLong(100) })
      return
    }
    setError({})
    create.mutate(
      { role, ...(name.trim() ? { name: name.trim() } : {}) },
      {
        onSuccess: setCreated,
        onError: (err) => setError(err.field === 'name' ? { name: errorMessage(err) } : { form: errorMessage(err) }),
      },
    )
  }

  if (created) {
    const wa = `https://wa.me/?text=${encodeURIComponent(ru.staff.whatsappText(c.name, created.url))}`
    return (
      <Dialog open onOpenChange={(o) => !o && onClose()} title={ru.staff.linkTitle} description={ru.staff.linkLead} footer={<Button onClick={onClose}>{ru.common.close}</Button>}>
        <div className={layout.stack}>
          <Input label={ru.staff.link} value={created.url} readOnly hint={ru.staff.expires(formatDateTime(created.expires_at))} onFocus={(e) => e.currentTarget.select()} />
          <div className={s.actions}>
            <CopyButton text={created.url} label={ru.staff.copyLink} variant="primary" />
            <ButtonAnchor variant="secondary" href={wa} target="_blank" rel="noopener noreferrer" icon={<MessageCircle size={18} aria-hidden="true" />}>
              {ru.staff.whatsapp}
            </ButtonAnchor>
          </div>
        </div>
      </Dialog>
    )
  }

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={ru.staff.inviteTitle}
      description={ru.staff.inviteLead}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {ru.common.cancel}
          </Button>
          <Button type="submit" form="invite-form" loading={create.isPending}>
            {ru.staff.create}
          </Button>
        </>
      }
    >
      <form id="invite-form" className={layout.stack} onSubmit={submit} noValidate>
        <RadioGroup
          legend={ru.staff.role}
          name="role"
          value={role}
          onChange={(v) => setRole(v as StaffRole)}
          options={ROLES.map((r) => ({ value: r, label: ru.roles[r], hint: ru.staff.roleHints[r] }))}
        />
        <Input label={ru.staff.name} hint={ru.staff.nameHint} optional value={name} onChange={(e) => setName(e.target.value)} error={error.name} />
        <FormError>{error.form}</FormError>
      </form>
    </Dialog>
  )
}
