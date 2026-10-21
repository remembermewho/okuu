import { MessageCircle } from 'lucide-react'
import { CopyButton } from '@/components/shared'
import shared from '@/components/shared.module.css'
import { Button, ButtonAnchor, Dialog, Input, layout } from '@/components/ui'
import { ru } from '@/i18n/ru'

/**
 * Одноразовая ссылка (приглашение, сброс пароля): поле со ссылкой, «Скопировать», «Отправить в WhatsApp».
 * Ссылка приходит только в ответе API — показываем один раз.
 */
export function ShareLinkDialog({
  title,
  lead,
  url,
  hint,
  whatsappText,
  onClose,
}: {
  title: string
  lead: string
  url: string
  hint?: string
  whatsappText: string
  onClose: () => void
}) {
  const wa = `https://wa.me/?text=${encodeURIComponent(whatsappText)}`
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={title} description={lead} footer={<Button onClick={onClose}>{ru.common.close}</Button>}>
      <div className={layout.stack}>
        <Input label={ru.share.link} value={url} readOnly hint={hint} onFocus={(e) => e.currentTarget.select()} />
        <div className={shared.actions}>
          <CopyButton text={url} label={ru.share.copy} variant="primary" />
          <ButtonAnchor variant="secondary" href={wa} target="_blank" rel="noopener noreferrer" icon={<MessageCircle size={18} aria-hidden="true" />}>
            {ru.share.whatsapp}
          </ButtonAnchor>
        </div>
      </div>
    </Dialog>
  )
}
