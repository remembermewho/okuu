import { X } from 'lucide-react'
import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import { ru } from '@/i18n/ru'
import { cx } from '@/utils/cx'
import s from './Toast.module.css'

export type ToastInput = {
  message: string
  tone?: 'info' | 'success' | 'error'
  action?: { label: string; onClick: () => void }
  /** мс; по умолчанию 5 с, с действием — 8 с. */
  duration?: number
}

type ToastItem = ToastInput & { id: number }

const Ctx = createContext<((t: ToastInput) => void) | null>(null)

/** Внешний доступ (обработчик ошибок QueryClient вне React). */
let external: ((t: ToastInput) => void) | null = null
export function showToast(t: ToastInput) {
  external?.(t)
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const seq = useRef(0)

  const dismiss = useCallback((id: number) => setItems((xs) => xs.filter((x) => x.id !== id)), [])

  const toast = useCallback(
    (t: ToastInput) => {
      const id = ++seq.current
      setItems((xs) => [...xs.filter((x) => x.message !== t.message), { ...t, id }].slice(-3))
      window.setTimeout(() => dismiss(id), t.duration ?? (t.action ? 8000 : 5000))
    },
    [dismiss],
  )
  external = toast

  const value = useMemo(() => toast, [toast])

  return (
    <Ctx.Provider value={value}>
      {children}
      <div className={s.region} role="region" aria-label={ru.ui.toastRegion} aria-live="polite">
        {items.map((t) => (
          <div key={t.id} className={cx(s.toast, t.tone === 'error' && s.error, t.tone === 'success' && s.success)} role={t.tone === 'error' ? 'alert' : 'status'}>
            <span className={s.message}>{t.message}</span>
            {t.action && (
              <button
                type="button"
                className={s.action}
                onClick={() => {
                  dismiss(t.id)
                  t.action?.onClick()
                }}
              >
                {t.action.label}
              </button>
            )}
            <button type="button" className={s.close} aria-label={ru.common.close} onClick={() => dismiss(t.id)}>
              <X size={18} aria-hidden="true" />
            </button>
          </div>
        ))}
      </div>
    </Ctx.Provider>
  )
}

export function useToast() {
  const t = useContext(Ctx)
  if (!t) throw new Error('useToast вне ToastProvider')
  return t
}
