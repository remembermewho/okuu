import { Building2, LayoutGrid, Link2, LogOut, Shield } from 'lucide-react'
import { NavLink, Outlet } from 'react-router'
import { useSignOut } from '@/app/AppLayout'
import { NotFoundPage } from '@/app/pages'
import { Button, ButtonLink, SkeletonList } from '@/components/ui'
import { useMe } from '@/features/auth/api'
import { ru } from '@/i18n/ru'
import s from './Platform.module.css'

/** /admin — только суперадмину (is_platform_admin в /me); остальным — «не найдено», как и сервер (404). */
export function RequirePlatformAdmin({ children }: { children: React.ReactNode }) {
  const me = useMe()
  if (me.isPending) return <SkeletonList rows={3} />
  if (!me.data?.is_platform_admin) return <NotFoundPage />
  return <>{children}</>
}

/** Каркас панели: своя шапка и вкладки, без меню центра. */
export function PlatformLayout() {
  const signOut = useSignOut()
  const me = useMe()
  const hasCenters = (me.data?.centers.length ?? 0) > 0
  return (
    <>
      <header className={s.header}>
        <div className={s.headerRow}>
          <div className={s.brand}>
            <span className={s.logo} aria-hidden="true">
              <Shield size={18} />
            </span>
            {ru.platform.title}
          </div>
          <div className={s.headerActions}>
            {hasCenters && (
              <ButtonLink to="/" variant="ghost" aria-label={ru.platform.toApp} icon={<LayoutGrid size={20} aria-hidden="true" />}>
                <span className={s.wideOnly}>{ru.platform.toApp}</span>
              </ButtonLink>
            )}
            <Button variant="ghost" iconOnly aria-label={ru.nav.logout} onClick={signOut.run} disabled={signOut.pending}>
              <LogOut size={20} aria-hidden="true" />
            </Button>
          </div>
        </div>
        <nav className={s.tabs} aria-label={ru.platform.navLabel}>
          <NavLink to="/admin" end className={s.tab}>
            <Building2 size={18} aria-hidden="true" />
            {ru.platform.centers}
          </NavLink>
          <NavLink to="/admin/links" className={s.tab}>
            <Link2 size={18} aria-hidden="true" />
            {ru.platform.links}
          </NavLink>
        </nav>
      </header>
      <main id="main" className={s.main} tabIndex={-1}>
        <Outlet />
      </main>
    </>
  )
}
