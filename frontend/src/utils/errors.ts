import type { FieldValues, Path, UseFormSetError } from 'react-hook-form'
import { ApiError, NETWORK } from '@/api/client'
import { ru } from '@/i18n/ru'

/** Понятное сообщение по-русски для любой ошибки (коды — docs/errors.md). */
export function errorMessage(err: unknown): string {
  if (!(err instanceof ApiError)) return ru.errors.unknown
  if (err.code === NETWORK) return ru.errors.network
  if (err.code === 'validation' && err.message) return err.message
  if (err.code === 'too_many_attempts') return ru.errors.tooManyAttempts(err.retryAfter)
  return ru.errors.codes[err.code] ?? (err.message || ru.errors.unknown)
}

export const isApiError = (err: unknown, code?: string): err is ApiError =>
  err instanceof ApiError && (code === undefined || err.code === code)

export const isNetworkError = (err: unknown) => isApiError(err, NETWORK)

/**
 * Ошибку поля (`field` из API) — под поле формы. fields: имя поля API → имя поля формы
 * (если совпадают — можно не указывать). Возвращает true, если ошибка показана у поля.
 */
export function applyFieldError<T extends FieldValues>(
  err: unknown,
  setError: UseFormSetError<T>,
  fields: Partial<Record<string, Path<T>>> | readonly Path<T>[],
): boolean {
  if (!(err instanceof ApiError) || !err.field) return false
  const target = Array.isArray(fields)
    ? (fields as readonly string[]).includes(err.field)
      ? (err.field as Path<T>)
      : undefined
    : (fields as Partial<Record<string, Path<T>>>)[err.field]
  if (!target) return false
  setError(target, { type: 'server', message: errorMessage(err) }, { shouldFocus: true })
  return true
}
