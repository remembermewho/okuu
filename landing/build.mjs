// Сборка лендинга: src/*.html + config.json → dist/ (Caddy отдаёт dist на {$LANDING_DOMAIN}).
// Только стандартная библиотека Node 20+, без npm install.
//
//   node landing/build.mjs                 собрать landing/dist
//   node landing/build.mjs --serve[=PORT]  собрать и открыть на http://localhost:PORT (по умолчанию 5180)
//   node landing/build.mjs --config=FILE   взять настройки из FILE вместо landing/config.json
//
// В src/*.html (и robots.txt, sitemap.xml) {{ключ}} заменяется значением из config.json (с экранированием HTML). Неизвестный ключ —
// ошибка сборки; значение-заглушка в [скобках] — предупреждение (на проде его быть не должно).
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
const css = readFileSync(join(src, 'styles.css'))
const values = Object.fromEntries(Object.entries(config).filter(([k]) => !k.startsWith('_')))
Object.assign(values, {
  registerUrl: `${config.appUrl}/register`,
  loginUrl: `${config.appUrl}/login`,
  whatsappUrl: `https://wa.me/${config.whatsapp}?text=${encodeURIComponent(config.whatsappText)}`,
  telegramUrl: `https://t.me/${config.telegram}`,
  emailUrl: `mailto:${config.email}`,
  cssVersion: createHash('sha256').update(css).digest('hex').slice(0, 10), // styles.css?v=… — кэш навсегда
})

const escape = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')

function render(file) {
  const html = readFileSync(join(src, file), 'utf8')
  return html.replace(/\{\{(\w+)\}\}/g, (_, key) => {
    if (!(key in values)) throw new Error(`${file}: неизвестный ключ {{${key}}} — добавьте его в landing/config.json`)
    return escape(values[key])
  })
}

// Очищаем содержимое, а не сам каталог: Caddy монтирует dist в контейнер, новый каталог он бы не увидел.
mkdirSync(dist, { recursive: true })
for (const f of readdirSync(dist)) rmSync(join(dist, f), { recursive: true, force: true })
const sizes = []
for (const file of readdirSync(src)) {
  if (/\.(html|txt|xml)$/.test(file)) {
    const text = render(file)
    writeFileSync(join(dist, file), text)
    if (file.endsWith('.html')) sizes.push(`${file} ${(Buffer.byteLength(text) / 1024).toFixed(1)} КБ`)
  } else {
    copyFileSync(join(src, file), join(dist, file))
  }
}
cpSync(join(root, 'img'), join(dist, 'img'), { recursive: true })
cpSync(join(root, 'fonts'), join(dist, 'fonts'), { recursive: true })

const placeholders = Object.entries(values).filter(([, v]) => /^\[.*\]$/.test(String(v)))
console.log(`landing/dist: ${sizes.join(', ')}, styles.css ${(css.length / 1024).toFixed(1)} КБ`)
if (placeholders.length > 0) {
  console.warn(`Внимание: в landing/config.json не заполнено: ${placeholders.map(([k, v]) => `${k} ${v}`).join(', ')}`)
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
