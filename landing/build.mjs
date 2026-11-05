// Сборка лендинга: src/*.html + config.json → dist/ (Caddy отдаёт dist на {$LANDING_DOMAIN}).
// Только стандартная библиотека Node 20+, без npm install.
//
//   node landing/build.mjs                 собрать landing/dist
//   node landing/build.mjs --serve[=PORT]  собрать и открыть на http://localhost:PORT (по умолчанию 5180)
//   node landing/build.mjs --config=FILE   взять настройки из FILE вместо landing/config.json
//   SITE_URL=https://okuu.kg APP_URL=https://app.okuu.kg node landing/build.mjs --prod
//                                          прод: адреса — из переменных (в config.json — локальные для разработки);
//                                          --prod не соберёт с http://, localhost или без SITE_URL/APP_URL
//
// SITE_URL и APP_URL, если заданы, заменяют siteUrl и appUrl из config.json (образ caddy получает их
// из APP_DOMAIN и LANDING_DOMAIN в deploy/.env — см. deploy/caddy.Dockerfile).
//
// В src/*.html (и robots.txt, sitemap.xml) {{ключ}} заменяется значением из config.json (с экранированием HTML). Неизвестный ключ —
// ошибка сборки. Незаполненное значение — «-», пустое или заглушка в [скобках]: обычная сборка предупреждает,
// --prod падает, если оно попало на страницу, а также если в собранных HTML осталась «[ЗАГЛУШКА]» или «уточнить».
// <!-- if ключ=значение -->…<!-- endif --> — блок только при этом значении (например, registration=invite);
// <!-- if ключ -->…<!-- endif --> — только если ключ заполнен, <!-- if !ключ -->…<!-- endif --> — если не заполнен
// (реквизиты исполнителя: пока их нет, строк в оферте нет). Блоки не вкладываются друг в друга.
import { createHash } from 'node:crypto'
import { copyFileSync, cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:http'
import { dirname, extname, join, normalize } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

const root = dirname(fileURLToPath(import.meta.url))
const src = join(root, 'src')
const dist = join(root, 'dist')

// --config=путь — другой файл настроек (например, для тестового домена); по умолчанию landing/config.json.
const configArg = process.argv.find((a) => a.startsWith('--config='))
const config = JSON.parse(readFileSync(configArg ? configArg.slice('--config='.length) : join(root, 'config.json'), 'utf8'))
for (const [key, env] of [['siteUrl', 'SITE_URL'], ['appUrl', 'APP_URL']]) {
  const v = process.env[env]?.trim()
  if (v) config[key] = v.replace(/\/+$/, '')
}
// --prod: адреса в ссылках, canonical, sitemap — только боевые. Ошибка лучше, чем localhost на сайте.
const prod = process.argv.includes('--prod')
if (prod) {
  for (const [key, env] of [['siteUrl', 'SITE_URL'], ['appUrl', 'APP_URL']]) {
    if (!process.env[env]?.trim()) throw new Error(`--prod: задайте ${env} (например, https://okuu.kg) — ${key} из config.json только для разработки`)
    let url
    try {
      url = new URL(config[key])
    } catch {
      throw new Error(`--prod: ${env} — не адрес: ${JSON.stringify(config[key])}`)
    }
    if (url.protocol !== 'https:' || ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname) || url.pathname !== '/') {
      throw new Error(`--prod: ${env} должен быть https://домен без пути и не localhost, получено ${JSON.stringify(config[key])}`)
    }
  }
}
const css = readFileSync(join(src, 'styles.css'))
const values = Object.fromEntries(Object.entries(config).filter(([k]) => !k.startsWith('_')))
Object.assign(values, {
  registerUrl: `${config.appUrl}/register`,
  loginUrl: `${config.appUrl}/login`,
  whatsappUrl: `https://wa.me/${config.whatsapp}?text=${encodeURIComponent(config.whatsappText)}`,
  requestUrl: `https://wa.me/${config.whatsapp}?text=${encodeURIComponent(config.whatsappRequestText)}`,
  telegramUrl: `https://t.me/${config.telegram}`,
  emailUrl: `mailto:${config.email}`,
  cssVersion: createHash('sha256').update(css).digest('hex').slice(0, 10), // styles.css?v=… — кэш навсегда
})

const escape = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')

// registration: invite — кнопки «Оставить заявку» (WhatsApp с готовым текстом), open — «Попробовать бесплатно».
if (!['invite', 'open'].includes(values.registration)) {
  throw new Error(`landing/config.json: registration — invite или open, получено ${JSON.stringify(values.registration)}`)
}

// Не заполнено: пусто, «-» (реквизиты до регистрации ИП) или заглушка в [скобках].
const blank = (v) => /^(|-|—|\[.*\])$/.test(String(v).trim())

