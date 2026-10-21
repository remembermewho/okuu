import * as RD from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import type { ReactNode } from 'react'
import { ru } from '@/i18n/ru'
import { cx } from '@/utils/cx'
import { Button } from './Button'
import s from './Dialog.module.css'

type DialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  children?: ReactNode
  footer?: ReactNode
  wide?: boolean
}

/** Диалог (Radix): на телефоне — шторка снизу, на широком экране — по центру. */
export function Dialog({ open, onOpenChange, title, description, children, footer, wide }: DialogProps) {
  return (
    <RD.Root open={open} onOpenChange={onOpenChange}>
      <RD.Portal>
        <RD.Overlay className={s.overlay} />
        <RD.Content className={cx(s.content, wide && s.wide)}>
          <div className={s.header}>
            <div>
              <RD.Title className={s.title}>{title}</RD.Title>
              {description ? <RD.Description className={s.description}>{description}</RD.Description> : <RD.Description className="visually-hidden">{title}</RD.Description>}
            </div>
            <RD.Close asChild>
              <Button variant="ghost" iconOnly aria-label={ru.ui.dialogClose}>
                <X size={20} aria-hidden="true" />
              </Button>
            </RD.Close>
          </div>
          {children}
          {footer && <div className={s.footer}>{footer}</div>}
        </RD.Content>
      </RD.Portal>
    </RD.Root>
  )
}

type ConfirmProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  text?: ReactNode
  confirmLabel: ReactNode
  danger?: boolean
  loading?: boolean
  onConfirm: () => void
  children?: ReactNode
}

/** Подтверждение действия (удалить, в архив). */
export function ConfirmDialog({ open, onOpenChange, title, text, confirmLabel, danger, loading, onConfirm, children }: ConfirmProps) {
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={text}
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            {ru.common.cancel}
          </Button>
          <Button variant={danger ? 'danger' : 'primary'} loading={loading} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children}
    </Dialog>
  )
}
