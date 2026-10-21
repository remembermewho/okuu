import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link, useNavigate, useParams } from 'react-router'
import { z } from 'zod'
import type { Schemas } from '@/api/client'
import { Button, ButtonLink, FormError, Input, Skeleton } from '@/components/ui'
import { useLogout, useMe } from '@/features/auth/api'
import { AuthLayout, authStyles as s } from '@/features/auth/AuthLayout'
import { nameField, passwordField, phoneField } from '@/features/auth/schemas'
import { ru } from '@/i18n/ru'
import { formatDateTime } from '@/utils/date'
import { applyFieldError, errorMessage, isApiError } from '@/utils/errors'
import { normalizePhone } from '@/utils/phone'
import { useAcceptInvite, usePublicInvite } from './api'

const schema = z.object({ name: nameField(), phone: phoneField, password: passwordField })
type Values = z.infer<typeof schema>

/** Центр, который появился после принятия (или единственный). */
function joinedCenter(before: Schemas['Me'] | undefined, after: Schemas['Me'], centerName: string): string {
  const known = new Set(before?.centers.map((c) => c.id) ?? [])
  const added = after.centers.find((c) => !known.has(c.id)) ?? after.centers.find((c) => c.name === centerName)
  return added ? `/c/${added.id}` : '/'
}

/** Публичная страница приглашения сотрудника: вошедший принимает кнопкой, новый — регистрируется. */
export function InvitePage() {
  const { token = '' } = useParams()
  const invite = usePublicInvite(token)
  const me = useMe()

  if (invite.isPending || me.isPending) {
    return (
      <AuthLayout title={ru.invite.title}>
        <Skeleton height={20} />
        <Skeleton height={44} />
      </AuthLayout>
    )
  }
  if (invite.isError) {
    const invalid = invite.error.status === 404
    return (
      <AuthLayout title={invalid ? ru.invite.invalidTitle : ru.errors.title} lead={invalid ? ru.invite.invalidText : errorMessage(invite.error)}>
        {invalid ? (
          <ButtonLink to="/" variant="secondary" block>
            {ru.auth.toLogin}
          </ButtonLink>
        ) : (
          <Button block onClick={() => void invite.refetch()}>
            {ru.common.retry}
          </Button>
        )}
      </AuthLayout>
    )
  }

  const inv = invite.data
  const lead = ru.invite.lead(inv.center_name, ru.roles[inv.role] ?? inv.role)
  return me.data ? <AcceptAsUser token={token} invite={inv} me={me.data} lead={lead} /> : <AcceptAsNew token={token} invite={inv} lead={lead} />
}

function Details({ invite }: { invite: Schemas['PublicInvite'] }) {
  return (
    <p className={s.lead}>
      {invite.name && (
        <>
          {ru.invite.forName(invite.name)}
          <br />
        </>
      )}
      {ru.invite.expires(formatDateTime(invite.expires_at))}
    </p>
  )
}

function AcceptAsUser({ token, invite, me, lead }: { token: string; invite: Schemas['PublicInvite']; me: Schemas['Me']; lead: string }) {
  const accept = useAcceptInvite(token)
  const logout = useLogout()
  const navigate = useNavigate()
  const [error, setError] = useState<string>()
  const [member, setMember] = useState(false)

  const onAccept = () => {
    setError(undefined)
    accept.mutate(undefined, {
      onSuccess: (after) => navigate(joinedCenter(me, after, invite.center_name), { replace: true }),
      onError: (err) => {
        if (isApiError(err, 'already_member')) setMember(true)
        setError(errorMessage(err))
      },
    })
  }

  const existing = me.centers.find((c) => c.name === invite.center_name)
  return (
    <AuthLayout title={ru.invite.title} lead={lead}>
      <Details invite={invite} />
      <p>{ru.invite.acceptAs(me.name)}</p>
      <FormError>{error}</FormError>
      {member && existing ? (
        <ButtonLink to={`/c/${existing.id}`} size="lg" block>
          {ru.invite.toCenter}
        </ButtonLink>
      ) : (
        <Button size="lg" block loading={accept.isPending} onClick={onAccept}>
          {ru.invite.accept}
        </Button>
      )}
      <Button variant="ghost" block loading={logout.isPending} onClick={() => logout.mutate()}>
        {ru.invite.notYou}
      </Button>
    </AuthLayout>
  )
}

function AcceptAsNew({ token, invite, lead }: { token: string; invite: Schemas['PublicInvite']; lead: string }) {
  const accept = useAcceptInvite(token)
  const navigate = useNavigate()
  const [formError, setFormError] = useState<string>()
  const [loginRequired, setLoginRequired] = useState(false)
  const next = `/invite/${token}`
  const loginHref = `/login?next=${encodeURIComponent(next)}`
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { name: invite.name ?? '', phone: '', password: '' } })

  const onSubmit = handleSubmit((v) => {
    setFormError(undefined)
    setLoginRequired(false)
    accept.mutate(
      { name: v.name, phone: normalizePhone(v.phone) ?? v.phone, password: v.password },
      {
        onSuccess: (after) => navigate(joinedCenter(undefined, after, invite.center_name), { replace: true }),
        onError: (err) => {
          if (isApiError(err, 'login_required')) {
            setLoginRequired(true)
            setError('phone', { type: 'server', message: ru.invite.loginRequired }, { shouldFocus: true })
            return
          }
          if (!applyFieldError(err, setError, ['name', 'phone', 'password'])) setFormError(errorMessage(err))
        },
      },
    )
  })

  return (
    <AuthLayout
      title={ru.invite.title}
      lead={lead}
      footer={
        <>
          {ru.invite.haveAccount} <Link to={loginHref}>{ru.invite.login}</Link>
        </>
      }
    >
      <Details invite={invite} />
      <h2 className={s.subtitle}>{ru.invite.newUser}</h2>
      <p className={s.lead}>{ru.invite.newUserLead}</p>
      <form className={s.form} onSubmit={onSubmit} noValidate>
        <Input label={ru.auth.name} autoComplete="name" error={errors.name?.message} {...register('name')} />
        <Input label={ru.auth.phone} hint={ru.auth.phoneHint} type="tel" autoComplete="tel" inputMode="tel" error={errors.phone?.message} {...register('phone')} />
        <Input label={ru.auth.password} hint={ru.auth.passwordHint} type="password" autoComplete="new-password" error={errors.password?.message} {...register('password')} />
        <FormError>{formError}</FormError>
        {loginRequired ? (
          <ButtonLink to={loginHref} size="lg" block>
            {ru.invite.login}
          </ButtonLink>
        ) : (
          <Button type="submit" size="lg" block loading={accept.isPending}>
            {ru.invite.accept}
          </Button>
        )}
      </form>
    </AuthLayout>
  )
}
