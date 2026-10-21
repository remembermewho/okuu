import type { ComponentProps, ReactNode } from 'react'
import { Link, type LinkProps } from 'react-router'
import { cx } from '@/utils/cx'
import s from './Button.module.css'

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger'
type Size = 'sm' | 'md' | 'lg'

type Common = {
  variant?: Variant
  size?: Size
  block?: boolean
  /** Кнопка-иконка: квадратная, текст — в aria-label. */
  iconOnly?: boolean
  icon?: ReactNode
}

function classes({ variant = 'primary', size = 'md', block, iconOnly }: Common, extra?: string) {
  return cx(s.btn, s[variant], size !== 'md' && s[size], block && s.block, iconOnly && s.icon, extra)
}

export type ButtonProps = Common & ComponentProps<'button'> & { loading?: boolean }

export function Button({ variant, size, block, iconOnly, icon, loading, className, children, disabled, type = 'button', ...rest }: ButtonProps) {
  return (
    <button
      type={type}
      className={classes({ variant, size, block, iconOnly }, className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading ? <span className={s.spinner} aria-hidden="true" /> : icon}
      {children}
    </button>
  )
}

/** Ссылка, оформленная как кнопка. */
export function ButtonLink({ variant, size, block, iconOnly, icon, className, children, ...rest }: Common & LinkProps) {
  return (
    <Link className={classes({ variant, size, block, iconOnly }, className)} {...rest}>
      {icon}
      {children}
    </Link>
  )
}

/** Внешняя ссылка или файл, оформленные как кнопка. */
export function ButtonAnchor({ variant, size, block, iconOnly, icon, className, children, ...rest }: Common & ComponentProps<'a'>) {
  return (
    <a className={classes({ variant, size, block, iconOnly }, className)} {...rest}>
      {icon}
      {children}
    </a>
  )
}
