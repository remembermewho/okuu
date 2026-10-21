// Скачивание файлов из API (xlsx): запрос через fetch — чтобы ошибки (401, 403) показать как обычно,
// а не скачать JSON с ошибкой; затем сохранить Blob через временную ссылку.

/** Имя файла из Content-Disposition (filename* по RFC 5987 или filename), иначе fallback. */
export function filenameFrom(disposition: string | null, fallback: string): string {
  if (!disposition) return fallback
  const star = /filename\*\s*=\s*(?:UTF-8|utf-8)''([^;]+)/.exec(disposition)
  if (star?.[1]) {
    try {
      return decodeURIComponent(star[1].trim())
    } catch {
      /* битое имя — ниже обычное */
    }
  }
  const plain = /filename\s*=\s*"?([^";]+)"?/.exec(disposition)
  return plain?.[1]?.trim() || fallback
}

export function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
  // Освобождаем после того, как браузер начал загрузку.
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
