import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/api/client'
import { qk } from '@/api/keys'

export type Student = Schemas['Student']
export type StudentListItem = Schemas['StudentListItem']
export type Enrollment = Schemas['Enrollment']
export type StudentStatus = Schemas['StudentStatus']
export type TgStatus = Schemas['TgStatus']

export type StudentFilter = { q?: string; group?: string; status?: StudentStatus; tg?: TgStatus; hide_left?: boolean }

const PAGE = 30

export function useStudentList(centerId: string, f: StudentFilter) {
  return useInfiniteQuery({
    queryKey: qk.studentList(centerId, f),
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/api/v1/c/{centerId}/students', {
          params: { path: { centerId }, query: { ...f, limit: PAGE, cursor: pageParam } },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
  })
}

export function useStudent(centerId: string, studentId: string) {
  return useQuery({
    queryKey: qk.student(centerId, studentId),
    queryFn: () => unwrap(api.GET('/api/v1/c/{centerId}/students/{studentId}', { params: { path: { centerId, studentId } } })),
  })
}

/** После изменений ученика — карточка, списки и должники. */
function invalidateStudent(qc: ReturnType<typeof useQueryClient>, centerId: string) {
  void qc.invalidateQueries({ queryKey: qk.students(centerId) })
  void qc.invalidateQueries({ queryKey: qk.debtors(centerId) })
  void qc.invalidateQueries({ queryKey: qk.ledger(centerId) })
}

export function useCreateStudent(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['CreateStudentRequest']) => unwrap(api.POST('/api/v1/c/{centerId}/students', { params: { path: { centerId } }, body })),
    onSuccess: () => invalidateStudent(qc, centerId),
    meta: { silent: true },
  })
}

export function usePatchStudent(centerId: string, studentId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['PatchStudentRequest']) =>
      unwrap(api.PATCH('/api/v1/c/{centerId}/students/{studentId}', { params: { path: { centerId, studentId } }, body })),
    onSuccess: (st) => {
      qc.setQueryData(qk.student(centerId, studentId), st)
      invalidateStudent(qc, centerId)
    },
  })
}

export function useEnroll(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ studentId, body }: { studentId: string; body: Schemas['EnrollRequest'] }) =>
      unwrap(api.POST('/api/v1/c/{centerId}/students/{studentId}/enrollments', { params: { path: { centerId, studentId } }, body })),
    onSuccess: () => invalidateStudent(qc, centerId),
    meta: { silent: true },
  })
}

export function useEndEnrollment(centerId: string, studentId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ enrollmentId, endDate }: { enrollmentId: string; endDate?: string }) =>
      unwrap(
        api.POST('/api/v1/c/{centerId}/students/{studentId}/enrollments/{enrollmentId}/end', {
          params: { path: { centerId, studentId, enrollmentId } },
          body: endDate ? { end_date: endDate } : {},
        }),
      ),
    onSuccess: () => invalidateStudent(qc, centerId),
    meta: { silent: true },
  })
}

export function useRenewEnrollment(centerId: string, studentId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (enrollmentId: string) =>
      unwrap(
        api.POST('/api/v1/c/{centerId}/students/{studentId}/enrollments/{enrollmentId}/renew', {
          params: { path: { centerId, studentId, enrollmentId } },
        }),
      ),
    onSuccess: () => invalidateStudent(qc, centerId),
  })
}

export function useParentInvite(centerId: string) {
  return useMutation({
    mutationFn: (parentId: string) => unwrap(api.POST('/api/v1/c/{centerId}/parents/{parentId}/invite', { params: { path: { centerId, parentId } } })),
  })
}

export function useStudentAttendance(centerId: string, studentId: string, month: string) {
  return useQuery({
    queryKey: qk.studentAttendance(centerId, studentId, month),
    queryFn: () =>
      unwrap(api.GET('/api/v1/c/{centerId}/students/{studentId}/attendance', { params: { path: { centerId, studentId }, query: { month } } })),
    select: (d) => d.items,
    placeholderData: keepPreviousData,
  })
}
