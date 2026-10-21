import * as Menu from '@radix-ui/react-dropdown-menu'
import { ChevronLeft, ChevronRight, MoreVertical, Plus } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { centerPath, useCenter } from '@/app/center'
import menu from '@/app/layout.module.css'
import { can } from '@/app/permissions'
import { Badge, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, FormError, Input, PageHeader, Select, SkeletonList, layout, useToast } from '@/components/ui'
import { useGroups, groupOptions } from '@/features/catalog/api'
import { ru } from '@/i18n/ru'
import { cx } from '@/utils/cx'
import { addDays, formatDate, formatDayMonth, todayISO, weekdayLong } from '@/utils/date'
import { errorMessage } from '@/utils/errors'
import { useCreateLesson, useLessons, useSaveMarks, useSetLessonCancelled, type Lesson, type Mark } from './api'
import s from './TodayPage.module.css'

const MARKS: Mark[] = ['present', 'absent', 'excused']
const ISO = /^\d{4}-\d{2}-\d{2}$/

/** Окно дат (как на бэкенде): преподаватель — сегодня−7…сегодня; владелец и админ — до сегодня+30. */
export function dateWindow(teacher: boolean, today: string): { min?: string; max: string } {
  return teacher ? { min: addDays(today, -7), max: today } : { max: addDays(today, 30) }
}

const clamp = (d: string, w: { min?: string; max: string }) => (w.min && d < w.min ? w.min : d > w.max ? w.max : d)

export function TodayPage() {
  const c = useCenter()
  const teacher = c.role === 'teacher'
  const today = todayISO()
  const win = dateWindow(teacher, today)
  const [params, setParams] = useSearchParams()
  const raw = params.get('date') ?? ''
  const date = ISO.test(raw) ? clamp(raw, win) : today
  const lessons = useLessons(c.id, date)
  const [dirty, setDirty] = useState<Record<string, number>>({})
  const [pendingDate, setPendingDate] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const unsaved = Object.values(dirty).reduce((a, b) => a + b, 0)

  const onDirty = useCallback((lessonId: string, n: number) => setDirty((d) => (d[lessonId] === n ? d : { ...d, [lessonId]: n })), [])

  const go = (d: string, force = false) => {
    const next = clamp(d, win)
    if (next === date) return
    if (unsaved > 0 && !force) {
      setPendingDate(next)
      return
    }
    setDirty({})
    setPendingDate(null)
    setParams(next === today ? {} : { date: next }, { replace: true })
  }

  const manage = can.manageCatalog(c.role)

  return (
    <>
      <PageHeader
        title={ru.today.title}
        actions={
          manage && (
            <Button variant="secondary" icon={<Plus size={18} aria-hidden="true" />} onClick={() => setAdding(true)}>
              {ru.today.addLesson}
            </Button>
          )
        }
      />
      <div className={s.dateBar}>
        <Button variant="secondary" iconOnly aria-label={ru.today.prevDay} disabled={win.min !== undefined && date <= win.min} onClick={() => go(addDays(date, -1))}>
          <ChevronLeft size={20} aria-hidden="true" />
        </Button>
        <div className={s.dateMain}>
          <strong className={s.dateTitle}>{ru.today.dayTitle(weekdayLong(date), formatDayMonth(date))}</strong>
          <input
            className={s.dateInput}
            type="date"
            aria-label={ru.today.pickDate}
            value={date}
            min={win.min}
            max={win.max}
            onChange={(e) => ISO.test(e.target.value) && go(e.target.value)}
          />
        </div>
        <Button variant="secondary" iconOnly aria-label={ru.today.nextDay} disabled={date >= win.max} onClick={() => go(addDays(date, 1))}>
          <ChevronRight size={20} aria-hidden="true" />
        </Button>
      </div>
      {(date !== today || teacher) && (
        <div className={s.dateNote}>
          {date !== today && (
            <Button size="sm" variant="ghost" onClick={() => go(today)}>
              {ru.today.toToday}
            </Button>
          )}
          {teacher && <span className={layout.muted}>{ru.today.windowTeacher}</span>}
        </div>
      )}

      {lessons.isPending ? (
        <SkeletonList rows={2} height={220} />
      ) : lessons.isError ? (
        <ErrorState message={errorMessage(lessons.error)} onRetry={() => void lessons.refetch()} />
      ) : lessons.data.length === 0 ? (
        <EmptyState title={ru.today.emptyTitle} text={manage ? ru.today.emptyTextAdmin : ru.today.emptyText} />
      ) : (
        <div className={s.lessons}>
          {lessons.data.map((l) => (
            <LessonCard key={l.id} lesson={l} onDirty={onDirty} />
          ))}
        </div>
      )}

      <ConfirmDialog
        open={pendingDate !== null}
        onOpenChange={(o) => !o && setPendingDate(null)}
        title={ru.today.leaveTitle}
        text={ru.today.leaveText}
        confirmLabel={ru.today.leaveConfirm}
        danger
        onConfirm={() => pendingDate && go(pendingDate, true)}
      />
      {manage && <AddLessonDialog open={adding} date={date} onClose={() => setAdding(false)} />}
    </>
  )
}

