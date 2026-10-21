import { describe, expect, it } from 'vitest'
import { formatPhone, normalizePhone } from './phone'

describe('phone', () => {
  it.each([
    ['0700 123 456', '+996700123456'],
    ['996700123456', '+996700123456'],
    ['+996 (555) 12-34-56', '+996555123456'],
    ['12345', null],
    ['+7 999 123 45 67', null],
    ['07001234567', null],
    ['телефон', null],
  ])('%s → %s', (input, want) => {
    expect(normalizePhone(input)).toBe(want)
  })

  it('формат для экрана', () => {
    expect(formatPhone('+996700123456')).toBe('+996 700 123 456')
  })
})
