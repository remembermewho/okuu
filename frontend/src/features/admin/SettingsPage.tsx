import { zodResolver } from '@hookform/resolvers/zod'
import { CalendarClock } from 'lucide-react'
import { useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { z } from 'zod'
import type { Schemas } from '@/api/client'
import { useCenter } from '@/app/center'
import { can } from '@/app/permissions'
import { Badge, Button, Card, CardHeader, Checkbox, ErrorState, FormError, Input, PageHeader, Select, SkeletonList, Textarea, layout, useToast } from '@/components/ui'
import { isValidPhone, normalizePhone } from '@/utils/phone'
import { ru } from '@/i18n/ru'
import { formatDate } from '@/utils/date'
import { applyFieldError, errorMessage } from '@/utils/errors'
import { usePatchSettings, useSettings, type Settings } from './api'
import s from './Settings.module.css'

const TIMEZONES = ['Asia/Bishkek', 'Asia/Almaty', 'Asia/Tashkent', 'Asia/Dushanbe', 'Europe/Moscow']

const intIn = (min: number, max: number) =>
  z
    .string()
    .trim()
    .refine((v) => /^\d+$/.test(v) && Number(v) >= min && Number(v) <= max, ru.errors.range(min, max))

const schema = z.object({
  name: z.string().trim().min(1, ru.errors.required).max(100, ru.errors.tooLong(100)),
  phone: z
    .string()
    .trim()
    .refine((v) => v === '' || isValidPhone(v), ru.errors.invalidPhone),
  timezone: z.string().min(1, ru.errors.required),
  billing_day: intIn(1, 28),
  due_days: intIn(0, 60),
  excused_deducts: z.boolean(),
  reminders_on: z.boolean(),
  overdue_repeat: z.enum(['weekly', 'off']),
  notify_absent: z.boolean(),
  pay_details: z.string().max(1000, ru.errors.tooLong(1000)),
})
type Values = z.infer<typeof schema>

const toForm = (st: Settings): Values => ({
  name: st.name,
  phone: st.phone ?? '',
  timezone: st.timezone,
  billing_day: String(st.billing_day_next ?? st.billing_day),
  due_days: String(st.due_days),
  excused_deducts: st.excused_deducts,
  reminders_on: st.reminders_on,
  overdue_repeat: st.overdue_repeat,
  notify_absent: st.notify_absent,
  pay_details: st.pay_details ?? '',
})

/** Только изменённые поля — PATCH частичный. */
export function settingsPatch(before: Values, v: Values): Schemas['PatchSettingsRequest'] {
  const out: Schemas['PatchSettingsRequest'] = {}
  if (v.name.trim() !== before.name) out.name = v.name.trim()
  if (v.phone.trim() !== before.phone) out.phone = v.phone.trim() ? (normalizePhone(v.phone) ?? v.phone.trim()) : null
  if (v.timezone !== before.timezone) out.timezone = v.timezone
  if (v.billing_day !== before.billing_day) out.billing_day = Number(v.billing_day)
  if (v.due_days !== before.due_days) out.due_days = Number(v.due_days)
  if (v.excused_deducts !== before.excused_deducts) out.excused_deducts = v.excused_deducts
  if (v.reminders_on !== before.reminders_on) out.reminders_on = v.reminders_on
  if (v.overdue_repeat !== before.overdue_repeat) out.overdue_repeat = v.overdue_repeat
  if (v.notify_absent !== before.notify_absent) out.notify_absent = v.notify_absent
  if (v.pay_details.trim() !== before.pay_details) out.pay_details = v.pay_details.trim() || null
  return out
}

export function SettingsPage() {
  const c = useCenter()
  const settings = useSettings(c.id)
  return (
    <>
      <PageHeader title={ru.settings.title} subtitle={can.manageStaff(c.role) ? undefined : ru.settings.readOnly} />
      {settings.isPending ? (
        <SkeletonList rows={4} height={140} />
      ) : settings.isError ? (
        <ErrorState message={errorMessage(settings.error)} onRetry={() => void settings.refetch()} />
      ) : (
        // key: после сохранения форма берёт новые значения с сервера.
        <SettingsForm key={JSON.stringify(settings.data)} settings={settings.data} />
      )}
    </>
  )
}

function SettingsForm({ settings }: { settings: Settings }) {
  const c = useCenter()
  const readOnly = !can.manageStaff(c.role)
  const patch = usePatchSettings(c.id)
  const toast = useToast()
  const [formError, setFormError] = useState<string>()
  const initial = toForm(settings)
  const {
    register,
    control,
    handleSubmit,
    setError,
    formState: { errors, isDirty },
  } = useForm<Values>({ resolver: zodResolver(schema), defaultValues: initial })
  const billingDay = useWatch({ control, name: 'billing_day' })
  const remindersOn = useWatch({ control, name: 'reminders_on' })
  const billingChanged = billingDay !== initial.billing_day

  const onSubmit = handleSubmit((v) => {
    setFormError(undefined)
    const body = settingsPatch(initial, v)
    if (Object.keys(body).length === 0) {
      toast({ message: ru.settings.noChanges })
      return
    }
    patch.mutate(body, {
      onSuccess: () => toast({ message: ru.settings.saved, tone: 'success' }),
      onError: (err) => {
        if (!applyFieldError(err, setError, ['name', 'phone', 'timezone', 'billing_day', 'due_days', 'pay_details'])) setFormError(errorMessage(err))
      },
    })
  })

  const tz = TIMEZONES.includes(settings.timezone) ? TIMEZONES : [settings.timezone, ...TIMEZONES]

  return (
    <form onSubmit={onSubmit} noValidate>
      <fieldset className={s.fieldset} disabled={readOnly}>
        <div className={s.grid}>
          <Card>
            <CardHeader title={ru.settings.center} />
            <div className={layout.stack}>
              <Input label={ru.settings.name} error={errors.name?.message} {...register('name')} />
              <Input label={ru.settings.phone} hint={ru.settings.phoneHint} optional type="tel" inputMode="tel" error={errors.phone?.message} {...register('phone')} />
              <Select label={ru.settings.timezone} options={tz.map((t) => ({ value: t, label: t }))} error={errors.timezone?.message} {...register('timezone')} />
              <p className={s.plan}>
                {ru.settings.plan}
                {': '}
                <Badge>{settings.plan}</Badge>
              </p>
            </div>
          </Card>

          <Card>
            <CardHeader title={ru.settings.billing} />
            <div className={layout.stack}>
              {settings.billing_day_next !== null && settings.billing_day_next_from && (
                <p className={s.notice} role="status">
                  <CalendarClock size={18} aria-hidden="true" />
                  {ru.settings.billingDayNext(settings.billing_day_next, formatDate(settings.billing_day_next_from))}
                </p>
              )}
              <Input
                label={ru.settings.billingDay}
                inputMode="numeric"
                hint={billingChanged ? ru.settings.billingDayChange : ru.settings.billingDayHint}
                error={errors.billing_day?.message}
                {...register('billing_day')}
              />
              <Input label={ru.settings.dueDays} inputMode="numeric" hint={ru.settings.dueDaysHint} error={errors.due_days?.message} {...register('due_days')} />
            </div>
          </Card>

          <Card>
            <CardHeader title={ru.settings.attendance} />
            <Checkbox label={ru.settings.excusedDeducts} hint={ru.settings.excusedDeductsHint} {...register('excused_deducts')} />
          </Card>

          <Card>
            <CardHeader title={ru.settings.notifications} />
            <div className={layout.stackSm}>
              <Checkbox label={ru.settings.remindersOn} hint={ru.settings.remindersOnHint} {...register('reminders_on')} />
              <Select
                label={ru.settings.overdueRepeat}
                hint={remindersOn ? ru.settings.overdueRepeatHint : ru.settings.overdueRepeatOffHint}
                options={(['weekly', 'off'] as const).map((v) => ({ value: v, label: ru.settings.overdueRepeatOptions[v] }))}
                {...register('overdue_repeat')}
              />
              <Checkbox label={ru.settings.notifyAbsent} {...register('notify_absent')} />
            </div>
          </Card>

          <Card className={s.wide}>
            <CardHeader title={ru.settings.payment} />
            <div className={layout.stack}>
              <p className={s.finik}>
                {ru.settings.finik}
                {': '}
                <Badge tone={settings.finik_connected ? 'success' : 'neutral'}>{settings.finik_connected ? ru.settings.finikOn : ru.settings.finikOff}</Badge>
              </p>
              <Textarea label={ru.settings.payDetails} hint={ru.settings.payDetailsHint} optional rows={4} error={errors.pay_details?.message} {...register('pay_details')} />
            </div>
          </Card>
        </div>
      </fieldset>
      {!readOnly && (
        <div className={s.footer}>
          <FormError>{formError}</FormError>
          <Button type="submit" size="lg" loading={patch.isPending} disabled={!isDirty}>
            {ru.common.save}
          </Button>
        </div>
      )}
    </form>
  )
}
