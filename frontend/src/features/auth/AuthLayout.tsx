import { Wallet } from 'lucide-react'
import type { ReactNode } from 'react'
import { Card } from '@/components/ui'
import { ru } from '@/i18n/ru'
import s from './AuthLayout.module.css'

/** Экран без меню: вход, регистрация, приглашение, оплата. */
export function AuthLayout({ title, lead, children, footer }: { title: ReactNode; lead?: ReactNode; children: ReactNode; footer?: ReactNode }) {
  return (
    <main className={s.page}>
      <div className={s.brand}>
        <span className={s.logo} aria-hidden="true">
          <Wallet size={24} />
        </span>
        <div>
          <div className={s.brandName}>{ru.app.name}</div>
          <div className={s.brandTag}>{ru.app.tagline}</div>
        </div>
      </div>
      <Card className={s.card}>
        <h1>{title}</h1>
        {lead && <p className={s.lead}>{lead}</p>}
        {children}
      </Card>
      {footer && <p className={s.switch}>{footer}</p>}
    </main>
  )
}

export const authStyles = s
