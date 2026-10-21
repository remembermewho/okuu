import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { PasswordInput } from './Field'

describe('PasswordInput', () => {
  it('глаз показывает и скрывает пароль, фокус и курсор остаются в поле', async () => {
    render(<PasswordInput label="Пароль" />)
    const input = screen.getByLabelText('Пароль')
    expect(input).toHaveAttribute('type', 'password')
    await userEvent.type(input, 'secret-pass')
    ;(input as HTMLInputElement).setSelectionRange(3, 3)

    const show = screen.getByRole('button', { name: 'Показать пароль' })
    expect(show).toHaveAttribute('aria-pressed', 'false')
    await userEvent.click(show)
    expect(input).toHaveAttribute('type', 'text')
    const hide = screen.getByRole('button', { name: 'Скрыть пароль' })
    expect(hide).toHaveAttribute('aria-pressed', 'true')
    await new Promise((r) => requestAnimationFrame(() => r(null)))
    expect(input).toHaveFocus()
    expect((input as HTMLInputElement).selectionStart).toBe(3)

    await userEvent.click(hide)
    expect(input).toHaveAttribute('type', 'password')
    expect(input).toHaveValue('secret-pass')
  })

  it('кнопка не отправляет форму', async () => {
    let submitted = false
    render(
      <form onSubmit={(e) => (e.preventDefault(), (submitted = true))}>
        <PasswordInput label="Пароль" />
      </form>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'Показать пароль' }))
    expect(submitted).toBe(false)
  })
})
