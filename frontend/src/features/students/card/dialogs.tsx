import { zodResolver } from '@hookform/resolvers/zod'
import { MessageCircle } from 'lucide-react'
import { useMemo, useState } from 'react'
import { FormProvider, useForm } from 'react-hook-form'
import { z } from 'zod'
import type { Schemas } from '@/api/client'
import { useCenter } from '@/app/center'
import { CopyButton } from '@/components/shared'
import { Button, ButtonAnchor, Dialog, FormError, Input, Textarea, layout, useToast } from '@/components/ui'
import { useGroups, useTariffs } from '@/features/catalog/api'
import { useCancelOperation } from '@/features/money/api'
import { ru } from '@/i18n/ru'
import { formatDateTime, todayISO } from '@/utils/date'
import { applyFieldError, errorMessage } from '@/utils/errors'
import { useEnroll } from '../api'
import { checkEnroll, emptyEnroll, enrollBody, EnrollFields, enrollFields } from '../enroll'
import s from './StudentPage.module.css'

/** Отмена операции — причина обязательна (R15). */
export function CancelOperationDialog({ ledgerId, onClose }: { ledgerId: string | null; onClose: () => void }) {
  const c = useCenter()
  const cancel = useCancelOperation(c.id)
  const toast = useToast()
  const [reason, setReason] = useState('')
  const [error, setError] = useState<string>()

  const close = () => {
    setReason('')
    setError(undefined)
    onClose()
  }
  const submit = () => {
    if (!ledgerId) return
    if (reason.trim().length < 3) {
      setError(ru.student.reasonMin)
      return
    }
    cancel.mutate(
      { ledgerId, reason: reason.trim() },
      {
        onSuccess: () => {
          toast({ message: ru.student.cancelDone, tone: 'success' })
          close()
        },
        onError: (err) => setError(errorMessage(err)),
      },
    )
  }

  return (
    <Dialog
      open={ledgerId !== null}
      onOpenChange={(o) => !o && close()}
      title={ru.student.cancelTitle}
      description={ru.student.cancelText}
      footer={
        <>
          <Button variant="secondary" onClick={close}>
            {ru.common.cancel}
          </Button>
          <Button variant="danger" loading={cancel.isPending} onClick={submit}>
            {ru.student.cancel}
          </Button>
        </>
      }
    >
      <Textarea label={ru.student.cancelReason} hint={ru.student.cancelReasonHint} rows={3} value={reason} onChange={(e) => setReason(e.target.value)} error={error} autoFocus />
    </Dialog>
  )
}

/** Приглашение родителя в Telegram-бот: ссылка, «Скопировать», «Отправить в WhatsApp». */
export function InviteParentDialog({ invite, onClose }: { invite: Schemas['ParentInvite'] | null; onClose: () => void }) {
  const [lang, setLang] = useState<'ru' | 'ky'>('ru')
  if (!invite) return null
  const text = invite.texts[lang]
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={ru.student.inviteTitle}
      description={ru.student.inviteLead}
      footer={<Button onClick={onClose}>{ru.common.close}</Button>}
    >
      <div className={layout.stack}>
        <Input label={ru.student.inviteLink} value={invite.bot_url} readOnly hint={ru.student.inviteExpires(formatDateTime(invite.expires_at))} onFocus={(e) => e.currentTarget.select()} />
        <div className={s.actions}>
          <CopyButton text={invite.bot_url} label={ru.student.copyLink} variant="primary" />
          <ButtonAnchor variant="secondary" href={invite.whatsapp_url} target="_blank" rel="noopener noreferrer" icon={<MessageCircle size={18} aria-hidden="true" />}>
            {ru.student.whatsapp}
          </ButtonAnchor>
        </div>
        <div className={s.langSwitch} role="group" aria-label={ru.student.inviteLangLabel}>
          {(['ru', 'ky'] as const).map((l) => (
            <Button key={l} size="sm" variant={lang === l ? 'primary' : 'secondary'} aria-pressed={lang === l} onClick={() => setLang(l)}>
              {ru.student.inviteLang[l]}
            </Button>
          ))}
        </div>
        <Textarea label={ru.student.inviteText} value={text} readOnly rows={5} />
        <CopyButton text={text} label={ru.student.copyText} />
      </div>
    </Dialog>
  )
}

const enrollSchema = z.object({ enroll: enrollFields })

/** Записать ученика в группу (из карточки). */
export function EnrollDialog({ studentId, open, onClose }: { studentId: string; open: boolean; onClose: () => void }) {
  const c = useCenter()
  const groups = useGroups(c.id)
  const tariffs = useTariffs(c.id)
  const enroll = useEnroll(c.id)
  const toast = useToast()
  const [formError, setFormError] = useState<string>()
  const schema = useMemo(() => enrollSchema.superRefine((v, ctx) => checkEnroll(v.enroll, groups.data ?? [], tariffs.data ?? [], ctx, ['enroll'])), [groups.data, tariffs.data])
  const form = useForm<{ enroll: z.infer<typeof enrollFields> }>({ resolver: zodResolver(schema), defaultValues: { enroll: emptyEnroll(todayISO()) } })

  const close = () => {
    form.reset({ enroll: emptyEnroll(todayISO()) })
    setFormError(undefined)
    onClose()
  }
  const onSubmit = form.handleSubmit((v) => {
    setFormError(undefined)
    enroll.mutate(
      { studentId, body: enrollBody(v.enroll, groups.data ?? [], tariffs.data ?? []) },
      {
        onSuccess: () => {
          toast({ message: ru.student.enrolled, tone: 'success' })
          close()
        },
        onError: (err) => {
          const shown = applyFieldError(err, form.setError, {
            group_id: 'enroll.group_id',
            tariff_id: 'enroll.tariff_id',
            start_date: 'enroll.start_date',
            manual_amount: 'enroll.manual_amount',
          })
          if (!shown) setFormError(errorMessage(err))
        },
      },
    )
  })

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && close()}
      title={ru.student.enrollTitle}
      wide
      footer={
        <>
          <Button variant="secondary" onClick={close}>
            {ru.common.cancel}
          </Button>
          <Button type="submit" form="enroll-form" loading={enroll.isPending}>
            {ru.student.addEnrollment}
          </Button>
        </>
      }
    >
      <FormProvider {...form}>
        <form id="enroll-form" className={layout.stack} onSubmit={onSubmit} noValidate>
          <EnrollFields groups={groups.data ?? []} tariffs={tariffs.data ?? []} />
          <FormError>{formError}</FormError>
        </form>
      </FormProvider>
    </Dialog>
  )
}
