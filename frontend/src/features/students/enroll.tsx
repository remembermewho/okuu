import { Controller, useFormContext, useWatch } from 'react-hook-form'
import { z } from 'zod'
import type { Schemas } from '@/api/client'
import { Input, RadioGroup, Select, layout } from '@/components/ui'
import type { Group, Tariff } from '@/features/catalog/api'
import { ru } from '@/i18n/ru'
import { formatSom, parseSom } from '@/utils/money'

/** Поля записи в группу внутри любой формы: ключ формы — `enroll`. */
export const enrollFields = z.object({
  group_id: z.string(),
  tariff_id: z.string(), // '' — тариф группы
  start_date: z.string(),
  first_month: z.enum(['full', 'manual', 'none']),
  manual_amount: z.string(),
})
export type EnrollValues = z.infer<typeof enrollFields>

export const emptyEnroll = (today: string): EnrollValues => ({ group_id: '', tariff_id: '', start_date: today, first_month: 'full', manual_amount: '' })

/** Тариф записи: выбранный или тариф группы по умолчанию. */
export function effectiveTariff(v: EnrollValues, groups: Group[] = [], tariffs: Tariff[] = []): Tariff | undefined {
  const id = v.tariff_id || groups.find((g) => g.id === v.group_id)?.default_tariff_id
  return tariffs.find((t) => t.id === id)
}

/** Проверка полей записи (для superRefine формы). path — префикс полей ('enroll'). */
export function checkEnroll(v: EnrollValues, groups: Group[], tariffs: Tariff[], ctx: z.RefinementCtx, path: string[]) {
  if (!v.group_id) ctx.addIssue({ code: 'custom', path: [...path, 'group_id'], message: ru.errors.required })
  if (!v.start_date) ctx.addIssue({ code: 'custom', path: [...path, 'start_date'], message: ru.errors.required })
  const t = effectiveTariff(v, groups, tariffs)
  if (v.group_id && !t) ctx.addIssue({ code: 'custom', path: [...path, 'tariff_id'], message: ru.errors.required })
  if (t?.kind === 'monthly' && v.first_month === 'manual' && parseSom(v.manual_amount) === null) {
    ctx.addIssue({ code: 'custom', path: [...path, 'manual_amount'], message: ru.errors.invalidAmount })
  }
}

export function enrollBody(v: EnrollValues, groups: Group[], tariffs: Tariff[]): Schemas['EnrollRequest'] {
  const t = effectiveTariff(v, groups, tariffs)
  const body: Schemas['EnrollRequest'] = { group_id: v.group_id, start_date: v.start_date }
  if (v.tariff_id) body.tariff_id = v.tariff_id
  if (t?.kind === 'monthly') {
    body.first_month = v.first_month
    if (v.first_month === 'manual') body.manual_amount = parseSom(v.manual_amount)
  }
  return body
}

type FormWithEnroll = { enroll: EnrollValues }

/** Группа, тариф, дата начала и «за текущий месяц» (R5). */
export function EnrollFields({ groups, tariffs }: { groups: Group[]; tariffs: Tariff[] }) {
  const {
    register,
    control,
    formState: { errors },
  } = useFormContext<FormWithEnroll>()
  const v = useWatch({ control, name: 'enroll' })
  const t = effectiveTariff(v, groups, tariffs)
  const e = errors.enroll

  return (
    <div className={`${layout.formGrid} ${layout.formGrid2}`}>
      <Select
        label={ru.studentForm.group}
        options={groups.map((g) => ({ value: g.id, label: g.name }))}
        placeholder={ru.studentForm.chooseGroup}
        error={e?.group_id?.message}
        {...register('enroll.group_id')}
      />
      <Select
        label={ru.studentForm.tariff}
        options={tariffs.map((x) => ({ value: x.id, label: `${x.name} · ${formatSom(x.price)}` }))}
        placeholder={ru.studentForm.tariffDefault}
        error={e?.tariff_id?.message}
        {...register('enroll.tariff_id')}
      />
      <Input label={ru.studentForm.startDate} type="date" error={e?.start_date?.message} {...register('enroll.start_date')} />
      {t?.kind === 'package' && <p className={layout.muted}>{ru.studentForm.packageNote(t.lessons_count, formatSom(t.price))}</p>}
      {t?.kind === 'monthly' && (
        <>
          <Controller
            control={control}
            name="enroll.first_month"
            render={({ field }) => (
              <RadioGroup
                legend={ru.studentForm.firstMonth}
                name="first_month"
                value={field.value}
                onChange={field.onChange}
                options={(['full', 'manual', 'none'] as const).map((k) => ({
                  value: k,
                  label: ru.studentForm.firstMonthOptions[k],
                  hint: k === 'full' ? formatSom(t.price) : undefined,
                }))}
              />
            )}
          />
          {v.first_month === 'manual' && (
            <Input label={ru.studentForm.manualAmount} inputMode="decimal" suffix={ru.common.som} error={e?.manual_amount?.message} {...register('enroll.manual_amount')} />
          )}
        </>
      )}
    </div>
  )
}
