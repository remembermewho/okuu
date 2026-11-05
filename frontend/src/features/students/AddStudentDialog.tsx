import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Controller, FormProvider, useForm, useWatch } from 'react-hook-form'
import { useNavigate } from 'react-router'
import { z } from 'zod'
import type { Schemas } from '@/api/client'
import { centerPath, useCenter } from '@/app/center'
import { Button, Checkbox, Dialog, FormError, Input, Select, layout, useToast } from '@/components/ui'
import { useGroups, useTariffs } from '@/features/catalog/api'
import { nameField, phoneField } from '@/features/auth/schemas'
import { ru } from '@/i18n/ru'
import { applyFieldError, errorMessage } from '@/utils/errors'
import { formatSom, parseSom } from '@/utils/money'
import { normalizePhone } from '@/utils/phone'
import { useCreateStudent, useEnroll, useParentLookup } from './api'

type FoundParent = NonNullable<Schemas['ParentLookup']['parent']>

import { checkEnroll, effectiveTariff, emptyEnroll, enrollBody, EnrollFields, enrollFields } from './enroll'

const base = z.object({
  full_name: nameField(),
  birth_date: z.string(),
  parent_name: z.string(), // обязательно, если родителя с этим телефоном ещё нет (superRefine)
  parent_phone: phoneField,
  discount_kind: z.enum(['none', 'fixed', 'percent']),
  discount_value: z.string(),
  with_enroll: z.boolean(),
  enroll: enrollFields,
})
type Values = z.infer<typeof base>

/**
 * Скидка формы → скидка API (fixed — в тыйынах, percent — 0..100). Не число — value NaN: такую форма
 * не отправит (checkDiscount), а подсказка суммы её не покажет (discountFits).
 */
export function discountBody(kind: string, value: string): Schemas['Discount'] | null {
  if (kind === 'fixed') return { kind: 'fixed', value: parseSom(value) ?? NaN }
  if (kind === 'percent') return { kind: 'percent', value: /^\d+$/.test(value.trim()) ? Number(value) : NaN }
  return null
}

/**
 * Проверка скидки до отправки: процент 0–100; фиксированная — сумма в сомах, не больше цены тарифов
 * (maxFor — самый дешёвый из выбранных / активных тарифов ученика; как на бэкенде).
 */
export function checkDiscount(kind: string, value: string, ctx: z.RefinementCtx, path: string[], maxFor?: { name: string; price: number }) {
  if (kind === 'fixed') {
    const t = parseSom(value)
    if (t === null || t < 0) ctx.addIssue({ code: 'custom', path, message: ru.errors.invalidAmount })
    else if (maxFor && t > maxFor.price) ctx.addIssue({ code: 'custom', path, message: ru.studentForm.discountFixedMax(formatSom(maxFor.price), maxFor.name) })
  }
  if (kind === 'percent') {
    const n = Number(value)
    if (!/^\d+$/.test(value.trim()) || n < 0 || n > 100) ctx.addIssue({ code: 'custom', path, message: ru.errors.range(0, 100) })
  }
}

/** Самый дешёвый тариф — его цена ограничивает фиксированную скидку. */
export function cheapest(tariffs: { name: string; price: number }[]): { name: string; price: number } | undefined {
  return tariffs.reduce<{ name: string; price: number } | undefined>((min, t) => (!min || t.price < min.price ? t : min), undefined)
}

