import { CheckCircle2, MessageCircle } from 'lucide-react'
import type { ReactNode } from 'react'
import { CopyButton } from '@/components/shared'
import { Badge, ButtonAnchor } from '@/components/ui'
import { ru } from '@/i18n/ru'
import s from './WhatsApp.module.css'

/**
 * Строка списка «отправьте в WhatsApp» (напоминания и рассылка). Настоящая ссылка: браузер откроет её в новой вкладке,
 * а окно со списком останется на месте. url = null — телефона нет.
 */
export function WhatsAppLinkRow({
  title,
  meta,
  aside,
  url,
  text,
  opened,
  onOpen,
}: {
  title: ReactNode
  meta?: ReactNode
  aside?: ReactNode
  url: string | null
  text: string
  opened: boolean
  onOpen: () => void
}) {
  return (
    <li className={s.item}>
      <div className={s.itemMain}>
        <strong>{title}</strong>
        {meta && <span className={s.lead}>{meta}</span>}
        {aside}
      </div>
      <div className={s.itemActions}>
        {opened && (
          <Badge tone="excused">
            <CheckCircle2 size={12} aria-hidden="true" /> {ru.whatsapp.opened}
          </Badge>
        )}
        {url ? (
          <>
            <ButtonAnchor
              variant={opened ? 'secondary' : 'primary'}
              href={url}
              target="_blank"
              rel="noopener noreferrer"
              onClick={onOpen}
              icon={<MessageCircle size={16} aria-hidden="true" />}
            >
              {ru.whatsapp.button}
            </ButtonAnchor>
            <CopyButton text={text} label={ru.whatsapp.copy} />
          </>
        ) : (
          <span className={s.lead}>{ru.whatsapp.noContact}</span>
        )}
      </div>
    </li>
  )
}
