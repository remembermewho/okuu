import { Check, Copy, MessageCircle } from 'lucide-react'
import { useState } from 'react'
import { useCenter } from '@/app/center'
import { CopyButton, copyText, InfoTip } from '@/components/shared'
import shared from '@/components/shared.module.css'
import { Button, ButtonAnchor, Dialog, Textarea, layout, useToast } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { cx } from '@/utils/cx'
import { errorMessage } from '@/utils/errors'
import { fetchWhatsAppReminder, useOpenWhatsApp, type WhatsAppReminder } from './api'
import s from './WhatsApp.module.css'

type Note = 'demo' | 'blocked'

/**
 * Текст берётся с сервера уже после нажатия, а буфер обмена браузер разрешает только в момент нажатия —
 * поэтому в ClipboardItem отдаём обещание текста; там, где так нельзя, — обычное копирование.
 */
async function copyLater(text: Promise<string>): Promise<void> {
  if (typeof ClipboardItem !== 'undefined' && navigator.clipboard?.write) {
    try {
      await navigator.clipboard.write([new ClipboardItem({ 'text/plain': text.then((t) => new Blob([t], { type: 'text/plain' })) })])
      return
    } catch {
      // Ошибку API покажет await ниже; отказ браузера — запасной путь.
    }
  }
  await copyText(await text)
}

/** Окно с текстом: демо-центр и случай, когда браузер не открыл вкладку. */
export function WhatsAppTextDialog({ message, note, onClose }: { message: WhatsAppReminder | null; note: Note; onClose: () => void }) {
  return (
    <Dialog
      open={message !== null}
      onOpenChange={(o) => !o && onClose()}
      title={ru.whatsapp.textTitle}
      description={note === 'demo' ? ru.whatsapp.demoNote : ru.whatsapp.blockedNote}
      footer={<Button onClick={onClose}>{ru.common.close}</Button>}
    >
      {message && (
        <div className={layout.stack}>
          <Textarea label={ru.whatsapp.textTitle} value={message.text} readOnly rows={10} />
          <div className={shared.actions}>
            <CopyButton text={message.text} label={ru.whatsapp.copy} />
            {note === 'blocked' && message.url && (
              <ButtonAnchor href={message.url} target="_blank" rel="noopener noreferrer" icon={<MessageCircle size={18} aria-hidden="true" />}>
                {ru.whatsapp.openLink}
              </ButtonAnchor>
            )}
          </div>
        </div>
      )}
    </Dialog>
  )
}

/**
 * Кнопка WhatsApp и «Скопировать текст» для ученика с долгом. Телефона нет — кнопка неактивна с подсказкой;
 * телефон некорректный (whatsappPhone = null) — кнопки нет, текст скопировать можно.
 */
export function WhatsAppActions({
  studentId,
  name,
  phone,
  whatsappPhone,
  hasDebt = true,
  label = ru.whatsapp.button,
  alwaysLabeled,
  className,
}: {
  studentId: string
  name: string
  phone: string | null | undefined
  whatsappPhone: string | null | undefined
  hasDebt?: boolean
  label?: string
  /** Подпись и на телефоне (карточка ученика); иначе на узком экране — только значок. */
  alwaysLabeled?: boolean
  className?: string
}) {
  const c = useCenter()
  const open = useOpenWhatsApp(c.id)
  const toast = useToast()
  const [dialog, setDialog] = useState<{ message: WhatsAppReminder; note: Note } | null>(null)
  const [copied, setCopied] = useState(false)
  const [loadingDemo, setLoadingDemo] = useState(false)

  const hasPhone = Boolean(phone?.trim())
  const labelClass = alwaysLabeled ? undefined : s.wideOnly

  const fail = (e: unknown) => toast({ message: errorMessage(e), tone: 'error' })

  const onOpen = () => {
    if (c.isDemo) {
      setLoadingDemo(true)
      fetchWhatsAppReminder(c.id, studentId)
        .then((message) => setDialog({ message, note: 'demo' }))
        .catch(fail)
        .finally(() => setLoadingDemo(false))
      return
    }
    // Вкладку открываем сразу, в момент нажатия: после ответа сервера браузер счёл бы её всплывающим окном.
    const tab = window.open('', '_blank')
    open.mutate(studentId, {
      onSuccess: (message) => {
        if (tab && message.url) {
          tab.opener = null
          tab.location.href = message.url
        } else {
          tab?.close()
          setDialog({ message, note: 'blocked' })
        }
      },
      onError: (e) => {
        tab?.close()
        fail(e)
      },
    })
  }

  const onCopy = async () => {
    try {
      await copyLater(fetchWhatsAppReminder(c.id, studentId).then((m) => m.text))
      setCopied(true)
      toast({ message: ru.whatsapp.copied, tone: 'success' })
      window.setTimeout(() => setCopied(false), 2000)
    } catch (e) {
      fail(e)
    }
  }

  const disabledHint = !hasDebt ? ru.whatsapp.noDebt : !hasPhone ? ru.whatsapp.noPhone : undefined

  return (
    <span className={cx(s.actions, className)}>
      {(whatsappPhone || !hasPhone || !hasDebt) && (
        <Button
          variant="secondary"
          icon={<MessageCircle size={16} aria-hidden="true" />}
          loading={open.isPending || loadingDemo}
          disabled={disabledHint !== undefined}
          title={disabledHint}
          onClick={onOpen}
          aria-label={ru.whatsapp.labelFor(label, name)}
        >
          <span className={labelClass}>{label}</span>
        </Button>
      )}
      {disabledHint && <InfoTip label={disabledHint} text={disabledHint} />}
      {hasDebt && (
        <Button
          variant="secondary"
          icon={copied ? <Check size={16} aria-hidden="true" /> : <Copy size={16} aria-hidden="true" />}
          onClick={() => void onCopy()}
          aria-label={ru.whatsapp.labelFor(ru.whatsapp.copy, name)}
        >
          <span className={labelClass}>{ru.whatsapp.copy}</span>
        </Button>
      )}
      <WhatsAppTextDialog message={dialog?.message ?? null} note={dialog?.note ?? 'demo'} onClose={() => setDialog(null)} />
    </span>
  )
}
