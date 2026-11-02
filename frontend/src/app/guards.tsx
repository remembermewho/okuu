import { Navigate, Outlet, useLocation, useParams } from 'react-router'
import { Hourglass, Lock, LogOut, MessageCircle } from 'lucide-react'
import { Button, ButtonAnchor, ButtonLink, EmptyState, ErrorState, SkeletonList } from '@/components/ui'
import { AuthLayout } from '@/features/auth/AuthLayout'
import { useMe, useRegistrationInfo } from '@/features/auth/api'
import { ru } from '@/i18n/ru'
import { errorMessage, isApiError } from '@/utils/errors'
import { whatsappUrl } from '@/utils/whatsapp'
import { useSignOut } from './AppLayout'
import { CenterContext, centerPath, useCenter } from './center'
import { NotFoundPage } from './pages'
import { canSee, homeSection, type Section } from './permissions'
import s from './layout.module.css'

function FullPageLoading() {
  return (
    <div className={s.fullPage}>
      <SkeletonList rows={3} />
    </div>
  )
}

/** Маршруты для вошедших: без сессии — на вход с возвратом. */
export function RequireAuth() {
  const me = useMe()
  const location = useLocation()
  if (me.isPending) return <FullPageLoading />
  if (me.isError) {
    if (isApiError(me.error) && me.error.status === 401) {
      const next = location.pathname + location.search
      return <Navigate to={'/login' + (next !== '/' ? '?next=' + encodeURIComponent(next) : '')} replace />
    }
    return (
      <div className={s.fullPage}>
        <ErrorState message={errorMessage(me.error)} onRetry={() => void me.refetch()} />
      </div>
    )
  }
  return <Outlet />
}

/** «/» — в единственный центр, к выбору центра или «нет центров». */
export function HomeRedirect() {
  const me = useMe()
  const centers = me.data?.centers ?? []
  if (centers.length === 1 && centers[0]) return <Navigate to={centerPath(centers[0].id)} replace />
  if (centers.length > 1) return <Navigate to="/centers" replace />
  if (me.data?.is_platform_admin) return <Navigate to="/admin" replace /> // суперадмин без своего центра
  return (
    <div className={s.fullPage}>
      <EmptyState icon={<Lock aria-hidden="true" />} title={ru.auth.noCentersTitle} text={ru.auth.noCentersText} />
    </div>
  )
}

/**
 * /c/:centerId — центр из /me; чужой (или несуществующий) центр — «Не найдено» с кнопкой «На главную»,
 * без молчаливого перехода; заявка или приостановленный — экран статуса.
 */
export function CenterScope({ children }: { children: React.ReactNode }) {
  const { centerId } = useParams()
  const me = useMe()
  const centers = me.data?.centers ?? []
  const c = centers.find((x) => x.id === centerId)
  if (!c) return <NotFoundPage />
  if (c.status !== 'active') return <CenterStatusScreen name={c.name} status={c.status} manyCenters={centers.length > 1} />
  return <CenterContext.Provider value={{ id: c.id, name: c.name, role: c.role, centersCount: centers.length, today: c.today }}>{children}</CenterContext.Provider>
}

/** «Заявка на рассмотрении» / «Доступ приостановлен»: данные центра недоступны, /me и выход работают. */
function CenterStatusScreen({ name, status, manyCenters }: { name: string; status: 'pending' | 'blocked'; manyCenters: boolean }) {
  const me = useMe()
  const info = useRegistrationInfo()
  const signOut = useSignOut()
  const pending = status === 'pending'
  const text = pending ? ru.centerStatus.whatsappPending(name) : ru.centerStatus.whatsappBlocked(name)
  return (
    <AuthLayout
      title={pending ? ru.centerStatus.pendingTitle : ru.centerStatus.blockedTitle}
      lead={pending ? ru.centerStatus.pendingText(name) : ru.centerStatus.blockedText(name)}
    >
      {pending ? <Hourglass size={40} aria-hidden="true" className={s.statusIcon} /> : <Lock size={40} aria-hidden="true" className={s.statusIcon} />}
      <Button size="lg" block loading={me.isFetching} onClick={() => void me.refetch()}>
        {ru.centerStatus.checkAgain}
      </Button>
      <ButtonAnchor href={whatsappUrl(info.data?.support_whatsapp, text)} target="_blank" rel="noopener" variant="secondary" block icon={<MessageCircle size={20} />}>
        {ru.auth.writeWhatsApp}
      </ButtonAnchor>
      {manyCenters && (
        <ButtonLink to="/centers" variant="secondary" block>
          {ru.centerStatus.otherCenters}
        </ButtonLink>
      )}
      <Button variant="ghost" block icon={<LogOut size={20} />} onClick={signOut.run}>
        {ru.nav.logout}
      </Button>
    </AuthLayout>
  )
}

/** /c/:centerId/ — первый экран по роли. */
export function CenterHome() {
  const c = useCenter()
  return <Navigate to={centerPath(c.id, homeSection(c.role))} replace />
}

/** Раздел только для ролей, которым он доступен; остальным — «нет доступа». */
export function RequireSection({ section, children }: { section: Section; children: React.ReactNode }) {
  const c = useCenter()
  if (!canSee(c.role, section)) return <NoAccess />
  return <>{children}</>
}

export function NoAccess() {
  return <EmptyState icon={<Lock aria-hidden="true" />} title={ru.errors.noAccess} text={ru.errors.noAccessText} />
}
