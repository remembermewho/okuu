import type { ComponentProps, ReactNode } from 'react'
import { AlertTriangle } from 'lucide-react'
import { ru } from '@/i18n/ru'
import { cx } from '@/utils/cx'
import { formatSom } from '@/utils/money'
import { Button } from './Button'
import s from './ui.module.css'

export { Button, ButtonAnchor, ButtonLink } from './Button'
export { Checkbox, Input, RadioGroup, Select, Textarea } from './Field'
export { Dialog, ConfirmDialog } from './Dialog'
export { ToastProvider, useToast } from './Toast'

export function Card({ flush, className, ...rest }: ComponentProps<'section'> & { flush?: boolean }) {
  return <section className={cx(s.card, flush && s.cardFlush, className)} {...rest} />
}

export function CardHeader({ title, actions }: { title: ReactNode; actions?: ReactNode }) {
  return (
    <div className={s.cardHeader}>
      <h2>{title}</h2>
      {actions}
    </div>
  )
}

export type BadgeTone = 'neutral' | 'success' | 'debt' | 'excused'

export function Badge({ tone = 'neutral', children, className }: { tone?: BadgeTone; children: ReactNode; className?: string }) {
  return <span className={cx(s.badge, s[tone], className)}>{children}</span>
}

export function Skeleton({ width = '100%', height = 16, className }: { width?: number | string; height?: number | string; className?: string }) {
  return <span className={cx(s.skeleton, className)} style={{ width, height }} aria-hidden="true" />
}

/** Скелетон списка: rows карточек-строк. */
export function SkeletonList({ rows = 4, height = 64 }: { rows?: number; height?: number }) {
  return (
    <div className={s.skeletonList} role="status" aria-label={ru.ui.loading}>
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} height={height} />
      ))}
    </div>
  )
}

export function EmptyState({ icon, title, text, action }: { icon?: ReactNode; title: ReactNode; text?: ReactNode; action?: ReactNode }) {
  return (
    <div className={s.empty}>
      {icon && <span className={s.emptyIcon}>{icon}</span>}
      <p className={s.emptyTitle}>{title}</p>
      {text && <p>{text}</p>}
      {action && <div className={s.emptyAction}>{action}</div>}
    </div>
  )
}

/**
 * Сумма из тыйын. tone: 'balance' — минус цветом долга, плюс — главным цветом;
 * 'debt' — всегда цветом долга.
 */
export function MoneyText({ tyiyn, tone, sign, className }: { tyiyn: number; tone?: 'balance' | 'debt'; sign?: boolean; className?: string }) {
  const color = tone === 'debt' ? s.moneyDebt : tone === 'balance' ? (tyiyn < 0 ? s.moneyDebt : tyiyn > 0 ? s.moneyAdvance : undefined) : undefined
  return <span className={cx(s.money, color, className)}>{formatSom(tyiyn, { sign })}</span>
}

export function PageHeader({ title, subtitle, actions }: { title: ReactNode; subtitle?: ReactNode; actions?: ReactNode }) {
  return (
    <header className={s.pageHeader}>
      <div className={s.pageTitle}>
        <h1>{title}</h1>
        {subtitle && <p className={s.pageSubtitle}>{subtitle}</p>}
      </div>
      {actions && <div className={s.pageActions}>{actions}</div>}
    </header>
  )
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className={s.errorBox} role="alert">
      <span className={s.row}>
        <AlertTriangle size={20} aria-hidden="true" />
        <strong>{ru.errors.title}</strong>
      </span>
      <p className={s.errorText}>{message}</p>
      {onRetry && (
        <Button variant="secondary" onClick={onRetry}>
          {ru.common.retry}
        </Button>
      )}
    </div>
  )
}

export function FormError({ children }: { children?: ReactNode }) {
  if (!children) return null
  return (
    <div className={s.formError} role="alert">
      {children}
    </div>
  )
}

export const layout = s
