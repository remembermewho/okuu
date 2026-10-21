import { describe, expect, it } from 'vitest'
import { filenameFrom } from './download'

describe('filenameFrom', () => {
  it.each([
    [null, 'f.xlsx'],
    ['attachment', 'f.xlsx'],
    ['attachment; filename="okuu-report-2026-10.xlsx"', 'okuu-report-2026-10.xlsx'],
    ['attachment; filename=okuu-import.xlsx', 'okuu-import.xlsx'],
    [`attachment; filename="x.xlsx"; filename*=UTF-8''%D0%BE%D1%82%D1%87%D1%91%D1%82.xlsx`, 'отчёт.xlsx'],
  ])('%s → %s', (header, want) => {
    expect(filenameFrom(header, 'f.xlsx')).toBe(want)
  })
})
