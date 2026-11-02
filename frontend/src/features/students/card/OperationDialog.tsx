import { zodResolver } from '@hookform/resolvers/zod'
import { useMemo, useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { z } from 'zod'
import { newIdempotencyKey } from '@/api/client'
import { useCenter } from '@/app/center'
import { Button, Dialog, FormError, Input, RadioGroup, Textarea, layout, useToast } from '@/components/ui'
import { useCreateOperation } from '@/features/money/api'
import { ru } from '@/i18n/ru'
import { applyFieldError, errorMessage } from '@/utils/errors'
import { formatSom, parseSom } from '@/utils/money'

export type OperationKind = 'payment' | 'refund' | 'correction'

const schema = z
  .object({
    amount: z.string(),
    method: z.enum(['cash', 'transfer', 'card']),
    direction: z.enum(['debit', 'credit']),
    comment: z.string().max(500, ru.errors.tooLong(500)),
    kind: z.enum(['payment', 'refund', 'correction']),
  })
  .superRefine((v, ctx) => {
    const t = parseSom(v.amount)
    if (t === null) ctx.addIssue({ code: 'custom', path: ['amount'], message: ru.errors.invalidAmount })
    else if (t <= 0) ctx.addIssue({ code: 'custom', path: ['amount'], message: ru.errors.amountPositive })
    if (v.kind === 'correction' && !v.comment.trim()) ctx.addIssue({ code: 'custom', path: ['comment'], message: ru.operation.commentReq })
  })
type Values = z.infer<typeof schema>

type Method = 'cash' | 'transfer' | 'card'

type OperationDialogProps = {
  kind: OperationKind | null
  studentId: string
  balance: number
  onClose: () => void
  /** Способ оплаты по умолчанию: в карточке — наличные, в «Должниках» — перевод (родитель платит по реквизитам). */
  defaultMethod?: Method
  /** Подпись под заголовком (например, ФИО ученика в «Должниках»). */
  description?: string
  /** Свой тост после сохранения вместо стандартного. */
  onDone?: () => void
}

/**
 * Оплата / возврат / поправка. Idempotency-Key создаётся при открытии формы и живёт до её закрытия:
 * повторное нажатие или повтор после обрыва связи не создаст вторую операцию.
 * Один диалог для карточки ученика и быстрой отметки оплаты из «Должников».
 */
export function OperationDialog(props: OperationDialogProps) {
  return props.kind !== null ? <OperationForm {...props} kind={props.kind} /> : null
}

function OperationForm({ kind, studentId, balance, onClose, defaultMethod = 'cash', description, onDone }: OperationDialogProps & { kind: OperationKind }) {
  const c = useCenter()
  const create = useCreateOperation(c.id, studentId)
  const toast = useToast()
  // Один ключ на открытие формы (компонент монтируется заново при каждом открытии).
  const [key] = useState(newIdempotencyKey)
  const [formError, setFormError] = useState<string>()
  const debt = balance < 0 ? -balance : 0

  const defaults = useMemo<Values>(
    () => ({ amount: kind === 'payment' && debt > 0 ? String(Math.round(debt / 100)) : '', method: defaultMethod, direction: 'credit', comment: '', kind }),
    [kind, debt, defaultMethod],
  )
  const {
    register,
    control,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<Values>({ resolver: zodResolver(schema), defaultValues: defaults })

  const onSubmit = handleSubmit((v) => {
    setFormError(undefined)
    const tyiyn = parseSom(v.amount) ?? 0
    const body =
      kind === 'correction'
        ? { kind, amount: v.direction === 'debit' ? -tyiyn : tyiyn, comment: v.comment.trim() }
        : { kind, amount: tyiyn, method: v.method, comment: v.comment.trim() || null }
    create.mutate(
      { body, key },
      {
        onSuccess: () => {
          if (onDone) onDone()
          else toast({ message: ru.operation.done[kind] ?? '', tone: 'success' })
          onClose()
        },
        onError: (err) => {
          if (!applyFieldError(err, setError, ['amount', 'method', 'comment'])) setFormError(errorMessage(err))
        },
      },
    )
  })

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={ru.operation.titles[kind]}
      description={description ?? (kind === 'refund' ? ru.operation.refundHint : kind === 'correction' ? ru.operation.correctionHint : undefined)}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {ru.common.cancel}
          </Button>
          <Button type="submit" form="operation-form" loading={create.isPending}>
            {ru.operation.submit[kind]}
          </Button>
        </>
      }
    >
      <form id="operation-form" className={layout.stack} onSubmit={onSubmit} noValidate>
        {kind === 'correction' && (
          <Controller
            control={control}
            name="direction"
            render={({ field }) => (
              <RadioGroup
                legend={ru.operation.direction}
                name="direction"
                value={field.value}
                onChange={field.onChange}
                options={(['debit', 'credit'] as const).map((d) => ({ value: d, label: ru.operation.directions[d] }))}
              />
            )}
          />
        )}
        <Input
          label={ru.operation.amount}
          inputMode="decimal"
          autoFocus
          suffix={ru.common.som}
          hint={kind === 'payment' && debt > 0 ? `${ru.student.balanceDebt}: ${formatSom(debt)}` : undefined}
          error={errors.amount?.message}
          {...register('amount')}
        />
        {kind !== 'correction' && (
          <Controller
            control={control}
            name="method"
            render={({ field }) => (
              <RadioGroup
                legend={ru.operation.method}
                name="method"
                value={field.value}
                onChange={field.onChange}
                options={(['cash', 'transfer', 'card'] as const).map((m) => ({ value: m, label: ru.operation.methods[m] }))}
              />
            )}
          />
        )}
        <Textarea
          label={kind === 'correction' ? ru.operation.commentRequired : ru.operation.comment}
          optional={kind !== 'correction'}
          rows={2}
          error={errors.comment?.message}
          {...register('comment')}
        />
        <FormError>{formError}</FormError>
      </form>
    </Dialog>
  )
}
