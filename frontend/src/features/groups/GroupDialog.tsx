import { X } from 'lucide-react'
import { useState } from 'react'
import { useCenter } from '@/app/center'
import { Button, Dialog, FormError, Input, Select, layout, useToast } from '@/components/ui'
import { useSaveGroup, useTariffs, type Group, type Slot } from '@/features/catalog/api'
import { useStaff } from '@/features/staff/api'
import { ru } from '@/i18n/ru'
import { cx } from '@/utils/cx'
import { dowShort } from '@/utils/date'
import { errorMessage } from '@/utils/errors'
import { formatSom } from '@/utils/money'
import s from './Groups.module.css'

const DAYS = [1, 2, 3, 4, 5, 6, 7]
const TIME = /^\d{2}:\d{2}$/

type Errors = { name?: string; teacher?: string; tariff?: string; schedule?: string; slots?: Record<number, string>; form?: string }

/** Ошибки расписания: время в формате ЧЧ:ММ и конец позже начала. */
export function checkSchedule(slots: Slot[]): Record<number, string> {
  const out: Record<number, string> = {}
  slots.forEach((sl, i) => {
    if (!TIME.test(sl.start) || !TIME.test(sl.end)) out[i] = ru.errors.required
    else if (sl.end <= sl.start) out[i] = ru.groups.timeOrder
  })
  return out
}

/** Создание и изменение группы: название, преподаватель, дни и время, тариф по умолчанию. */
export function GroupDialog({ open, group, onClose }: { open: boolean; group: Group | null; onClose: () => void }) {
  return open ? <GroupForm group={group} onClose={onClose} /> : null
}

