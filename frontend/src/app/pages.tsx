import { Building2, ChevronRight, LogOut, SearchX, Shield, UserRound } from 'lucide-react'
import { Link } from 'react-router'
import { ButtonLink, Card, EmptyState, PageHeader } from '@/components/ui'
import { AuthLayout } from '@/features/auth/AuthLayout'
import { useMe } from '@/features/auth/api'
import { ru } from '@/i18n/ru'
import { useSignOut } from './AppLayout'
import { centerPath, useCenter } from './center'
import { moreItems } from './nav'
import s from './layout.module.css'

/** Выбор центра, если пользователь сотрудник нескольких. */
export function CenterPicker() {
  const me = useMe()
  return (
    <AuthLayout title={ru.auth.chooseCenter}>
      <div className={s.morelist}>
        {me.data?.centers.map((c) => (
          <Link key={c.id} to={centerPath(c.id)} className={s.moreItem}>
            <Building2 size={20} aria-hidden="true" />
            <span>
              {c.name}
              <br />
              <small>
                {ru.roles[c.role]}
                {c.status !== 'active' && ` · ${ru.centerStatus.badge[c.status]}`}
              </small>
            </span>
            <ChevronRight size={18} aria-hidden="true" />
          </Link>
        ))}
        {me.data?.is_platform_admin && (
          <Link to="/admin" className={s.moreItem}>
            <Shield size={20} aria-hidden="true" />
            <span>{ru.platform.menu}</span>
            <ChevronRight size={18} aria-hidden="true" />
          </Link>
        )}
      </div>
    </AuthLayout>
  )
}

/** «Ещё» на телефоне — разделы, не вошедшие в нижнее меню. */
export function MorePage() {
  const c = useCenter()
  const me = useMe()
  const signOut = useSignOut()
  return (
    <>
      <PageHeader title={ru.nav.more} />
      <Card flush>
        <nav className={s.morelist} aria-label={ru.nav.more}>
          {moreItems(c.role).map((i) => (
            <Link key={i.section} to={centerPath(c.id, i.section)} className={s.moreItem}>
              <i.icon size={20} aria-hidden="true" />
              {i.label}
              <ChevronRight size={18} aria-hidden="true" />
            </Link>
          ))}
          {me.data?.is_platform_admin && (
            <Link to="/admin" className={s.moreItem}>
              <Shield size={20} aria-hidden="true" />
              {ru.platform.menu}
              <ChevronRight size={18} aria-hidden="true" />
            </Link>
          )}
          <Link to="/account" state={{ from: centerPath(c.id, 'more') }} className={s.moreItem}>
            <UserRound size={20} aria-hidden="true" />
            {ru.account.menu}
            <ChevronRight size={18} aria-hidden="true" />
          </Link>
          <button type="button" className={`${s.moreItem} ${s.moreButton}`} onClick={signOut.run}>
            <LogOut size={20} aria-hidden="true" />
            {ru.nav.logout}
          </button>
        </nav>
      </Card>
    </>
  )
}

export function NotFoundPage() {
  return (
    <div className={s.fullPage}>
      <EmptyState
        icon={<SearchX aria-hidden="true" />}
        title={ru.errors.notFound}
        text={ru.errors.notFoundText}
        action={
          <ButtonLink to="/" variant="secondary">
            {ru.errors.toHome}
          </ButtonLink>
        }
      />
    </div>
  )
}
