import { zodResolver } from '@hookform/resolvers/zod'
import { MessageCircle } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link, Navigate, useNavigate, useParams, useSearchParams } from 'react-router'
import { Button, ButtonAnchor, ButtonLink, FormError, Input, PasswordInput, Skeleton } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { applyFieldError, errorMessage, isApiError } from '@/utils/errors'
import { whatsappUrl } from '@/utils/whatsapp'
import { useMe, useRegister, useRegistrationInfo, useRegistrationLink } from './api'
import { AuthLayout, authStyles as s } from './AuthLayout'
import { safeNext } from './LoginPage'
import { registerSchema, type RegisterForm } from './schemas'

/**
 * /register и /register/:token. Режим invite без ссылки — «Регистрация по приглашению» и WhatsApp;
 * недействительная ссылка — «Ссылка недействительна»; approval — форма с пометкой, что заявку проверят.
 */
export function RegisterPage() {
  const { token = '' } = useParams()
  const [params] = useSearchParams()
  const next = safeNext(params.get('next'))
  const me = useMe()
  const info = useRegistrationInfo()
  const link = useRegistrationLink(token)
  const [closed, setClosed] = useState(false) // сервер ответил registration_closed / registration_invalid

  if (me.data) return <Navigate to={next} replace />
  if (info.isPending || (token && link.isPending)) {
    return (
      <AuthLayout title={ru.auth.registerTitle}>
        <Skeleton height={44} />
        <Skeleton height={44} />
      </AuthLayout>
    )
  }
  if (info.isError) {
    return (
      <AuthLayout title={ru.errors.title} lead={errorMessage(info.error)}>
        <Button block onClick={() => void info.refetch()}>
          {ru.common.retry}
        </Button>
      </AuthLayout>
    )
  }
  const support = info.data.support_whatsapp
  if (token && (link.isError || closed)) return <ClosedScreen title={ru.auth.registerLinkInvalidTitle} text={ru.auth.registerLinkInvalidText} support={support} />
  if ((!token && info.data.mode === 'invite') || closed) return <ClosedScreen title={ru.auth.inviteOnlyTitle} text={ru.auth.inviteOnlyText} support={support} />

  return <RegisterForm token={token} next={next} approval={!token && info.data.mode === 'approval'} onClosed={() => setClosed(true)} />
}

function ClosedScreen({ title, text, support }: { title: string; text: string; support: string | null }) {
  return (
    <AuthLayout title={title} lead={text}>
      <ButtonAnchor href={whatsappUrl(support, ru.auth.whatsappRequestText)} target="_blank" rel="noopener" size="lg" block icon={<MessageCircle size={20} />}>
        {ru.auth.writeWhatsApp}
      </ButtonAnchor>
      <ButtonLink to="/login" variant="secondary" block>
        {ru.auth.toLogin}
      </ButtonLink>
    </AuthLayout>
  )
}

function RegisterForm({ token, next, approval, onClosed }: { token: string; next: string; approval: boolean; onClosed: () => void }) {
  const navigate = useNavigate()
  const reg = useRegister()
  const [formError, setFormError] = useState<string>()
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<RegisterForm>({ resolver: zodResolver(registerSchema) })

  const onSubmit = handleSubmit((values) => {
    setFormError(undefined)
    reg.mutate(token ? { ...values, registration_token: token } : values, {
      onSuccess: () => navigate(next, { replace: true }),
      onError: (err) => {
        if (isApiError(err, 'registration_closed') || isApiError(err, 'registration_invalid')) {
          onClosed()
          return
        }
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
      lead={approval ? ru.auth.approvalLead : ru.auth.registerLead}
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
          {approval ? ru.auth.submitApplication : ru.auth.submitRegister}
        </Button>
      </form>
    </AuthLayout>
  )
}