export function AddStudentDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const c = useCenter()
  const groups = useGroups(c.id)
  const tariffs = useTariffs(c.id)
  const create = useCreateStudent(c.id)
  const enroll = useEnroll(c.id)
  const toast = useToast()
  const navigate = useNavigate()
  const [formError, setFormError] = useState<string>()
  // Существующий родитель: телефон ищем после ухода из поля (с задержкой), подсказка — только если найден.
  const [lookupPhone, setLookupPhone] = useState<string | null>(null)
  const lookup = useParentLookup(c.id, lookupPhone)
  const lookupTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(() => () => clearTimeout(lookupTimer.current), [])
  const foundRef = useRef<FoundParent | null>(null)

  const schema = useMemo(
    () =>
      base.superRefine((v, ctx) => {
        if (!foundRef.current) {
          const name = nameField().safeParse(v.parent_name)
          if (!name.success) ctx.addIssue({ code: 'custom', path: ['parent_name'], message: name.error.issues[0]?.message ?? ru.errors.required })
        }
        const t = v.with_enroll ? effectiveTariff(v.enroll, groups.data ?? [], tariffs.data ?? []) : undefined
        checkDiscount(v.discount_kind, v.discount_value, ctx, ['discount_value'], t)
        if (v.with_enroll) checkEnroll(v.enroll, groups.data ?? [], tariffs.data ?? [], ctx, ['enroll'])
      }),
    [groups.data, tariffs.data],
  )

  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      full_name: '',
      birth_date: '',
      parent_name: '',
      parent_phone: '',
      discount_kind: 'none',
      discount_value: '',
      with_enroll: true,
      enroll: emptyEnroll(c.today),
    },
  })
  const {
    register,
    control,
    handleSubmit,
    setError,
    setValue,
    getValues,
    clearErrors,
    reset,
    formState: { errors },
  } = form
  const discountKind = useWatch({ control, name: 'discount_kind' })
  const discountValue = useWatch({ control, name: 'discount_value' })
  const withEnroll = useWatch({ control, name: 'with_enroll' })
  const phoneNow = useWatch({ control, name: 'parent_phone' })
  // Найденный родитель действует, пока телефон в поле — тот же, что искали.
  const found = lookupPhone !== null && normalizePhone(phoneNow) === lookupPhone ? (lookup.data?.parent ?? null) : null
  foundRef.current = found
  useEffect(() => {
    if (!found) return
    if (!getValues('parent_name').trim()) setValue('parent_name', found.name)
    clearErrors('parent_name')
  }, [found, getValues, setValue, clearErrors])
  const phoneInput = register('parent_phone')
  const onPhoneBlur: typeof phoneInput.onBlur = (e) => {
    const phone = normalizePhone(e.target.value)
    clearTimeout(lookupTimer.current)
    lookupTimer.current = setTimeout(() => setLookupPhone(phone), 300)
    return phoneInput.onBlur(e)
  }

  const close = (o: boolean) => {
    if (!o) {
      reset()
      setFormError(undefined)
      setLookupPhone(null)
    }
    onOpenChange(o)
  }

  const onSubmit = handleSubmit(async (v) => {
    setFormError(undefined)
    let st: Schemas['CreatedStudent']
    try {
      st = await create.mutateAsync({
        full_name: v.full_name,
        birth_date: v.birth_date || null,
        parent: { name: v.parent_name.trim() || (found?.name ?? ''), phone: normalizePhone(v.parent_phone) ?? v.parent_phone },
        discount: discountBody(v.discount_kind, v.discount_value),
      })
    } catch (err) {
      const shown = applyFieldError(err, setError, {
        full_name: 'full_name',
        birth_date: 'birth_date',
        'parent.name': 'parent_name',
        'parent.phone': 'parent_phone',
        discount: 'discount_value',
        'discount.value': 'discount_value',
      })
      if (!shown) setFormError(errorMessage(err))
      return
    }
    if (v.with_enroll) {
      try {
        await enroll.mutateAsync({ studentId: st.id, body: enrollBody(v.enroll, groups.data ?? [], tariffs.data ?? []) })
      } catch (err) {
        toast({ message: `${ru.studentForm.enrollFailed}: ${errorMessage(err)}`, tone: 'error' })
      }
    }
    toast({ message: st.parent_existing ? ru.studentForm.createdParentExisting : ru.studentForm.created, tone: 'success' })
    close(false)
    navigate(centerPath(c.id, `students/${st.id}`))
  })

  return (
    <Dialog
      open={open}
      onOpenChange={close}
      title={ru.studentForm.title}
      wide
      footer={
        <>
          <Button variant="secondary" onClick={() => close(false)}>
            {ru.common.cancel}
          </Button>
          <Button type="submit" form="add-student" loading={create.isPending || enroll.isPending}>
            {ru.studentForm.submit}
          </Button>
        </>
      }
    >
      <FormProvider {...form}>
        <form id="add-student" className={layout.stack} onSubmit={onSubmit} noValidate>
          <div className={`${layout.formGrid} ${layout.formGrid2}`}>
            <Input label={ru.studentForm.fullName} hint={ru.studentForm.fullNameHint} error={errors.full_name?.message} {...register('full_name')} />
            <Input label={ru.studentForm.birthDate} type="date" optional error={errors.birth_date?.message} {...register('birth_date')} />
            <Input
              label={ru.studentForm.parentName}
              autoComplete="off"
              optional={found !== null}
              hint={found ? ru.studentForm.parentNameFound : undefined}
              error={errors.parent_name?.message}
              {...register('parent_name')}
            />
            <Input
              label={ru.studentForm.parentPhone}
              type="tel"
              inputMode="tel"
              hint={found ? ru.studentForm.parentFound(found.name, found.children) : undefined}
              error={errors.parent_phone?.message}
              {...phoneInput}
              onBlur={onPhoneBlur}
            />
            <Select
              label={ru.studentForm.discountKind}
              options={(['none', 'fixed', 'percent'] as const).map((k) => ({ value: k, label: ru.studentForm.discountKinds[k] ?? k }))}
              {...register('discount_kind')}
            />
            {discountKind !== 'none' && (
              <Input
                label={ru.studentForm.discountValue}
                inputMode="decimal"
                suffix={discountKind === 'percent' ? '%' : ru.common.som}
                hint={discountKind === 'percent' ? ru.studentForm.discountPercentHint : ru.studentForm.discountFixedHint}
                error={errors.discount_value?.message}
                {...register('discount_value')}
              />
            )}
          </div>
          <Controller
            control={control}
            name="with_enroll"
            render={({ field }) => (
              <Checkbox label={ru.studentForm.enroll} hint={ru.studentForm.enrollHint} checked={field.value} onChange={(e) => field.onChange(e.target.checked)} />
            )}
          />
          {withEnroll && <EnrollFields groups={groups.data ?? []} tariffs={tariffs.data ?? []} discount={discountBody(discountKind, discountValue)} checkDiscount />}
          <FormError>{formError}</FormError>
        </form>
      </FormProvider>
    </Dialog>
  )
}
