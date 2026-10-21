import { QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { createMemoryRouter, RouterProvider, type RouteObject } from 'react-router'
import { vi } from 'vitest'
import { createQueryClient } from '@/app/queryClient'
import { routes as appRoutes } from '@/app/router'
import { ToastProvider } from '@/components/ui'

type Reply = { status?: number; body?: unknown; headers?: Record<string, string> }
type Handler = Reply | ((req: Request) => Reply | Promise<Reply>)

/**
 * Подменяет fetch: handlers — «МЕТОД /путь» (без query) → ответ. Незнакомый запрос — 404.
 * Возвращает список сделанных запросов.
 */
export function mockApi(handlers: Record<string, Handler>) {
  const calls: Request[] = []
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = input instanceof Request ? input : new Request(new URL(String(input), 'http://localhost'), init)
    calls.push(req.clone())
    const url = new URL(req.url, 'http://localhost')
    const h = handlers[`${req.method} ${url.pathname}`]
    const r: Reply = h ? (typeof h === 'function' ? await h(req) : h) : { status: 404, body: { error: { code: 'not_found', message: 'Не найдено' } } }
    const status = r.status ?? 200
    return new Response(status === 204 ? null : JSON.stringify(r.body ?? {}), {
      status,
      headers: { 'Content-Type': 'application/json', ...r.headers },
    })
  })
  vi.stubGlobal('fetch', fetchMock)
  return { calls, fetchMock }
}

export const apiError = (status: number, code: string, message: string, field?: string): Reply => ({
  status,
  body: { error: { code, message, ...(field ? { field } : {}) } },
})

export const unauthorized = apiError(401, 'unauthorized', 'Требуется вход')

/** Рендер маршрутов приложения (или своих) с QueryClient и тостами. */
export function renderRoute(path: string, routes: RouteObject[] = appRoutes) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  const qc = createQueryClient()
  const utils = render(
    <QueryClientProvider client={qc}>
      <ToastProvider>
        <RouterProvider router={router} />
      </ToastProvider>
    </QueryClientProvider>,
  )
  return { ...utils, router, qc }
}

export async function bodyOf(req: Request): Promise<unknown> {
  return JSON.parse(await req.text())
}