function GroupForm({ group, onClose }: { group: Group | null; onClose: () => void }) {
  const c = useCenter()
  const staff = useStaff(c.id)
  const tariffs = useTariffs(c.id)
  const save = useSaveGroup(c.id)
  const toast = useToast()
  const [name, setName] = useState(group?.name ?? '')
  const [teacherId, setTeacherId] = useState(group?.teacher_id ?? '')
  const [tariffId, setTariffId] = useState(group?.default_tariff_id ?? '')
  const [slots, setSlots] = useState<Slot[]>(() => [...(group?.schedule ?? [])].sort((a, b) => a.dow - b.dow || a.start.localeCompare(b.start)))
  const [errors, setErrors] = useState<Errors>({})

  const toggleDay = (dow: number) => {
    if (slots.some((sl) => sl.dow === dow)) {
      setSlots(slots.filter((sl) => sl.dow !== dow))
      return
    }
    // Новое время — как у последнего дня: обычно группа занимается в одно время.
    const last = slots[slots.length - 1]
    const next = [...slots, { dow, start: last?.start ?? '', end: last?.end ?? '' }]
    setSlots(next.sort((a, b) => a.dow - b.dow || a.start.localeCompare(b.start)))
  }
  const setSlot = (i: number, patch: Partial<Slot>) => setSlots(slots.map((sl, j) => (j === i ? { ...sl, ...patch } : sl)))

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    const errs: Errors = {}
    if (!name.trim()) errs.name = ru.errors.required
    else if (name.trim().length > 100) errs.name = ru.errors.tooLong(100)
    const se = checkSchedule(slots)
    if (Object.keys(se).length) errs.slots = se
    setErrors(errs)
    if (errs.name || errs.slots) return
    save.mutate(
      { id: group?.id, body: { name: name.trim(), teacher_id: teacherId || null, default_tariff_id: tariffId || null, schedule: slots } },
      {
        onSuccess: () => {
          toast({ message: group ? ru.groups.saved : ru.groups.created, tone: 'success' })
          onClose()
        },
        onError: (err) => {
          const msg = errorMessage(err)
          if (err.field === 'name') setErrors({ name: msg })
          else if (err.field === 'teacher_id') setErrors({ teacher: msg })
          else if (err.field === 'default_tariff_id') setErrors({ tariff: msg })
          else if (err.field?.startsWith('schedule')) setErrors({ schedule: msg })
          else setErrors({ form: msg })
        },
      },
    )
  }

  const tariffOptions = (tariffs.data ?? []).map((t) => ({ value: t.id, label: `${t.name} · ${formatSom(t.price)}` }))
  // Тариф группы мог уйти в архив — оставим его в списке, чтобы не сбросить молча.
  if (tariffId && group?.default_tariff_id === tariffId && !tariffOptions.some((o) => o.value === tariffId)) {
    tariffOptions.push({ value: tariffId, label: ru.groups.archivedTariff })
  }

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={group ? ru.groups.editTitle : ru.groups.createTitle}
      wide
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {ru.common.cancel}
          </Button>
          <Button type="submit" form="group-form" loading={save.isPending}>
            {group ? ru.common.save : ru.common.create}
          </Button>
        </>
      }
    >
      <form id="group-form" className={layout.stack} onSubmit={submit} noValidate>
        <Input label={ru.groups.name} placeholder={ru.groups.namePlaceholder} value={name} onChange={(e) => setName(e.target.value)} error={errors.name} autoFocus />
        <Select
          label={ru.groups.teacher}
          value={teacherId}
          onChange={(e) => setTeacherId(e.target.value)}
          placeholder={ru.groups.noTeacher}
          options={(staff.data ?? []).map((m) => ({ value: m.user_id, label: `${m.name} · ${(ru.roles[m.role] ?? m.role).toLowerCase()}` }))}
          error={errors.teacher}
        />
        <fieldset className={s.schedule}>
          <legend className={s.legend}>{ru.groups.schedule}</legend>
          <p className={s.hint}>{ru.groups.scheduleHint}</p>
          <div className={s.days}>
            {DAYS.map((d) => {
              const on = slots.some((sl) => sl.dow === d)
              return (
                <button key={d} type="button" className={cx(s.day, on && s.dayOn)} aria-pressed={on} onClick={() => toggleDay(d)}>
                  {dowShort(d)}
                </button>
              )
            })}
          </div>
          {slots.map((sl, i) => (
            <div key={`${sl.dow}-${i}`} className={s.slot}>
              <span className={s.slotDay}>{dowShort(sl.dow)}</span>
              <Input
                label={<span className="visually-hidden">{ru.groups.startLabel(dowShort(sl.dow))}</span>}
                className={s.time}
                type="time"
                value={sl.start}
                aria-invalid={errors.slots?.[i] ? true : undefined}
                onChange={(e) => setSlot(i, { start: e.target.value })}
              />
              <span aria-hidden="true">–</span>
              <Input
                label={<span className="visually-hidden">{ru.groups.endLabel(dowShort(sl.dow))}</span>}
                className={s.time}
                type="time"
                value={sl.end}
                aria-invalid={errors.slots?.[i] ? true : undefined}
                onChange={(e) => setSlot(i, { end: e.target.value })}
              />
              <Button variant="ghost" iconOnly aria-label={`${ru.common.remove}: ${dowShort(sl.dow)}`} onClick={() => setSlots(slots.filter((_, j) => j !== i))}>
                <X size={18} aria-hidden="true" />
              </Button>
              {errors.slots?.[i] && (
                <span className={s.slotError} role="alert">
                  {errors.slots[i]}
                </span>
              )}
            </div>
          ))}
          {errors.schedule && (
            <span className={s.slotError} role="alert">
              {errors.schedule}
            </span>
          )}
        </fieldset>
        <Select
          label={ru.groups.tariff}
          hint={tariffs.data?.length === 0 ? ru.groups.noTariffs : ru.groups.tariffHint}
          value={tariffId}
          onChange={(e) => setTariffId(e.target.value)}
          placeholder={ru.groups.noTariff}
          options={tariffOptions}
          error={errors.tariff}
        />
        <FormError>{errors.form}</FormError>
      </form>
    </Dialog>
  )
}
