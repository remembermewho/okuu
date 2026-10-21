import * as Menu from '@radix-ui/react-dropdown-menu'
import { ArrowLeftRight, CircleUserRound, LogOut, MoreHorizontal, UserRound, Wallet } from 'lucide-react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router'
import { Button } from '@/components/ui'
import { useLogout, useMe } from '@/features/auth/api'
import { ru } from '@/i18n/ru'
import { centerPath, useCenter } from './center'
import { bottomItems, visibleItems } from './nav'
import s from './layout.module.css'

function useSignOut() {
  const logout = useLogout()
  const navigate = useNavigate()
  return {
    pending: logout.isPending,
    run: () => logout.mutate(undefined, { onSettled: () => navigate('/login', { replace: true }) }),
  }
}

function AccountMenu() {
  const c = useCenter()
  const me = useMe()
  const navigate = useNavigate()
  const location = useLocation()
  const signOut = useSignOut()
  return (
    <Menu.Root>
      <Menu.Trigger asChild>
        <Button variant="ghost" iconOnly aria-label={ru.nav.account}>
          <CircleUserRound size={24} aria-hidden="true" />
        </Button>
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Content className={s.menu} align="end" sideOffset={6}>
          <Menu.Label className={s.menuLabel}>
            {me.data?.name}
            {' · '}
            {ru.roles[c.role]}
          </Menu.Label>
          <Menu.Item className={s.menuItem} onSelect={() => navigate('/account', { state: { from: location.pathname } })}>
            <UserRound size={18} aria-hidden="true" />
            {ru.account.menu}
          </Menu.Item>
          {c.centersCount > 1 && (
            <Menu.Item className={s.menuItem} onSelect={() => navigate('/centers')}>
              <ArrowLeftRight size={18} aria-hidden="true" />
              {ru.nav.switchCenter}
            </Menu.Item>
          )}
          <Menu.Item className={s.menuItem} onSelect={signOut.run}>
            <LogOut size={18} aria-hidden="true" />
            {ru.nav.logout}
          </Menu.Item>
        </Menu.Content>
      </Menu.Portal>
    </Menu.Root>
  )
}

/** Каркас экранов центра: шапка, нижнее меню (телефон) или боковое (≥ 1024px). */
export function AppLayout() {
  const c = useCenter()
  const signOut = useSignOut()
  const side = visibleItems(c.role)
  const bottom = bottomItems(c.role)

  return (
    <div className={s.shell}>
      <a className={s.skip} href="#main">
        {ru.app.skipToContent}
      </a>

      <nav className={s.sidebar} aria-label={ru.nav.main}>
        <div className={s.sideBrand}>
          <span className={s.logo} aria-hidden="true">
            <Wallet size={18} />
          </span>
          {ru.app.name}
        </div>
        {side.map((i) => (
          <NavLink key={i.section} to={centerPath(c.id, i.section)} className={s.sideItem}>
            <i.icon size={20} aria-hidden="true" />
            {i.label}
          </NavLink>
        ))}
        <div className={s.sideFooter}>
          {c.centersCount > 1 && (
            <NavLink to="/centers" className={s.sideItem}>
              <ArrowLeftRight size={20} aria-hidden="true" />
              {ru.nav.switchCenter}
            </NavLink>
          )}
          <button type="button" className={`${s.sideItem} ${s.sideButton}`} onClick={signOut.run} disabled={signOut.pending}>
            <LogOut size={20} aria-hidden="true" />
            {ru.nav.logout}
          </button>
        </div>
      </nav>

      <header className={s.header}>
        <div className={s.centerName}>
          <span className={s.logo} aria-hidden="true">
            <Wallet size={18} />
          </span>
          <span>{c.name}</span>
        </div>
        <AccountMenu />
      </header>

      <main id="main" className={s.main} tabIndex={-1}>
        <Outlet />
      </main>

      <nav className={s.bottomNav} aria-label={ru.nav.main}>
        {bottom.items.map((i) => (
          <NavLink key={i.section} to={centerPath(c.id, i.section)} className={s.bottomItem}>
            <i.icon size={22} aria-hidden="true" />
            {i.label}
          </NavLink>
        ))}
        {bottom.more && (
          <NavLink to={centerPath(c.id, 'more')} className={s.bottomItem}>
            <MoreHorizontal size={22} aria-hidden="true" />
            {ru.nav.more}
          </NavLink>
        )}
      </nav>
    </div>
  )
}

export { useSignOut }
