/** Уход на внешний сайт (страница банка). Отдельной функцией — чтобы подменять в тестах. */
export function redirectTo(url: string) {
  window.location.assign(url)
}
