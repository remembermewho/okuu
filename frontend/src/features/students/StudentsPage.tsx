import { FileSpreadsheet, Plus, SearchX, Users } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { centerPath, useCenter } from '@/app/center'
import { StudentStatusBadge, TgBadge } from '@/components/shared'
import shared from '@/components/shared.module.css'
import { Button, ButtonLink, Card, Checkbox, EmptyState, ErrorState, Input, MoneyText, PageHeader, Select, SkeletonList } from '@/components/ui'
import { groupOptions, useGroups } from '@/features/catalog/api'
import { ru } from '@/i18n/ru'
import { errorMessage } from '@/utils/errors'
import { formatPhone } from '@/utils/phone'
import { AddStudentDialog } from './AddStudentDialog'
import { useStudentList, type StudentFilter } from './api'

function useDebounced<T>(value: T, ms = 300): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const t = window.setTimeout(() => setV(value), ms)
    return () => window.clearTimeout(t)
  }, [value, ms])
  return v
}

export function StudentsPage() {
  const c = useCenter()
  // Фильтры — в адресе: обновление страницы и «назад» из карточки их не теряют.
  const [params, setParams] = useSearchParams()
  const [q, setQ] = useState(params.get('q') ?? '')
  const debouncedQ = useDebounced(q.trim())
  const filter: StudentFilter = {
    q: debouncedQ || undefined,
    group: params.get('group') || undefined,
    status: (params.get('status') || undefined) as StudentFilter['status'],
    tg: (params.get('tg') || undefined) as StudentFilter['tg'],
    // Ушедшие по умолчанию скрыты; «Показать ушедших» или фильтр по статусу — показываются.
    hide_left: params.get('show_left') !== '1' && !params.get('status') ? true : undefined,
  }
  const showLeft = params.get('show_left') === '1'
  const groups = useGroups(c.id)
  const list = useStudentList(c.id, filter)
  const [adding, setAdding] = useState(false)

  useEffect(() => {
    setParams(
      (p) => {
        if (debouncedQ) p.set('q', debouncedQ)
        else p.delete('q')
        return p
      },
      { replace: true },
    )
  }, [debouncedQ, setParams])

  const setFilter = (key: string, value: string) =>
    setParams(
      (p) => {
        if (value) p.set(key, value)
        else p.delete(key)
        return p
      },
      { replace: true },
    )

  const items = list.data?.pages.flatMap((p) => p.items) ?? []
  const filtered = Boolean(filter.q || filter.group || filter.status || filter.tg)

  return (
    <>
      <PageHeader
        title={ru.students.title}
        subtitle={list.data ? ru.students.count(items.length, list.hasNextPage) : undefined}
        actions={
          <Button icon={<Plus size={18} aria-hidden="true" />} onClick={() => setAdding(true)}>
            {ru.students.add}
          </Button>
        }
      />

      <div className={shared.filters} role="search">
        <Input label={ru.students.searchLabel} type="search" placeholder={ru.students.searchPlaceholder} value={q} onChange={(e) => setQ(e.target.value)} />
        <Select label={ru.students.status} value={filter.status ?? ''} onChange={(e) => setFilter('status', e.target.value)} options={(['active', 'paused', 'left'] as const).map((k) => ({ value: k, label: ru.statuses.studentFilter[k] ?? k }))} placeholder={ru.statuses.studentFilter['']} />
        <Select label={ru.students.group} value={filter.group ?? ''} onChange={(e) => setFilter('group', e.target.value)} options={groupOptions(groups.data)} placeholder={ru.students.allGroups} />
        <Select label={ru.students.telegram} value={filter.tg ?? ''} onChange={(e) => setFilter('tg', e.target.value)} options={(['active', 'none', 'stopped', 'blocked'] as const).map((k) => ({ value: k, label: ru.statuses.tgFilter[k] ?? k }))} placeholder={ru.statuses.tgFilter['']} />
      </div>
      {!filter.status && (
        <div className={shared.toggleRow}>
          <Checkbox label={ru.students.showLeft} checked={showLeft} onChange={(e) => setFilter('show_left', e.target.checked ? '1' : '')} />
        </div>
      )}

      {list.isPending ? (
        <SkeletonList rows={6} />
      ) : list.isError ? (
        <ErrorState message={errorMessage(list.error)} onRetry={() => void list.refetch()} />
      ) : items.length === 0 ? (
        <Card>
          {filtered ? (
            <EmptyState icon={<SearchX aria-hidden="true" />} title={ru.students.notFoundTitle} text={ru.students.notFoundText} />
          ) : (
            <EmptyState
              icon={<Users aria-hidden="true" />}
              title={ru.students.emptyTitle}
              text={ru.students.emptyText}
              action={
                <div className={shared.sectionHead}>
                  <Button icon={<Plus size={18} aria-hidden="true" />} onClick={() => setAdding(true)}>
                    {ru.students.add}
                  </Button>
                  <ButtonLink variant="secondary" to={centerPath(c.id, 'import')} icon={<FileSpreadsheet size={18} aria-hidden="true" />}>
                    {ru.students.importLink}
                  </ButtonLink>
                </div>
              }
            />
          )}
        </Card>
      ) : (
        <Card flush>
          <ul className={shared.list} aria-label={ru.students.title}>
            {items.map((st) => (
              <li key={st.id}>
                <Link to={centerPath(c.id, `students/${st.id}`)} className={shared.row}>
                  <div className={shared.rowMain}>
                    <span className={shared.rowTitle}>
                      {st.full_name}
                      <StudentStatusBadge status={st.status} />
                    </span>
                    <span className={shared.rowMeta}>
                      {st.groups.map((g) => g.name).join(', ') || ru.students.noGroups}
                      {st.parent ? ` · ${formatPhone(st.parent.phone)}` : ''}
                    </span>
                    {st.parent && (
                      <span>
                        <TgBadge status={st.parent.tg_status} short />
                      </span>
                    )}
                  </div>
                  <div className={shared.rowSide}>
                    <MoneyText tyiyn={st.balance} tone="balance" className={shared.amount} />
                  </div>
                </Link>
              </li>
            ))}
          </ul>
          {list.hasNextPage && (
            <div className={shared.loadMore}>
              <Button variant="secondary" loading={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
                {ru.common.loadMore}
              </Button>
            </div>
          )}
        </Card>
      )}

      <AddStudentDialog open={adding} onOpenChange={setAdding} />
    </>
  )
}