function render(file) {
  const html = readFileSync(join(src, file), 'utf8')
    // <!-- if ключ=значение -->, <!-- if ключ -->, <!-- if !ключ --> … <!-- endif --> — блок по значению из config.json.
    .replace(/<!-- if (!?)(\w+)(?:=(\w+))? -->([\s\S]*?)<!-- endif -->\n?/g, (_, not, key, want, body) => {
      if (!(key in values)) throw new Error(`${file}: неизвестный ключ в <!-- if ${key} --> — добавьте его в landing/config.json`)
      const ok = want === undefined ? !blank(values[key]) : values[key] === want
      return ok !== (not === '!') ? body : ''
    })
  return html.replace(/\{\{(\w+)\}\}/g, (_, key) => {
    if (!(key in values)) throw new Error(`${file}: неизвестный ключ {{${key}}} — добавьте его в landing/config.json`)
    if (prod && blank(values[key])) {
      throw new Error(`--prod: ${file}: {{${key}}} не заполнен (${JSON.stringify(values[key])}) — заполните в landing/config.json или выводите строку через <!-- if ${key} -->`)
    }
    return escape(values[key])
  })
}

// --prod: в готовой странице не должно остаться заглушек вида [ДАТА], [РЕКВИЗИТЫ] и пометок «уточнить».
function checkPlaceholders(file, text) {
  const problems = []
  text.split('\n').forEach((line, i) => {
    for (const [re, what] of [
      [/\[[A-ZА-ЯЁ][^\]\n]*\]?/, 'заглушка в [скобках]'],
      [/уточнить/i, '«уточнить»'],
      [/>\s*[-—]\s*</, 'прочерк вместо значения'],
    ]) {
      const m = re.exec(line)
      if (m) problems.push(`  ${file}:${i + 1}: ${what}: …${line.slice(Math.max(0, m.index - 40), m.index + 80).trim()}…`)
    }
  })
  if (problems.length > 0) {
    throw new Error(`--prod: в собранных страницах остались незаполненные места — исправьте landing/src или landing/config.json:\n${problems.join('\n')}`)
  }
}

// Сначала собираем всё в память: при ошибке --prod старый dist остаётся как был.
const rendered = new Map()
for (const file of readdirSync(src)) {
  if (!/\.(html|txt|xml)$/.test(file)) continue
  const text = render(file)
  if (prod && file.endsWith('.html')) checkPlaceholders(file, text)
  rendered.set(file, text)
}

// Очищаем содержимое, а не сам каталог (при разработке dist может быть смонтирован в контейнер).
mkdirSync(dist, { recursive: true })
for (const f of readdirSync(dist)) rmSync(join(dist, f), { recursive: true, force: true })
const sizes = []
for (const file of readdirSync(src)) {
  if (rendered.has(file)) {
    const text = rendered.get(file)
    writeFileSync(join(dist, file), text)
    if (file.endsWith('.html')) sizes.push(`${file} ${(Buffer.byteLength(text) / 1024).toFixed(1)} КБ`)
  } else {
    copyFileSync(join(src, file), join(dist, file))
  }
}
cpSync(join(root, 'img'), join(dist, 'img'), { recursive: true })
cpSync(join(root, 'fonts'), join(dist, 'fonts'), { recursive: true })

const placeholders = Object.entries(values).filter(([, v]) => blank(v))
console.log(`landing/dist: ${sizes.join(', ')}, styles.css ${(css.length / 1024).toFixed(1)} КБ`)
if (placeholders.length > 0) {
  console.warn(`Внимание: в landing/config.json не заполнено: ${placeholders.map(([k, v]) => `${k} ${v}`).join(', ')}` +
    ' (строки в <!-- if ключ --> не выводятся; если значение стоит на странице как есть, --prod не соберёт)')
}

// --serve: локальный просмотр. Как в Caddy: /offer → offer.html, неизвестное — 404.html; gzip для текста.
const serveArg = process.argv.find((a) => a.startsWith('--serve'))
if (serveArg) {
  const port = Number(serveArg.split('=')[1] ?? 5180)
  const types = {
    '.html': 'text/html; charset=utf-8', '.css': 'text/css; charset=utf-8', '.svg': 'image/svg+xml',
    '.webp': 'image/webp', '.jpg': 'image/jpeg', '.png': 'image/png', '.woff2': 'font/woff2', '.txt': 'text/plain; charset=utf-8',
    '.xml': 'application/xml',
  }
  const resolve = (path) => {
    const p = normalize(decodeURIComponent(path.split('?')[0])).replace(/^([/\\])+/, '')
    for (const c of [p, p + '.html', join(p, 'index.html')]) {
      const f = join(dist, c)
      if (f.startsWith(dist) && existsSync(f) && statSync(f).isFile()) return f
    }
    return null
  }
  createServer((req, res) => {
    const file = resolve(req.url ?? '/')
    const status = file ? 200 : 404
    const path = file ?? join(dist, '404.html')
    let body = readFileSync(path)
    const type = types[extname(path)] ?? 'application/octet-stream'
    const headers = { 'Content-Type': type }
    if (/text|xml|svg/.test(type) && /gzip/.test(req.headers['accept-encoding'] ?? '')) {
      body = gzipSync(body)
      headers['Content-Encoding'] = 'gzip'
    }
    res.writeHead(status, headers).end(body)
  }).listen(port, () => console.log(`Лендинг: http://localhost:${port}  (Ctrl+C — остановить)`))
}