function LessonCard({ lesson, onDirty }: { lesson: Lesson; onDirty: (id: string, n: number) => void }) {
  const c = useCenter()
  const manage = can.manageCatalog(c.role)
  const save = useSaveMarks(c.id)
  const toast = useToast()
  // Только изменения поверх сохранённого; после сохранения — пусто.
  const [changes, setChanges] = useState<Record<string, Mark | null>>({})
  const [confirmCancel, setConfirmCancel] = useState(false)
  const cancel = useSetLessonCancelled(c.id)

  const effective = (id: string, saved: Mark | null) => (id in changes ? (changes[id] ?? null) : saved)
  const changed = useMemo(() => Object.keys(changes), [changes])
  const markedCount = lesson.students.filter((st) => effective(st.student_id, st.status) !== null).length

  const update = (next: Record<string, Mark | null>) => {
    setChanges(next)
    onDirty(lesson.id, Object.keys(next).length)
  }
  const setMark = (studentId: string, saved: Mark | null, m: Mark) => {
    const current = effective(studentId, saved)
    const value = current === m ? null : m // повторное нажатие снимает отметку
    const next = { ...changes }
    if (value === saved) delete next[studentId]
    else next[studentId] = value
    update(next)
  }
  const allPresent = () => {
    const next = { ...changes }
    for (const st of lesson.students) {
      if (effective(st.student_id, st.status) === null) {
        if (st.status === 'present') delete next[st.student_id]
        else next[st.student_id] = 'present'
      }
    }
    update(next)
  }
  const onSave = () => {
    if (changed.length === 0) return
    save.mutate(
      { lessonId: lesson.id, marks: changed.map((id) => ({ student_id: id, status: changes[id] ?? null })) },
      {
        onSuccess: () => {
          update({})
          toast({ message: ru.today.saved, tone: 'success' })
        },
        onError: (err) => toast({ message: errorMessage(err), tone: 'error' }),
      },
    )
  }

  const time = lesson.end_time ? `${lesson.start_time}–${lesson.end_time}` : lesson.start_time
  const titleId = `lesson-${lesson.id}`

  return (
    <Card className={cx(s.lesson, lesson.cancelled && s.lessonCancelled)} aria-labelledby={titleId}>
      <div className={s.lessonHead}>
        <div className={s.lessonTitle}>
          <span className={cx(s.time, 'num')}>{time}</span>
          <h2 id={titleId} className={s.groupName}>
            {lesson.group.name}
          </h2>
          {!lesson.end_time && <Badge>{ru.today.unscheduled}</Badge>}
        </div>
        {manage && (
          <Menu.Root>
            <Menu.Trigger asChild>
              <Button variant="ghost" iconOnly aria-label={`${ru.today.lessonMenu}: ${lesson.group.name} ${time}`}>
                <MoreVertical size={20} aria-hidden="true" />
              </Button>
            </Menu.Trigger>
            <Menu.Portal>
              <Menu.Content className={menu.menu} align="end" sideOffset={6}>
                {lesson.cancelled ? (
                  <Menu.Item
                    className={menu.menuItem}
                    onSelect={() => cancel.mutate({ lessonId: lesson.id, cancelled: false }, { onSuccess: () => toast({ message: ru.today.lessonRestored, tone: 'success' }) })}
                  >
                    {ru.today.restoreLesson}
                  </Menu.Item>
                ) : (
                  <Menu.Item className={menu.menuItem} onSelect={() => setConfirmCancel(true)}>
                    {ru.today.cancelLesson}
                  </Menu.Item>
                )}
              </Menu.Content>
            </Menu.Portal>
          </Menu.Root>
        )}
      </div>

      {lesson.cancelled ? (
        <p className={layout.muted}>
          {ru.today.cancelled}
          {' · '}
          {ru.today.cancelledHint}
        </p>
      ) : lesson.students.length === 0 ? (
        <p className={layout.muted}>{ru.today.noStudents}</p>
      ) : (
        <>
          <p className={cx(s.counter, markedCount === lesson.students.length && s.counterDone)} aria-live="polite">
            {ru.today.marked(markedCount, lesson.students.length)}
          </p>
          <ul className={s.students}>
            {lesson.students.map((st) => {
              const value = effective(st.student_id, st.status)
              const isChanged = st.student_id in changes
              return (
                <li key={st.student_id} className={s.student}>
                  <div className={s.studentInfo}>
                    {manage ? (
                      <Link to={centerPath(c.id, `students/${st.student_id}`)} className={s.studentName}>
                        {st.full_name}
                      </Link>
                    ) : (
                      <span className={s.studentName}>{st.full_name}</span>
                    )}
                    {isChanged && <span className={s.changedDot} aria-hidden="true" />}
                    {st.has_debt && <Badge tone="debt">{ru.today.hasDebt}</Badge>}
                    {st.lessons_left !== null && (
                      <span className={st.lessons_left <= 1 ? s.warn : layout.muted}>{ru.today.lessonsLeft(st.lessons_left)}</span>
                    )}
                  </div>
                  <div className={s.marks} role="group" aria-label={st.full_name}>
                    {MARKS.map((m) => (
                      <button
                        key={m}
                        type="button"
                        className={cx(s.mark, s[`mark_${m}`], value === m && s.markOn)}
                        aria-pressed={value === m}
                        title={ru.today.marksFull[m]}
                        onClick={() => setMark(st.student_id, st.status, m)}
                      >
                        {ru.today.marks[m]}
                      </button>
                    ))}
                  </div>
                </li>
              )
            })}
          </ul>
          <div className={s.lessonActions}>
            <Button variant="secondary" onClick={allPresent} disabled={markedCount === lesson.students.length}>
              {ru.today.allPresent}
            </Button>
            <Button onClick={onSave} loading={save.isPending} disabled={changed.length === 0}>
              {ru.today.save}
            </Button>
            {changed.length > 0 && <span className={s.unsaved}>{ru.today.unsaved(changed.length)}</span>}
          </div>
        </>
      )}

      <ConfirmDialog
        open={confirmCancel}
        onOpenChange={setConfirmCancel}
        title={ru.today.cancelLessonTitle}
        text={ru.today.cancelLessonText}
        confirmLabel={ru.today.cancelLesson}
        danger
        loading={cancel.isPending}
        onConfirm={() =>
          cancel.mutate(
            { lessonId: lesson.id, cancelled: true },
            {
              onSuccess: () => {
                setConfirmCancel(false)
                update({})
                toast({ message: ru.today.lessonCancelled, tone: 'success' })
              },
            },
          )
        }
      />
    </Card>
  )
}

