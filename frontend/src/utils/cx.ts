/** Склейка CSS-классов: пропускает false/undefined. */
export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(' ')
}
