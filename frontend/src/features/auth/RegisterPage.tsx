import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router'
import { Button, FormError, Input, PasswordInput } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { applyFieldError, errorMessage, isApiError } from '@/utils/errors'
import { useMe, useRegister } from './api'
import { AuthLayout, authStyles as s } from './AuthLayout'
import { safeNext } from './LoginPage'
import { registerSchema, type RegisterForm } from './schemas'

export function RegisterPage() {
  const [params] = useSearchParams()
  const next = safeNext(params.get('next'))
  const navigate = useNavigate()
  const me = useMe()
  const reg = useRegister()
  const [formError, setFormError] = useState<string>()
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<RegisterForm>({ resolver: zodResolver(registerSchema) })

  if (me.data) return <Navigate to={next} replace />

  const onSubmit = handleSubmit((values) => {
    setFormError(undefined)
    reg.mutate(values, {
      onSuccess: () => navigate(next, { replace: true }),
      onError: (err) => {
        if (isApiError(err, 'phone_taken')) {
          setError('phone', { type: 'server', message: errorMessage(err) }, { shouldFocus: true })
          return
        }
        if (!applyFieldError(err, setError, ['phone', 'password', 'name', 'center_name'])) setFormError(errorMessage(err))
      },
    })
  })

  return (
    <AuthLayout
      title={ru.auth.registerTitle}
      lead={ru.auth.registerLead}
      footer={
        <>
          {ru.auth.haveAccount} <Link to="/login">{ru.auth.toLogin}</Link>
        </>
      }
    >
      <form className={s.form} onSubmit={onSubmit} noValidate>
        <Input label={ru.auth.centerName} autoComplete="organization" error={errors.center_name?.message} {...register('center_name')} />
        <Input label={ru.auth.name} autoComplete="name" error={errors.name?.message} {...register('name')} />
        <Input label={ru.auth.phone} hint={ru.auth.phoneHint} type="tel" autoComplete="tel" inputMode="tel" error={errors.phone?.message} {...register('phone')} />
        <PasswordInput label={ru.auth.password} hint={ru.auth.passwordHint} autoComplete="new-password" error={errors.password?.message} {...register('password')} />
        <FormError>{formError}</FormError>
        <Button type="submit" size="lg" block loading={reg.isPending}>
          {ru.auth.submitRegister}
        </Button>
      </form>
    </AuthLayout>
  )
}