function AddLessonDialog({ open, date, onClose }: { open: boolean; date: string; onClose: () => void }) {
  const c = useCenter()
  const groups = useGroups(c.id)
  const create = useCreateLesson(c.id)
  const toast = useToast()
  const [groupId, setGroupId] = useState('')
  const [time, setTime] = useState('')
  const [errors, setErrors] = useState<{ group?: string; time?: string; form?: string }>({})

  const close = () => {
    setGroupId('')
    setTime('')
    setErrors({})
    onClose()
  }
  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    const errs = { group: groupId ? undefined : ru.errors.required, time: /^\d{2}:\d{2}$/.test(time) ? undefined : ru.errors.required }
    setErrors(errs)
    if (errs.group || errs.time) return
    create.mutate(
      { group_id: groupId, date, start_time: time },
      {
        onSuccess: () => {
          toast({ message: ru.today.lessonAdded, tone: 'success' })
          close()
        },
        onError: (err) => {
          const msg = errorMessage(err)
          if (err.code === 'lesson_exists' || err.field === 'start_time') setErrors({ time: msg })
          else if (err.field === 'group_id') setErrors({ group: msg })
          else setErrors({ form: msg })
        },
      },
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && close()}
      title={ru.today.addLessonTitle}
      description={ru.today.addLessonText(formatDate(date))}
      footer={
        <>
          <Button variant="secondary" onClick={close}>
            {ru.common.cancel}
          </Button>
          <Button type="submit" form="add-lesson" loading={create.isPending}>
            {ru.common.add}
          </Button>
        </>
      }
    >
      <form id="add-lesson" className={layout.stack} onSubmit={submit} noValidate>
        <Select
          label={ru.groups.title}
          placeholder={ru.studentForm.chooseGroup}
          options={groupOptions(groups.data)}
          value={groupId}
          onChange={(e) => setGroupId(e.target.value)}
          error={errors.group}
        />
        <Input label={ru.today.time} type="time" value={time} onChange={(e) => setTime(e.target.value)} error={errors.time} />
        <FormError>{errors.form}</FormError>
      </form>
    </Dialog>
  )
}
