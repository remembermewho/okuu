// Телефон КР — как на бэкенде (auth.NormalizePhone): +996XXXXXXXXX, 996XXXXXXXXX, 0XXXXXXXXX;
// пробелы, дефисы и скобки допустимы. Проверка на фронтенде — подсказка до отправки; решает сервер.

export function normalizePhone(input: string): string | null {
  const s = input.trim()
  if (!/^\+?[\d\s\-()]+$/.test(s)) return null
  const plus = s.startsWith('+')
  const d = s.replace(/\D/g, '')
  let local: string
  if (plus) {
    if (!d.startsWith('996')) return null
    local = d.slice(3)
  } else if (d.length === 12 && d.startsWith('996')) {
    local = d.slice(3)
  } else if (d.length === 10 && d.startsWith('0')) {
    local = d.slice(1)
  } else {
    return null
  }
  return local.length === 9 ? '+996' + local : null
}

export const isValidPhone = (s: string) => normalizePhone(s) !== null

/** +996700123456 → «+996 700 123 456». */
export function formatPhone(p: string): string {
  const m = /^\+996(\d{3})(\d{3})(\d{3})$/.exec(p)
  return m ? `+996 ${m[1]} ${m[2]} ${m[3]}` : p
}
