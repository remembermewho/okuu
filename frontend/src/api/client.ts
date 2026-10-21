// Клиент API: типы — из docs/openapi.yaml (npm run gen:api → schema.d.ts), руками не пишутся.
import createClient from 'openapi-fetch'
import type { components, paths } from './schema'

export type Schemas = components['schemas']

/** Ошибка API в формате {error:{code,message,field?}} или сети (code = 'network'). */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly field?: string
  readonly retryAfter?: number

  constructor(status: number, code: string, message: string, field?: string, retryAfter?: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.field = field
    this.retryAfter = retryAfter
  }
}

export const NETWORK = 'network'

export const api = createClient<paths>({
  // Тот же origin, что у фронтенда (Caddy / Vite proxy отдают /api); абсолютный адрес —
  // чтобы Request собирался и вне браузера (тесты).
  baseUrl: typeof window === 'undefined' ? '' : window.location.origin,
  credentials: 'include',
  // fetch берём в момент запроса — тесты подменяют globalThis.fetch.
  fetch: (req) => globalThis.fetch(req),
})

type Result<T> = { data?: T; error?: unknown; response: Response }

/** Данные ответа или ApiError. Ошибка сети тоже становится ApiError (code = 'network'). */
export async function unwrap<T>(p: Promise<Result<T>>): Promise<T> {
  let res: Result<T>
  try {
    res = await p
  } catch (e) {
    if (e instanceof ApiError) throw e
    throw new ApiError(0, NETWORK, e instanceof Error ? e.message : String(e))
  }
  if (res.response.ok) return res.data as T
  throw toApiError(res.response, res.error)
}

function toApiError(response: Response, body: unknown): ApiError {
  const retry = Number(response.headers.get('Retry-After'))
  const err = (body as Schemas['Error'] | undefined)?.error
  if (err && typeof err.code === 'string') {
    return new ApiError(response.status, err.code, err.message, err.field, Number.isFinite(retry) && retry > 0 ? retry : undefined)
  }
  return new ApiError(response.status, 'http_' + response.status, response.statusText)
}

/** Ключ идемпотентности оплаты: один на открытие формы. */
export function newIdempotencyKey(): string {
  return crypto.randomUUID()
}
