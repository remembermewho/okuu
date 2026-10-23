import { createBrowserRouter, type RouteObject } from 'react-router'
import { AuditPage } from '@/features/admin/AuditPage'
import { ImportPage } from '@/features/admin/ImportPage'
import { ReportPage } from '@/features/admin/ReportPage'
import { SettingsPage } from '@/features/admin/SettingsPage'
import { AccountPage } from '@/features/auth/AccountPage'
import { LoginPage } from '@/features/auth/LoginPage'
import { ResetPasswordPage } from '@/features/auth/ResetPasswordPage'
import { RegisterPage } from '@/features/auth/RegisterPage'
import { GroupPage } from '@/features/groups/GroupPage'
import { GroupsPage } from '@/features/groups/GroupsPage'
import { DebtorsPage } from '@/features/money/DebtorsPage'
import { InvitePage } from '@/features/staff/InvitePage'
import { StaffPage } from '@/features/staff/StaffPage'
import { StudentPage } from '@/features/students/card/StudentPage'
import { StudentsPage } from '@/features/students/StudentsPage'
import { TariffsPage } from '@/features/tariffs/TariffsPage'
import { PayPage } from '@/features/pay/PayPage'
import { CenterPage as PlatformCenterPage } from '@/features/platform/CenterPage'
import { CentersPage as PlatformCentersPage } from '@/features/platform/CentersPage'
import { LinksPage as PlatformLinksPage } from '@/features/platform/LinksPage'
import { PlatformLayout, RequirePlatformAdmin } from '@/features/platform/PlatformLayout'
import { TodayPage } from '@/features/today/TodayPage'
import { AppLayout } from './AppLayout'
import { CenterHome, CenterScope, HomeRedirect, RequireAuth, RequireSection } from './guards'
import { CenterPicker, MorePage, NotFoundPage } from './pages'
import type { Section } from './permissions'
import { setUnauthorizedHandler } from './queryClient'

const section = (s: Section, element: React.ReactNode): RouteObject => ({
  path: s,
  element: <RequireSection section={s}>{element}</RequireSection>,
})

export const routes: RouteObject[] = [
  { path: '/login', element: <LoginPage /> },
  { path: '/register', element: <RegisterPage /> },
  { path: '/register/:token', element: <RegisterPage /> },
  { path: '/invite/:token', element: <InvitePage /> },
  { path: '/pay/:token', element: <PayPage /> },
  { path: '/reset/:token', element: <ResetPasswordPage /> },
  {
    element: <RequireAuth />,
    children: [
      { path: '/', element: <HomeRedirect /> },
      { path: '/centers', element: <CenterPicker /> },
      { path: '/account', element: <AccountPage /> },
      // Панель суперадмина — свой каркас; без is_platform_admin — «не найдено».
      {
        path: '/admin',
        element: (
          <RequirePlatformAdmin>
            <PlatformLayout />
          </RequirePlatformAdmin>
        ),
        children: [
          { index: true, element: <PlatformCentersPage /> },
          { path: 'centers/:id', element: <PlatformCenterPage /> },
          { path: 'links', element: <PlatformLinksPage /> },
        ],
      },
      {
        path: '/c/:centerId',
        element: (
          <CenterScope>
            <AppLayout />
          </CenterScope>
        ),
        children: [
          { index: true, element: <CenterHome /> },
          section('today', <TodayPage />),
          section('debtors', <DebtorsPage />),
          section('students', <StudentsPage />),
          { path: 'students/:studentId', element: <RequireSection section="students"><StudentPage /></RequireSection> },
          section('groups', <GroupsPage />),
          // Состав группы — только владелец и админ (список учеников преподавателю недоступен).
          { path: 'groups/:groupId', element: <RequireSection section="students"><GroupPage /></RequireSection> },
          section('tariffs', <TariffsPage />),
          section('staff', <StaffPage />),
          section('import', <ImportPage />),
          section('report', <ReportPage />),
          section('settings', <SettingsPage />),
          section('audit', <AuditPage />),
          { path: 'more', element: <MorePage /> },
          { path: '*', element: <NotFoundPage /> },
        ],
      },
    ],
  },
  { path: '*', element: <NotFoundPage /> },
]

export function createRouter() {
  const router = createBrowserRouter(routes)
  // 401 из любого запроса — на вход, вернуться туда же после входа.
  setUnauthorizedHandler(() => {
    const { pathname, search } = router.state.location
    if (pathname === '/login') return
    void router.navigate('/login?next=' + encodeURIComponent(pathname + search), { replace: true })
  })
  return router
}
