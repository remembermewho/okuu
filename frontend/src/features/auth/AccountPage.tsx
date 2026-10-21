import { zodResolver } from '@hookform/resolvers/zod'
import { ChevronLeft, ChevronRight, KeyRound, LogOut, Wallet } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link, useLocation, useNavigate } from 'react-router'
import { z } from 'zod'
import shared from '@/components/shared.module.css'
import { Badge, Button, Card, CardHeader, ConfirmDialog, Dialog, FormError, Input, PageHeader, PasswordInput, Skeleton, layout, showToast, useToast } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { applyFieldError, errorMessage } from '@/utils/errors'
import { formatPhone } from '@/utils/phone'
import { useChangePassword, useLogoutAll, useMe, usePatchMe } from './api'
import s from './AccountPage.module.css'
import { changePasswordSchema, nameField, type ChangePasswordForm } from './schemas'

const nameSchema = z.object({ name: nameField() })

/** Профиль (/account): имя, телефон (логин, только просмотр), центры и роли, смена пароля, выход везде. */
export function AccountPage() {
  const me = useMe()
  const location = useLocation()
  // Откуда пришли (из меню аккаунта центра) — туда и «Назад».
  const from = (location.state as { from?: string } | null)?.from
  const back = from && from.startsWith('/c/') ? from : '/'

  return (
    <div className={s.page}>
      <header className={s.top}>
        <Link to={back} className={s.back}>
          <ChevronLeft size={18} aria-hidden="true" />
          {ru.account.back}
        </Link>
        <span className={s.brand}>
          <span className={s.logo} aria-hidden="true">
            <Wallet size={18} />
          </span>
          {ru.app.name}
        </span>
      </header>
      <main id="main" className={s.main}>
        <PageHeader title={ru.account.title} />
        {me.data ? (
          <div className={layout.stack}>
            <ProfileCard key={me.data.name} name={me.data.name} phone={me.data.phone} />
            <Card>
              <CardHeader title={ru.account.centers} />
              <ul className={shared.list}>
                {me.data.centers.map((c) => (
                  <li key={c.id}>
                    <Link to={`/c/${c.id}`} className={s.centerRow}>
                      <span className={s.centerName}>{c.name}</span>
                      <Badge>{ru.roles[c.role]}</Badge>
                      <ChevronRight size={18} aria-hidden="true" />
                    </Link>
                  </li>
                ))}
              </ul>
            </Card>
            <PasswordCard />
            <SessionsCard />
          </div>
        ) : (
          <Skeleton height={240} />
        )}
      </main>
    </div>
  )
}

function ProfileCard({ name, phone }: { name: string; phone: string }) {
  const patch = usePatchMe()
  const toast = useToast()
  const [formError, setFormError] = useState<string>()
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isDirty },
  } = useForm<{ name: string }>({ resolver: zodResolver(nameSchema), defaultValues: { name } })

  const onSubmit = handleSubmit((v) => {
    setFormError(undefined)
    patch.mutate(v, {
      onSuccess: () => toast({ message: ru.account.nameSaved, tone: 'success' }),
      onError: (err) => {
        if (!applyFieldError(err, setError, ['name'])) setFormError(errorMessage(err))
      },
    })
  })

  return (
    <Card>
      <CardHeader title={ru.account.profile} />
      <form className={layout.stack} onSubmit={onSubmit} noValidate>
        <Input label={ru.account.name} hint={ru.account.nameHint} autoComplete="name" error={errors.name?.message} {...register('name')} />
        <Input label={ru.account.phone} hint={ru.account.phoneHint} value={formatPhone(phone)} readOnly disabled />
        <FormError>{formError}</FormError>
        <div>
          <Button type="submit" loading={patch.isPending} disabled={!isDirty}>
            {ru.common.save}
          </Button>
        </div>
      </form>
    </Card>
  )
}

function PasswordCard() {
  const [open, setOpen] = useState(false)
  return (
    <Card>
      <CardHeader title={ru.account.password} />
      <p className={layout.muted}>{ru.account.passwordText}</p>
      <Button variant="secondary" icon={<KeyRound size={18} aria-hidden="true" />} onClick={() => setOpen(true)}>
        {ru.account.changePassword}
      </Button>
      {open && <ChangePasswordDialog onClose={() => setOpen(false)} />}
    </Card>
  )
}

function ChangePasswordDialog({ onClose }: { onClose: () => void }) {
  const change = useChangePassword()
  const toast = useToast()
  const [formError, setFormError] = useState<string>()
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<ChangePasswordForm>({ resolver: zodResolver(changePasswordSchema), defaultValues: { current_password: '', new_password: '', repeat_password: '' } })

  const onSubmit = handleSubmit((v) => {
    setFormError(undefined)
    change.mutate(
      { current_password: v.current_password, new_password: v.new_password },
      {
        onSuccess: () => {
          toast({ message: ru.account.passwordChanged, tone: 'success' })
          onClose()
        },
        onError: (err) => {
          if (!applyFieldError(err, setError, ['current_password', 'new_password'])) setFormError(errorMessage(err))
        },
      },
    )
  })

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={ru.account.changePassword}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {ru.common.cancel}
          </Button>
          <Button type="submit" form="change-password" loading={change.isPending}>
            {ru.common.save}
          </Button>
        </>
      }
    >
      <form id="change-password" className={layout.stack} onSubmit={onSubmit} noValidate>
        <PasswordInput label={ru.account.currentPassword} autoComplete="current-password" error={errors.current_password?.message} {...register('current_password')} />
        <PasswordInput label={ru.account.newPassword} hint={ru.auth.passwordHint} autoComplete="new-password" error={errors.new_password?.message} {...register('new_password')} />
        <PasswordInput label={ru.account.repeatPassword} autoComplete="new-password" error={errors.repeat_password?.message} {...register('repeat_password')} />
        <FormError>{formError}</FormError>
      </form>
    </Dialog>
  )
}

function SessionsCard() {
  const logoutAll = useLogoutAll()
  const navigate = useNavigate()
  const [confirm, setConfirm] = useState(false)
  return (
    <Card>
      <CardHeader title={ru.account.sessions} />
      <p className={layout.muted}>{ru.account.sessionsText}</p>
      <Button variant="danger" icon={<LogOut size={18} aria-hidden="true" />} onClick={() => setConfirm(true)}>
        {ru.account.logoutAll}
      </Button>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={ru.account.logoutAllTitle}
        text={ru.account.logoutAllText}
        confirmLabel={ru.account.logoutAll}
        danger
        loading={logoutAll.isPending}
        onConfirm={() =>
          logoutAll.mutate(undefined, {
            onSuccess: () => {
              showToast({ message: ru.account.loggedOutAll, tone: 'success' })
              navigate('/login', { replace: true })
            },
          })
        }
      />
    </Card>
  )
}
