import { zodResolver } from '@hookform/resolvers/zod'
import { useMemo, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { z } from 'zod'
import type { Schemas } from '@/api/client'
import { useCenter } from '@/app/center'
import { Button, Dialog, FormError, Input, Select, layout, useToast } from '@/components/ui'
import { isValidPhone, normalizePhone } from '@/utils/phone'
import { ru } from '@/i18n/ru'
import { applyFieldError, errorMessage } from '@/utils/errors'
import { tyiynToInput } from '@/utils/money'
import { usePatchStudent, type Student } from '../api'
import { cheapest, checkDiscount, discountBody } from '../AddStudentDialog'

const base = z.object({
  full_name: z.string().trim().min(1, ru.errors.required).max(100, ru.errors.tooLong(100)),
  birth_date: z.string(),
  parent_name: z.string().trim().max(100, ru.errors.tooLong(100)),
  parent_phone: z.string().trim(),
  discount_kind: z.enum(['none', 'fixed', 'percent']),
  discount_value: z.string(),
})
type Values = z.infer<typeof base>

function initial(st: Student): Values {
  const d = st.discount
  return {
    full_name: st.full_name,
    birth_date: st.birth_date ?? '',
    parent_name: st.parent?.name ?? '',
    parent_phone: st.parent?.phone ?? '',
    discount_kind: d?.kind ?? 'none',
    discount_value: !d ? '' : d.kind === 'fixed' ? tyiynToInput(d.value) : String(d.value),
  }
}

/** Только изменённые поля — PATCH частичный; родитель — если поменяли имя или телефон. */
export function studentPatch(before: Values, v: Values): Schemas['PatchStudentRequest'] {
  const body: Schemas['PatchStudentRequest'] = {}
  if (v.full_name.trim() !== before.full_name) body.full_name = v.full_name.trim()
  if (v.birth_date !== before.birth_date) body.birth_date = v.birth_date || null
  const parent: NonNullable<Schemas['PatchStudentRequest']['parent']> = {}
  if (v.parent_name.trim() !== before.parent_name) parent.name = v.parent_name.trim()
  const phone = normalizePhone(v.parent_phone) ?? v.parent_phone.trim()
  if (phone !== before.parent_phone) parent.phone = phone
  if (Object.keys(parent).length) body.parent = parent
  if (v.discount_kind !== before.discount_kind || v.discount_value.trim() !== before.discount_value) {
    body.discount = discountBody(v.discount_kind, v.discount_value)
  }
  return body
}

/** Изменить данные ученика: ФИО, дата рождения, родитель (имя, телефон), скидка. */
export function EditStudentDialog({ student, onClose }: { student: Student; onClose: () => void }) {
  const c = useCenter()
  const patch = usePatchStudent(c.id, student.id)
  const toast = useToast()
  const [formError, setFormError] = useState<string>()
  const before = useMemo(() => initial(student), [student])
  // Фиксированная скидка — не больше цены самого дешёвого активного тарифа (так проверяет и сервер).
  const maxFor = cheapest(student.enrollments.filter((e) => e.active).map((e) => ({ name: e.tariff.name, price: e.tariff.price })))
  const hadParent = student.parent !== null

  const schema = useMemo(
    () =>
      base.superRefine((v, ctx) => {
        checkDiscount(v.discount_kind, v.discount_value, ctx, ['discount_value'], maxFor)
        // У ученика с родителем оба поля обязательны; без родителя — либо оба, либо ничего.
        const any = v.parent_name.trim() !== '' || v.parent_phone.trim() !== ''
        if (hadParent || any) {
          if (!v.parent_name.trim()) ctx.addIssue({ code: 'custom', path: ['parent_name'], message: ru.errors.required })
          if (!v.parent_phone.trim()) ctx.addIssue({ code: 'custom', path: ['parent_phone'], message: ru.errors.required })
          else if (!isValidPhone(v.parent_phone)) ctx.addIssue({ code: 'custom', path: ['parent_phone'], message: ru.errors.invalidPhone })
        }
      }),
    [maxFor, hadParent],
  )
  const {
    register,
    control,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<Values>({ resolver: zodResolver(schema), defaultValues: before })
  const kind = useWatch({ control, name: 'discount_kind' })

  const onSubmit = handleSubmit((v) => {
    setFormError(undefined)
    const body = studentPatch(before, v)
    if (Object.keys(body).length === 0) {
      onClose()
      return
    }
    patch.mutate(body, {
      onSuccess: () => {
        toast({ message: ru.studentEdit.saved, tone: 'success' })
        onClose()
      },
      onError: (err) => {
        const shown = applyFieldError(err, setError, {
          full_name: 'full_name',
          birth_date: 'birth_date',
          parent: 'parent_name',
          'parent.name': 'parent_name',
          'parent.phone': 'parent_phone',
          discount: 'discount_value',
          'discount.value': 'discount_value',
        })
        if (!shown) setFormError(errorMessage(err))
      },
    })
  })

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={ru.studentEdit.title}
      wide
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {ru.common.cancel}
          </Button>
          <Button type="submit" form="edit-student" loading={patch.isPending}>
            {ru.common.save}
          </Button>
        </>
      }
    >
      <form id="edit-student" className={layout.stack} onSubmit={onSubmit} noValidate>
        <div className={`${layout.formGrid} ${layout.formGrid2}`}>
          <Input label={ru.studentForm.fullName} error={errors.full_name?.message} {...register('full_name')} />
          <Input label={ru.studentForm.birthDate} type="date" optional error={errors.birth_date?.message} {...register('birth_date')} />
          <Input label={ru.studentForm.parentName} autoComplete="off" optional={!hadParent} error={errors.parent_name?.message} {...register('parent_name')} />
          <Input
            label={ru.studentForm.parentPhone}
            type="tel"
            inputMode="tel"
            optional={!hadParent}
            hint={hadParent ? ru.studentEdit.phoneHint : undefined}
            error={errors.parent_phone?.message}
            {...register('parent_phone')}
          />
          <Select
            label={ru.studentForm.discountKind}
            options={(['none', 'fixed', 'percent'] as const).map((k) => ({ value: k, label: ru.studentForm.discountKinds[k] ?? k }))}
            {...register('discount_kind')}
          />
          {kind !== 'none' && (
            <Input
              label={ru.studentForm.discountValue}
              inputMode="decimal"
              suffix={kind === 'percent' ? '%' : ru.common.som}
              hint={kind === 'percent' ? ru.studentForm.discountPercentHint : ru.studentForm.discountFixedHint}
              error={errors.discount_value?.message}
              {...register('discount_value')}
            />
          )}
        </div>
        <p className={layout.muted}>{ru.studentEdit.discountNote}</p>
        <FormError>{formError}</FormError>
      </form>
    </Dialog>
  )
}
