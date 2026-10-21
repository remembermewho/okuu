import { zodResolver } from '@hookform/resolvers/zod'
import { CheckCircle2 } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useParams } from 'react-router'
import { Button, ButtonLink, FormError, PasswordInput, Skeleton } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { applyFieldError, errorMessage, isApiError } from '@/utils/errors'
import { useCompletePasswordReset, usePasswordReset } from './api'
import { AuthLayout, authStyles as s } from './AuthLayout'
import { resetPasswordSchema, type ResetPasswordForm } from './schemas'

/** Публичная страница /reset/:token — новый пароль по одноразовой ссылке (без входа). */
export function ResetPasswordPage() {
  const { token = '' } = useParams()
  const reset = usePasswordReset(token)
  const complete = useCompletePasswordReset(token)
  const [done, setDone] = useState(false)
  const [formError, setFormError] = useState<string>()
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<ResetPasswordForm>({ resolver: zodResolver(resetPasswordSchema), defaultValues: { new_password: '', repeat_password: '' } })

  // «Готово» — раньше проверки ссылки: после смены пароля она уже использована.
  if (done) {
    return (
      <AuthLayout title={ru.reset.doneTitle} lead={ru.reset.doneText}>
        <CheckCircle2 size={40} aria-hidden="true" className={s.okIcon} />
        <ButtonLink to="/login" size="lg" block>
          {ru.reset.toLogin}
        </ButtonLink>
      </AuthLayout>
    )
  }
  const invalid = (reset.isError && reset.error.status === 404) || isApiError(complete.error, 'reset_invalid')
  if (invalid) {
    return (
      <AuthLayout title={ru.reset.invalidTitle} lead={ru.reset.invalidText}>
        <ButtonLink to="/login" variant="secondary" block>
          {ru.reset.toLogin}
        </ButtonLink>
      </AuthLayout>
    )
  }
  if (reset.isPending) {
    return (
      <AuthLayout title={ru.reset.title}>
        <Skeleton height={20} />
        <Skeleton height={44} />
      </AuthLayout>
    )
  }
  if (reset.isError) {
    return (
      <AuthLayout title={ru.errors.title} lead={errorMessage(reset.error)}>
        <Button block onClick={() => void reset.refetch()}>
          {ru.common.retry}
        </Button>
      </AuthLayout>
    )
  }

  const onSubmit = handleSubmit((v) => {
    setFormError(undefined)
    complete.mutate(
      { new_password: v.new_password },
      {
        onSuccess: () => setDone(true),
        onError: (err) => {
          if (isApiError(err, 'reset_invalid')) return
          if (!applyFieldError(err, setError, ['new_password'])) setFormError(errorMessage(err))
        },
      },
    )
  })

  return (
    <AuthLayout title={ru.reset.title} lead={ru.reset.lead(reset.data.name, reset.data.center_name)}>
      <p className={s.lead}>{ru.reset.text}</p>
      <form className={s.form} onSubmit={onSubmit} noValidate>
        <PasswordInput label={ru.account.newPassword} hint={ru.auth.passwordHint} autoComplete="new-password" error={errors.new_password?.message} {...register('new_password')} />
        <PasswordInput label={ru.account.repeatPassword} autoComplete="new-password" error={errors.repeat_password?.message} {...register('repeat_password')} />
        <FormError>{formError}</FormError>
        <Button type="submit" size="lg" block loading={complete.isPending}>
          {ru.reset.submit}
        </Button>
      </form>
    </AuthLayout>
  )
}
