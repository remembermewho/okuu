import { describe, expect, it } from 'vitest'
import { formatSom, MINUS, NBSP, parseSom, tyiynToInput } from './money'

const s = (str: string) => str.replace(/ /g, NBSP).replace(/-/g, MINUS)

describe('formatSom', () => {
  it.each([
    [0, '0 с'],
    [100, '1 с'],
    [3_450_000, '34 500 с'],
    [100_000_000, '1 000 000 с'],
    [150_050, '1 501 с'], // тыйыны округляются до сома (R1)
    [150_049, '1 500 с'],
    [49, '0 с'],
    [-150_000, '-1 500 с'],
    [-49, '0 с'], // −0 не показываем
    [99_999, '1 000 с'],
  ])('%d → %s', (tyiyn, want) => {
    expect(formatSom(tyiyn)).toBe(s(want))
  })

  it('знак плюс для аванса и без единицы', () => {
    expect(formatSom(50_000, { sign: true })).toBe(s('+500 с'))
    expect(formatSom(-50_000, { sign: true })).toBe(s('-500 с'))
    expect(formatSom(0, { sign: true })).toBe(s('0 с'))
    expect(formatSom(1_234_500, { unit: false })).toBe(s('12 345'))
  })

  it('неразрывные пробелы', () => {
    expect(formatSom(3_450_000)).not.toContain(' ')
  })
})

describe('parseSom', () => {
  it.each([
    ['1500', 150_000],
    ['1 500', 150_000],
    [`1${NBSP}500`, 150_000],
    ['1 500,50', 150_050],
    ['1500.5', 150_050],
    ['0,01', 1],
    ['0', 0],
    ['  300  ', 30_000],
    ['10000000', 1_000_000_000],
  ])('%s → %d', (input, want) => {
    expect(parseSom(input)).toBe(want)
  })

  it.each(['', 'abc', '-100', '1,5,0', '1.234', '1e3', '10000000,01', '1500 сом'])('%s → null', (input) => {
    expect(parseSom(input)).toBeNull()
  })
})

describe('tyiynToInput', () => {
  it.each([
    [300_000, '3000'],
    [150_050, '1500,5'],
    [150_005, '1500,05'],
    [1, '0,01'],
    [0, '0'],
  ])('%d → %s', (tyiyn, want) => {
    expect(tyiynToInput(tyiyn)).toBe(want)
    expect(parseSom(want)).toBe(tyiyn)
  })
})
