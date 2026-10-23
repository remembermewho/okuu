import { Link2, Plus } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { ShareLinkDialog } from '@/components/ShareLinkDialog'
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, Input, PageHeader, SkeletonList, useToast, type BadgeTone } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { formatDate, formatDateTime } from '@/utils/date'
import { errorMessage } from '@/utils/errors'
import { useCreateRegistrationLink, useRegistrationLinks, useRevokeRegistrationLink, type RegistrationLink } from './api'
import s from './Platform.module.css'

const linkTone: Record<string, BadgeTone> = { active: 'success', used: 'neutral', revoked: 'debt', expired: 'neutral' }

/** /admin/links — ссылки регистрации центров: создать (с пометкой, для кого), список, отозвать. */
export function LinksPage() {
  const links = useRegistrationLinks()
  const create = useCreateRegistrationLink()
  const [note, setNote] = useState('')
  const [error, setError] = useState<string>()
  const [issued, setIssued] = useState<string>()
  const [revoking, setRevoking] = useState<RegistrationLink>()

  return (
    <>
      <PageHeader title={ru.platform.links} />
      <Card>
        <form
          className={s.createRow}
          noValidate
          onSubmit={(e) => {
            e.preventDefault()
            setError(undefined)
            if (!note.trim()) {
              setError(ru.errors.required)
              return
            }
            create.mutate(note.trim(), {
              onSuccess: (l) => {
                setIssued(l.url)
                setNote('')
              },
              onError: (err) => setError(errorMessage(err)),
            })
          }}
        >
          <Input label={ru.platform.linkNote} value={note} error={error} maxLength={200} onChange={(e) => setNote(e.target.value)} />
          <Button type="submit" icon={<Plus size={18} aria-hidden="true" />} loading={create.isPending}>
            {ru.platform.linkCreate}
          </Button>
        </form>
        <p className={s.note}>
          {ru.platform.linkNoteHint}
        </p>
      </Card>

      <div className={s.gap} aria-hidden="true" />
      {links.isPending ? (
        <SkeletonList rows={3} />
      ) : links.isError ? (
        <ErrorState message={errorMessage(links.error)} onRetry={() => void links.refetch()} />
      ) : links.data.items.length === 0 ? (
        <EmptyState icon={<Link2 aria-hidden="true" />} title={ru.platform.linksEmpty} text={ru.platform.linksEmptyText} />
      ) : (
        <Card flush>
          <ul className={s.links}>
            {links.data.items.map((l) => (
              <li key={l.id}>
                <div>
                  <div className={s.linkNote}>{l.note}</div>
                  <div className={s.auditMeta}>
                    {ru.platform.linkCreated(formatDateTime(l.created_at), l.created_by_name ?? ru.platform.console)}
                    {l.status === 'active' && ` · ${ru.platform.linkExpires(formatDate(l.expires_at.slice(0, 10)))}`}
                  </div>
                  {l.center && (
                    <div className={s.auditMeta}>
                      {ru.platform.linkCenter}
                      {': '}
                      <Link to={`/admin/centers/${l.center.id}`}>{l.center.name}</Link>
                    </div>
                  )}
                </div>
                <div className={s.badges}>
                  <Badge tone={linkTone[l.status]}>{ru.platform.linkStatus[l.status]}</Badge>
                  {l.status === 'active' && (
                    <Button variant="ghost" onClick={() => setRevoking(l)}>
                      {ru.platform.linkRevoke}
                    </Button>
                  )}
                </div>
              </li>
            ))}
          </ul>
        </Card>
      )}

      {issued && (
        <ShareLinkDialog
          title={ru.platform.linkCreatedTitle}
          lead={ru.platform.linkCreatedLead}
          url={issued}
          whatsappText={ru.platform.linkWa(issued)}
          onClose={() => setIssued(undefined)}
        />
      )}
      {revoking && <RevokeDialog link={revoking} onClose={() => setRevoking(undefined)} />}
    </>
  )
}

function RevokeDialog({ link, onClose }: { link: RegistrationLink; onClose: () => void }) {
  const toast = useToast()
  const revoke = useRevokeRegistrationLink()
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={ru.platform.linkRevokeTitle}
      text={ru.platform.linkRevokeText(link.note)}
      confirmLabel={ru.platform.linkRevoke}
      danger
      loading={revoke.isPending}
      onConfirm={() =>
        revoke.mutate(link.id, {
          onSuccess: () => toast({ message: ru.platform.linkRevoked, tone: 'success' }),
          onSettled: onClose,
        })
      }
    />
  )
}
