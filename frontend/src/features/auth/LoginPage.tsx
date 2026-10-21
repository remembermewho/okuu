import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router'
import { Button, FormError, Input } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { applyFieldError, errorMessage } from '@/utils/errors'
import { useLogin, useMe } from './api'
import { AuthLayout, authStyles as s } from './AuthLayout'
import { loginSchema, type LoginForm } from './schemas'

/** Куда вернуть после входа: только свои пути, без открытого редиректа. */
export function safeNext(next: string | null): string {
  return next && next.startsWith('/') && !next.startsWith('//') ? next : '/'
}

export function LoginPage() {
  const [params] = useSearchParams()
  const next = safeNext(params.get('next'))
  const navigate = useNavigate()
  const me = useMe()
  const login = useLogin()
  const [formError, setFormError] = useState<string>()
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<LoginForm>({ resolver: zodResolver(loginSchema) })

  if (me.data) return <Navigate to={next} replace />

  const onSubmit = handleSubmit((values) => {
    setFormError(undefined)
    login.mutate(values, {
      onSuccess: () => navigate(next, { replace: true }),
      onError: (err) => {
        if (!applyFieldError(err, setError, ['phone', 'password'])) setFormError(errorMessage(err))
      },
    })
  })

  return (
    <AuthLayout
      title={ru.auth.loginTitle}
      lead={ru.auth.loginLead}
      footer={
        <>
          {ru.auth.noAccount} <Link to={'/register' + (next !== '/' ? '?next=' + encodeURIComponent(next) : '')}>{ru.auth.toRegister}</Link>
        </>
      }
    >
      <form className={s.form} onSubmit={onSubmit} noValidate>
        <Input label={ru.auth.phone} hint={ru.auth.phoneHint} type="tel" autoComplete="tel" inputMode="tel" error={errors.phone?.message} {...register('phone')} />
        <Input label={ru.auth.password} type="password" autoComplete="current-password" error={errors.password?.message} {...register('password')} />
        <FormError>{formError}</FormError>
        <Button type="submit" size="lg" block loading={login.isPending}>
          {ru.auth.submitLogin}
        </Button>
      </form>
    </AuthLayout>
  )
}
