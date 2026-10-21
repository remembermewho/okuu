import { Archive, CalendarDays, LayoutGrid, Pencil, Plus, User } from 'lucide-react'
import { useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { centerPath, useCenter } from '@/app/center'
import { can } from '@/app/permissions'
import { Badge, Button, Card, Checkbox, ConfirmDialog, EmptyState, ErrorState, PageHeader, SkeletonList, layout, useToast } from '@/components/ui'
import { useArchiveGroup, useGroups, useTariffs, type Group } from '@/features/catalog/api'
import { ru } from '@/i18n/ru'
import { errorMessage } from '@/utils/errors'
import { formatSom } from '@/utils/money'
import { GroupDialog } from './GroupDialog'
import s from './Groups.module.css'
import { scheduleText } from './schedule'

export function GroupsPage() {
  const c = useCenter()
  const manage = can.manageCatalog(c.role)
  const [params, setParams] = useSearchParams()
  const archived = manage && params.get('archived') === '1'
  const groups = useGroups(c.id, archived)
  const tariffs = useTariffs(c.id, false, manage)
  const archive = useArchiveGroup(c.id)
  const toast = useToast()
  const [editing, setEditing] = useState<Group | null | undefined>(undefined) // undefined — закрыто, null — новая
  const [archiving, setArchiving] = useState<Group | null>(null)
  const tariffName = (id: string | null) => (id ? tariffs.data?.find((t) => t.id === id) : undefined)

  return (
    <>
      <PageHeader
        title={ru.groups.title}
        actions={
          manage && (
            <Button icon={<Plus size={18} aria-hidden="true" />} onClick={() => setEditing(null)}>
              {ru.groups.add}
            </Button>
          )
        }
      />
      {manage && (
        <div className={s.toolbar}>
          <Checkbox label={ru.common.showArchived} checked={archived} onChange={(e) => setParams(e.target.checked ? { archived: '1' } : {}, { replace: true })} />
        </div>
      )}

      {groups.isPending ? (
        <SkeletonList rows={3} height={120} />
      ) : groups.isError ? (
        <ErrorState message={errorMessage(groups.error)} onRetry={() => void groups.refetch()} />
      ) : groups.data.length === 0 ? (
        <Card>
          <EmptyState
            icon={<LayoutGrid aria-hidden="true" />}
            title={archived ? ru.groups.emptyArchive : manage ? ru.groups.emptyTitle : ru.groups.emptyTeacher}
            text={!archived && manage ? ru.groups.emptyText : undefined}
            action={
              !archived &&
              manage && (
                <Button icon={<Plus size={18} aria-hidden="true" />} onClick={() => setEditing(null)}>
                  {ru.groups.add}
                </Button>
              )
            }
          />
        </Card>
      ) : (
        <ul className={s.grid}>
          {groups.data.map((g) => {
            const t = tariffName(g.default_tariff_id)
            return (
              <li key={g.id}>
                <Card className={s.card}>
                  <div className={s.cardHead}>
                    {manage ? (
                      <Link to={centerPath(c.id, `groups/${g.id}`)} className={s.cardTitle}>
                        {g.name}
                      </Link>
                    ) : (
                      <span className={s.cardTitle}>{g.name}</span>
                    )}
                    {g.archived && <Badge>{ru.common.archived}</Badge>}
                  </div>
                  <span className={s.meta}>
                    <CalendarDays size={16} aria-hidden="true" />
                    {g.schedule.length ? scheduleText(g.schedule) : ru.groups.noSchedule}
                  </span>
                  <span className={s.meta}>
                    <User size={16} aria-hidden="true" />
                    {g.teacher_name ?? ru.groups.noTeacher}
                  </span>
                  <span className={layout.muted}>
                    {ru.groups.students(g.students_count)}
                    {t ? ` · ${t.name} · ${formatSom(t.price)}` : ''}
                  </span>
                  {manage && !g.archived && (
                    <div className={s.cardActions}>
                      <Button size="sm" variant="secondary" icon={<Pencil size={16} aria-hidden="true" />} onClick={() => setEditing(g)}>
                        {ru.common.edit}
                      </Button>
                      <Button size="sm" variant="ghost" icon={<Archive size={16} aria-hidden="true" />} onClick={() => setArchiving(g)}>
                        {ru.common.archive}
                      </Button>
                    </div>
                  )}
                </Card>
              </li>
            )
          })}
        </ul>
      )}

      <GroupDialog open={editing !== undefined} group={editing ?? null} onClose={() => setEditing(undefined)} />
      <ConfirmDialog
        open={archiving !== null}
        onOpenChange={(o) => !o && setArchiving(null)}
        title={ru.groups.archiveTitle}
        text={ru.groups.archiveText}
        confirmLabel={ru.common.archive}
        danger
        loading={archive.isPending}
        onConfirm={() => archiving && archive.mutate(archiving.id, { onSuccess: () => (setArchiving(null), toast({ message: ru.groups.archived, tone: 'success' })) })}
      />
    </>
  )
}
