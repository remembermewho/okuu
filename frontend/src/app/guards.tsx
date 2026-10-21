import { Navigate, Outlet, useLocation, useParams } from 'react-router'
import { Lock } from 'lucide-react'
import { EmptyState, ErrorState, SkeletonList } from '@/components/ui'
import { useMe } from '@/features/auth/api'
import { ru } from '@/i18n/ru'
import { errorMessage, isApiError } from '@/utils/errors'
import { CenterContext, centerPath, useCenter } from './center'
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
  return (
    <div className={s.fullPage}>
      <EmptyState icon={<Lock aria-hidden="true" />} title={ru.auth.noCentersTitle} text={ru.auth.noCentersText} />
    </div>
  )
}

/** /c/:centerId — центр из /me; чужой центр — на главную. */
export function CenterScope({ children }: { children: React.ReactNode }) {
  const { centerId } = useParams()
  const me = useMe()
  const centers = me.data?.centers ?? []
  const c = centers.find((x) => x.id === centerId)
  if (!c) return <Navigate to="/" replace />
  return <CenterContext.Provider value={{ id: c.id, name: c.name, role: c.role, centersCount: centers.length }}>{children}</CenterContext.Provider>
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
