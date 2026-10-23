/** Ссылка «написать в WhatsApp» с готовым текстом; number — только цифры (996700123456), пусто — без адресата. */
export function whatsappUrl(number: string | null | undefined, text: string): string {
  // encodeURIComponent кодирует пробел как %20: «+» WhatsApp показал бы как есть.
  return `https://wa.me/${number ?? ''}?text=${encodeURIComponent(text)}`
}
