// Импорт, отчёт, настройки, журнал.
import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Schemas } from '@/api/client'
import { qk } from '@/api/keys'
import { filenameFrom, saveBlob } from '@/utils/download'

export type ImportPreview = Schemas['ImportPreview']
export type ImportRow = Schemas['ImportRow']
export type Settings = Schemas['Settings']
export type AuditItem = Schemas['AuditItem']
export type AuditFilter = { user?: string; action?: string; from?: string; to?: string }

/** Скачать файл из ответа openapi-fetch с parseAs: 'blob' (в схеме файл описан строкой, на деле — Blob). */
async function download(p: Promise<{ data?: unknown; error?: unknown; response: Response }>, fallback: string) {
  let disposition: string | null = null
  const data = await unwrap(
    p.then((r) => {
      disposition = r.response.headers.get('Content-Disposition')
      return r
    }),
  )
  saveBlob(data as Blob, filenameFrom(disposition, fallback))
}

// ---- импорт

export function useImportTemplate(centerId: string) {
  return useMutation({
    mutationFn: () => download(api.GET('/api/v1/c/{centerId}/import/template.xlsx', { params: { path: { centerId } }, parseAs: 'blob' }), 'okuu-import.xlsx'),
  })
}

export function useImportPreview(centerId: string) {
  return useMutation({
    mutationFn: (file: File) => {
      const form = new FormData()
      form.append('file', file)
      return unwrap(
        api.POST('/api/v1/c/{centerId}/import/preview', {
          params: { path: { centerId } },
          // FormData уходит как есть: браузер сам ставит multipart с границей.
          body: form as unknown as { file: string },
          bodySerializer: (b) => b as unknown as FormData,
        }),
      )
    },
    meta: { silent: true },
  })
}

export function useImportCommit(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['ImportCommitRequest']) => unwrap(api.POST('/api/v1/c/{centerId}/import/commit', { params: { path: { centerId } }, body })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.students(centerId) })
      void qc.invalidateQueries({ queryKey: qk.debtors(centerId) })
      void qc.invalidateQueries({ queryKey: qk.groups(centerId) })
      void qc.invalidateQueries({ queryKey: qk.ledger(centerId) })
    },
    meta: { silent: true },
  })
}

// ---- отчёт

export function useMonthReport(centerId: string, month: string) {
  return useQuery({
    queryKey: qk.report(centerId, month),
    queryFn: () => unwrap(api.GET('/api/v1/c/{centerId}/reports/month', { params: { path: { centerId }, query: { m: month } } })),
    placeholderData: keepPreviousData,
  })
}

export function useReportExport(centerId: string) {
  return useMutation({
    mutationFn: (month: string) =>
      download(api.GET('/api/v1/c/{centerId}/reports/month/export.xlsx', { params: { path: { centerId }, query: { m: month } }, parseAs: 'blob' }), `okuu-report-${month}.xlsx`),
  })
}

// ---- настройки

export function useSettings(centerId: string) {
  return useQuery({
    queryKey: qk.settings(centerId),
    queryFn: () => unwrap(api.GET('/api/v1/c/{centerId}/settings', { params: { path: { centerId } } })),
  })
}

export function usePatchSettings(centerId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas['PatchSettingsRequest']) => unwrap(api.PATCH('/api/v1/c/{centerId}/settings', { params: { path: { centerId } }, body })),
    onSuccess: (s) => {
      qc.setQueryData(qk.settings(centerId), s)
      // Название центра — в шапке и меню (из /me).
      void qc.invalidateQueries({ queryKey: ['me'] })
    },
    meta: { silent: true },
  })
}

// ---- журнал

export function useAudit(centerId: string, f: AuditFilter) {
  return useInfiniteQuery({
    queryKey: qk.audit(centerId, f),
    queryFn: ({ pageParam }) =>
      unwrap(api.GET('/api/v1/c/{centerId}/audit', { params: { path: { centerId }, query: { ...f, limit: 50, cursor: pageParam } } })),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
  })
}
