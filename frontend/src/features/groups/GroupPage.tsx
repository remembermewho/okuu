import { ChevronLeft, Pencil } from 'lucide-react'
import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { centerPath, useCenter } from '@/app/center'
import shared from '@/components/shared.module.css'
import { StudentStatusBadge } from '@/components/shared'
import { Badge, Button, Card, CardHeader, EmptyState, ErrorState, MoneyText, PageHeader, Skeleton, SkeletonList } from '@/components/ui'
import { useGroup, useTariffs } from '@/features/catalog/api'
import { useStudentList } from '@/features/students/api'
import { ru } from '@/i18n/ru'
import { errorMessage } from '@/utils/errors'
import { formatSom } from '@/utils/money'
import { GroupDialog } from './GroupDialog'
import s from './Groups.module.css'
import { scheduleText } from './schedule'

/** Группа и её состав (владелец, админ). */
export function GroupPage() {
  const { groupId = '' } = useParams()
  const c = useCenter()
  const group = useGroup(c.id, groupId)
  const tariffs = useTariffs(c.id)
  const members = useStudentList(c.id, { group: groupId })
  const [editing, setEditing] = useState(false)
  const items = members.data?.pages.flatMap((p) => p.items) ?? []

  const back = (
    <Link to={centerPath(c.id, 'groups')} className={s.back}>
      <ChevronLeft size={18} aria-hidden="true" />
      {ru.groups.back}
    </Link>
  )
  if (group.isPending)
    return (
      <>
        {back}
        <Skeleton height={32} width="50%" />
        <SkeletonList rows={4} />
      </>
    )
  if (group.isError)
    return (
      <>
        {back}
        <ErrorState message={errorMessage(group.error)} onRetry={() => void group.refetch()} />
      </>
    )
  const g = group.data
  const t = tariffs.data?.find((x) => x.id === g.default_tariff_id)

  return (
    <>
      {back}
      <PageHeader
        title={
          <span className={s.titleRow}>
            {g.name}
            {g.archived && <Badge>{ru.common.archived}</Badge>}
          </span>
        }
        subtitle={[g.schedule.length ? scheduleText(g.schedule) : ru.groups.noSchedule, g.teacher_name ?? ru.groups.noTeacher, t && `${t.name} · ${formatSom(t.price)}`]
          .filter(Boolean)
          .join(' · ')}
        actions={
          !g.archived && (
            <Button variant="secondary" icon={<Pencil size={16} aria-hidden="true" />} onClick={() => setEditing(true)}>
              {ru.common.edit}
            </Button>
          )
        }
      />
      <Card flush>
        <div className={s.membersHead}>
          <CardHeader title={`${ru.groups.members} · ${ru.groups.students(g.students_count)}`} />
        </div>
        {members.isPending ? (
          <SkeletonList rows={4} />
        ) : members.isError ? (
          <ErrorState message={errorMessage(members.error)} onRetry={() => void members.refetch()} />
        ) : items.length === 0 ? (
          <EmptyState title={ru.groups.membersEmpty} />
        ) : (
          <>
            <ul className={shared.list} aria-label={ru.groups.members}>
              {items.map((st) => (
                <li key={st.id}>
                  <Link to={centerPath(c.id, `students/${st.id}`)} className={shared.row}>
                    <div className={shared.rowMain}>
                      <span className={shared.rowTitle}>
                        {st.full_name}
                        <StudentStatusBadge status={st.status} />
                      </span>
                    </div>
                    <div className={shared.rowSide}>
                      <MoneyText tyiyn={st.balance} tone="balance" className={shared.amount} />
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
            {members.hasNextPage && (
              <div className={shared.loadMore}>
                <Button variant="secondary" loading={members.isFetchingNextPage} onClick={() => void members.fetchNextPage()}>
                  {ru.common.loadMore}
                </Button>
              </div>
            )}
          </>
        )}
      </Card>
      <GroupDialog open={editing} group={g} onClose={() => setEditing(false)} />
    </>
  )
}
