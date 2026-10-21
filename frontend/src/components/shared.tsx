import { Check, ChevronLeft, ChevronRight, Copy, Send } from 'lucide-react'
import { useState } from 'react'
import { Badge, Button, type BadgeTone } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { addMonths, formatMonth } from '@/utils/date'
import s from './shared.module.css'

const tgTone: Record<string, BadgeTone> = { active: 'success', none: 'neutral', stopped: 'debt', blocked: 'debt' }

/** Статус связи родителя с Telegram-ботом. */
export function TgBadge({ status, short }: { status: string | null | undefined; short?: boolean }) {
  const st = status ?? 'none'
  return (
    <Badge tone={tgTone[st] ?? 'neutral'}>
      {st === 'active' && <Send size={12} aria-hidden="true" />}
      {(short ? ru.statuses.tgShort : ru.statuses.tg)[st] ?? st}
    </Badge>
  )
}

/** Статус ученика: «учится» не показываем, пауза и ушёл — бейджем. */
export function StudentStatusBadge({ status }: { status: string }) {
  if (status === 'active') return null
  return <Badge tone={status === 'left' ? 'debt' : 'neutral'}>{ru.statuses.student[status] ?? status}</Badge>
}

/** ← Октябрь 2026 → */
export function MonthSwitcher({ month, onChange, max }: { month: string; onChange: (m: string) => void; max?: string }) {
  return (
    <div className={s.month}>
      <Button variant="ghost" iconOnly aria-label={ru.month.prev} onClick={() => onChange(addMonths(month, -1))}>
        <ChevronLeft size={20} aria-hidden="true" />
      </Button>
      <span className={s.monthLabel} aria-live="polite">
        {formatMonth(month)}
      </span>
      <Button variant="ghost" iconOnly aria-label={ru.month.next} disabled={max !== undefined && month >= max} onClick={() => onChange(addMonths(month, 1))}>
        <ChevronRight size={20} aria-hidden="true" />
      </Button>
    </div>
  )
}

/** Скопировать текст в буфер; на пару секунд показывает «Скопировано». */
export function CopyButton({ text, label, variant = 'secondary', block }: { text: string; label: string; variant?: 'primary' | 'secondary'; block?: boolean }) {
  const [done, setDone] = useState(false)
  return (
    <Button
      variant={variant}
      block={block}
      icon={done ? <Check size={18} aria-hidden="true" /> : <Copy size={18} aria-hidden="true" />}
      onClick={async () => {
        await copyText(text)
        setDone(true)
        window.setTimeout(() => setDone(false), 2000)
      }}
    >
      {done ? ru.common.copied : label}
    </Button>
  )
}

export async function copyText(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    // Старые браузеры и http без secure context — через временное поле.
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
  }
}
