// Деньги: в API — целые тыйыны (1 сом = 100 тыйын), на экране — сомы.
// Единственное место перевода туда и обратно; компоненты сами не делят и не умножают на 100.

/** Неразрывный пробел (U+00A0): «34 500 с» не разрывается на строки. */
export const NBSP = String.fromCharCode(0xa0)
/** Знак минуса (U+2212, не дефис) — для отрицательных сумм. */
export const MINUS = String.fromCharCode(0x2212)

export const TYIYN_PER_SOM = 100
/** Верхняя граница ввода: столько принимает API (MaxOperationAmount, 1 000 000 000 тыйын). */
export const MAX_SOM = 10_000_000

function groupThousands(digits: string): string {
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, NBSP)
}

/**
 * Сумма в сомах для экрана: тыйыны округляются до сома (R1), тысячи — через пробел.
 * 3_450_000 → «34 500 с», −150_050 → «−1 501 с». sign: '+' перед положительными (аванс).
 */
export function formatSom(tyiyn: number, opts: { sign?: boolean; unit?: boolean } = {}): string {
  const { sign = false, unit = true } = opts
  const som = Math.round(Math.abs(tyiyn) / TYIYN_PER_SOM)
  let s = groupThousands(String(som))
  if (som !== 0 && tyiyn < 0) s = MINUS + s
  else if (som !== 0 && sign && tyiyn > 0) s = '+' + s
  return unit ? `${s}${NBSP}с` : s
}

/**
 * Ввод суммы в сомах → тыйыны. Принимает «1500», «1 500», «1 500,50», «1500.5»
 * (\s в регулярке покрывает и неразрывный пробел).
 * null — не число, больше двух знаков после запятой, отрицательное или больше MAX_SOM.
 */
export function parseSom(input: string): number | null {
  const s = input.replace(/\s/g, '').replace(',', '.')
  if (!/^\d+(\.\d{1,2})?$/.test(s)) return null
  const [whole = '0', frac = ''] = s.split('.')
  const tyiyn = Number(whole) * TYIYN_PER_SOM + Number(frac.padEnd(2, '0'))
  if (!Number.isSafeInteger(tyiyn) || tyiyn > MAX_SOM * TYIYN_PER_SOM) return null
  return tyiyn
}

/** Тыйыны → строка для поля ввода в сомах без группировки: 150_050 → «1500,5», 300_000 → «3000». */
export function tyiynToInput(tyiyn: number): string {
  const whole = Math.trunc(tyiyn / TYIYN_PER_SOM)
  const frac = Math.abs(tyiyn % TYIYN_PER_SOM)
  if (frac === 0) return String(whole)
  return `${whole},${String(frac).padStart(2, '0').replace(/0$/, '')}`
}
