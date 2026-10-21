import { Eye, EyeOff } from 'lucide-react'
import { useId, useRef, useState, type ComponentProps, type ReactNode } from 'react'
import { ru } from '@/i18n/ru'
import { cx } from '@/utils/cx'
import s from './Field.module.css'

type FieldProps = {
  label: ReactNode
  hint?: ReactNode
  error?: string
  optional?: boolean
  /** Подпись справа внутри поля, например «с». */
  suffix?: ReactNode
}

type Wrapped = FieldProps & { id: string; children: ReactNode; className?: string }

function Wrap({ id, label, hint, error, optional, children, className }: Wrapped) {
  return (
    <div className={cx(s.field, className)}>
      <label className={s.label} htmlFor={id}>
        {label}
        {optional && (
          <span className={s.optional}>
            {' · '}
            {ru.common.optional}
          </span>
        )}
      </label>
      {children}
      {hint && !error && (
        <span className={s.hint} id={id + '-hint'}>
          {hint}
        </span>
      )}
      {error && (
        <span className={s.error} id={id + '-error'} role="alert">
          {error}
        </span>
      )}
    </div>
  )
}

function described(id: string, hint?: ReactNode, error?: string) {
  return error ? id + '-error' : hint ? id + '-hint' : undefined
}

export type InputProps = FieldProps & ComponentProps<'input'>

export function Input({ label, hint, error, optional, suffix, className, id: idProp, ...rest }: InputProps) {
  const auto = useId()
  const id = idProp ?? auto
  return (
    <Wrap id={id} label={label} hint={hint} error={error} optional={optional} className={className}>
      <div className={s.control}>
        <input
          id={id}
          className={cx(s.input, suffix != null && s.withSuffix)}
          aria-invalid={error ? true : undefined}
          aria-describedby={described(id, hint, error)}
          {...rest}
        />
        {suffix != null && (
          <span className={s.suffix} aria-hidden="true">
            {suffix}
          </span>
        )}
      </div>
    </Wrap>
  )
}

/**
 * Поле пароля с кнопкой «показать / скрыть». Кнопка не забирает фокус (mousedown — preventDefault),
 * курсор и выделение в поле восстанавливаются после смены type.
 */
export function PasswordInput({ label, hint, error, optional, className, id: idProp, ref, ...rest }: Omit<InputProps, 'type' | 'suffix'>) {
  const auto = useId()
  const id = idProp ?? auto
  const [shown, setShown] = useState(false)
  const inner = useRef<HTMLInputElement | null>(null)
  const setRef = (el: HTMLInputElement | null) => {
    inner.current = el
    if (typeof ref === 'function') ref(el)
    else if (ref) ref.current = el
  }
  const toggle = () => {
    const el = inner.current
    const focused = el !== null && document.activeElement === el
    const start = el?.selectionStart ?? null
    const end = el?.selectionEnd ?? null
    setShown((v) => !v)
    if (el && focused) {
      requestAnimationFrame(() => {
        el.focus()
        if (start !== null && end !== null) el.setSelectionRange(start, end)
      })
    }
  }
  return (
    <Wrap id={id} label={label} hint={hint} error={error} optional={optional} className={className}>
      <div className={s.control}>
        <input
          id={id}
          ref={setRef}
          type={shown ? 'text' : 'password'}
          className={cx(s.input, s.withToggle)}
          aria-invalid={error ? true : undefined}
          aria-describedby={described(id, hint, error)}
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          {...rest}
        />
        <button
          type="button"
          className={s.toggle}
          aria-label={shown ? ru.ui.hidePassword : ru.ui.showPassword}
          aria-pressed={shown}
          aria-controls={id}
          onMouseDown={(e) => e.preventDefault()}
          onClick={toggle}
        >
          {shown ? <EyeOff size={20} aria-hidden="true" /> : <Eye size={20} aria-hidden="true" />}
        </button>
      </div>
    </Wrap>
  )
}

export type SelectProps = FieldProps & ComponentProps<'select'> & { options: { value: string; label: string }[]; placeholder?: string }

export function Select({ label, hint, error, optional, options, placeholder, className, id: idProp, ...rest }: SelectProps) {
  const auto = useId()
  const id = idProp ?? auto
  return (
    <Wrap id={id} label={label} hint={hint} error={error} optional={optional} className={className}>
      <select id={id} className={s.input} aria-invalid={error ? true : undefined} aria-describedby={described(id, hint, error)} {...rest}>
        {placeholder != null && <option value="">{placeholder}</option>}
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </Wrap>
  )
}

export type TextareaProps = FieldProps & ComponentProps<'textarea'>

export function Textarea({ label, hint, error, optional, className, id: idProp, ...rest }: TextareaProps) {
  const auto = useId()
  const id = idProp ?? auto
  return (
    <Wrap id={id} label={label} hint={hint} error={error} optional={optional} className={className}>
      <textarea id={id} className={s.input} aria-invalid={error ? true : undefined} aria-describedby={described(id, hint, error)} {...rest} />
    </Wrap>
  )
}

/** Группа радиокнопок (fieldset + legend): крупные строки ≥ 44px. */
export function RadioGroup({
  legend,
  name,
  value,
  onChange,
  options,
  error,
}: {
  legend: ReactNode
  name: string
  value: string
  onChange: (v: string) => void
  options: { value: string; label: ReactNode; hint?: ReactNode }[]
  error?: string
}) {
  return (
    <fieldset className={s.radios}>
      <legend className={s.label}>{legend}</legend>
      {options.map((o) => (
        <label key={o.value} className={cx(s.check, s.radio, value === o.value && s.radioOn)}>
          <input type="radio" name={name} value={o.value} checked={value === o.value} onChange={() => onChange(o.value)} />
          <span className={s.checkText}>
            <span>{o.label}</span>
            {o.hint && <span className={s.hint}>{o.hint}</span>}
          </span>
        </label>
      ))}
      {error && (
        <span className={s.error} role="alert">
          {error}
        </span>
      )}
    </fieldset>
  )
}

export function Checkbox({ label, hint, className, ...rest }: { label: ReactNode; hint?: ReactNode } & ComponentProps<'input'>) {
  return (
    <label className={cx(s.check, className)}>
      <input type="checkbox" {...rest} />
      <span className={s.checkText}>
        <span>{label}</span>
        {hint && <span className={s.hint}>{hint}</span>}
      </span>
    </label>
  )
}
