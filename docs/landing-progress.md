# Лендинг и демо-центр — журнал

## Часть A. Демо-центр — готово

### Что сделано
- Миграция `0014_demo_center.sql`: `centers.is_demo boolean NOT NULL DEFAULT false`.
- Демо молчит:
  - автонапоминания не берут демо-центры (`ListCentersForReminders`);
  - событийные уведомления (`paid`, `package_low`, `absent`) демо-центра удаляются из очереди без отправки;
  - бот не видит демо-родителей (`ListChatParents`, `GetChatStudent`);
  - старые приглашения демо в боте недействительны (`GetParentInviteByToken`);
  - «Напомнить» и «Пригласить в Telegram» отвечают `409 demo_center`. Код добавлен в `docs/errors.md`,
    в `openapi.yaml` и в тексты фронтенда.
- Пакет `backend/internal/demo`, подкоманды:
  - `server demo seed --phone --password` — удаляет прежний демо-центр и создаёт новый;
  - `server demo reset` — пересоздаёт с тем же владельцем и паролем.
- Что в демо:
  - 3 тарифа: месяц 3 500, месяц 4 000, абонемент на 8 занятий 4 000;
  - 4 группы с расписанием, 2 преподавателя, 1 администратор;
  - 40 учеников (кыргызские и русские имена), брат и сестра со скидкой 10%;
  - история за 3 прошлых месяца и текущий: оплаты наличными, переводом и картой;
  - 3 отменённые оплаты с причинами: лишний ноль, внесено дважды, не тот способ;
  - 8 должников с просрочкой: 3 срока, 2 срока, 1 срок, частичная оплата, ушедший с долгом;
  - 5 «ожидают оплаты»: 2 новичка и 3 продлённых вчера абонемента;
  - 2 ушедших;
  - посещаемость за 14 дней до сегодня: одно занятие отменено, сегодня не отмечено, абонементы списываются;
  - журнал на все эти действия;
  - отметки «напоминание отправлено» у части должников.
- Телефоны:
  - родители — `+996 555 01X XXX`;
  - сотрудники — `+996 555 090 001…003`;
  - у родителей нет `telegram_chat_id`.
- Тесты:
  - `internal/demo` — картина 8/5/2 на трёх датах (середина месяца, 3-е число, 1-е число в 8 утра); 13 ненулевых балансов, 3 отмены, 4 месяца начислений, ничего в будущем;
  - seed дважды подряд;
  - reset: тот же владелец и пароль, настоящий центр не тронут, пользователи не плодятся;
  - отказы: reset без демо; телефон из настоящего центра (транзакция откатывается, пароль не меняется); телефон демо-сотрудника;
  - `httpapi/demo_test.go` на фейковом Telegram: автонапоминание уходит обычному центру и не уходит демо; «спасибо за оплату» в демо не уходит и очередь пуста; `demo_center` у «Напомнить» и приглашения; бот на `/balance` и `/attendance` отвечает «не подключены», старое приглашение недействительно.
- README: раздел «Демо для показа» — команды на сервере и локально, входы сотрудников, что работает и что нет.
- Смоук на :8091 (отдельная база `okuu_demo_smoke`):
  - seed, повторный seed и reset прошли;
  - вход владельцем работает;
  - должники: 77/77/46/46/46/16/16/16 дней;
  - ожидают оплаты — 5;
  - отчёт за октябрь заполнен по трём способам оплаты;
  - вчерашние занятия отмечены, сегодняшнее не отмечено.
- Проверки: gofmt, vet, sqlc diff, `go test ./...`, typecheck, lint, vitest 136/136, build — зелёные.

### Решения на проверку
1. Данные демо вставляются прямо SQL, а не через сервисы: сервисы не умеют прошлые даты операций и журнала.
   Форматы журнала (before/after) повторяют настоящие. Случайность детерминирована от даты, поэтому reset в тот же день даёт ту же картину.
2. Демо-центр один. `seed` с другим телефоном заменяет прежний демо-центр. Прежний владелец удаляется, если он больше нигде не работает.
3. Телефон, который уже есть в настоящем центре, `seed` не принимает (`ErrPhoneInUse`): иначе мы бы сменили пароль настоящему человеку.
4. У сотрудников демо (2 преподавателя, администратор) тот же пароль, что у владельца, — чтобы показать вид преподавателя. При каждом seed/reset их сессии закрываются.
5. Большинство демо-родителей — `tg_status = active` без `telegram_chat_id`: на экране видно «подключён к Telegram», но отправить некуда. Часть — «не подключён», один — «отключил».
6. «Напомнить» и приглашение в демо сначала проверяют демо и только потом наличие бота: всегда понятный ответ `demo_center`. Порядок изменён и в `reminders.Manual`, и в `parents.CreateInvite`.
7. `billing.monthly` для демо работает как обычно: с 1-го числа появятся начисления нового месяца, и демо «живёт». Чтобы освежить даты истории, нужен `demo reset`.
8. В README пароль передаётся флагом. Он остаётся в истории оболочки — README советует отдельный пароль для демо.

### Файлы
- `backend/migrations/0014_demo_center.sql` (новый)
- `backend/queries/reminders.sql`, `backend/queries/telegram.sql`, `backend/queries/payments.sql`
- `backend/internal/db/sqlc/*.go` (сгенерировано: centers, models, payments, reminders, settings, telegram)
- `backend/internal/reminders/service.go`, `backend/internal/parents/service.go`, `backend/internal/httpapi/errors.go`
- `backend/internal/demo/demo.go`, `backend/internal/demo/demo_test.go` (новые)
- `backend/internal/httpapi/demo_test.go` (новый)
- `backend/cmd/server/main.go`
- `docs/errors.md`, `docs/openapi.yaml`, `frontend/src/api/schema.d.ts`, `frontend/src/i18n/ru.ts`
- `README.md`, `docs/landing-progress.md`

### Сообщение для коммита
```
Add demo center: server demo seed/reset, is_demo silences bot and reminders

- migration 0014: centers.is_demo
- reminders, event notifications and the bot skip demo centers; manual
  reminders and parent invites answer 409 demo_center
- internal/demo: 40 fictional students, 3 months of history, debtors,
  pending, cancellations, attendance and audit; reset recreates only is_demo
- README: how to create a demo for a sales call
```

## Часть B. Скриншоты для лендинга — готово

### Что сделано
- `npm run screenshots` (в `frontend/`) работает через Playwright с конфигом `playwright.screenshots.config.ts`:
  - тот же `e2e/start-backend.mjs`, но с отдельной базой `okuu_screens` (`E2E_DB`);
  - `server demo seed` перед стартом (`E2E_DEMO_PHONE` / `E2E_DEMO_PASSWORD`);
  - API на :8091, фронтенд на :5175;
  - обычный `npm run e2e` эти файлы не запускает: у них маска `*.shot.ts`.
- `e2e/screenshots/landing.shot.ts` снимает экраны на 390px (масштаб 2) и 1280px:
  - «Должники»;
  - «Карточка ученика» — Жумабаев Эрлан: частичная оплата, долг 1 800 с;
  - «Сегодня» — последний понедельник или среда до сегодня: две группы, отметки стоят;
  - «Отчёт» — прошлый полный месяц.
- Картинка бота `bot-390.webp` — свёрстанный макет чата `e2e/screenshots/bot-chat.html`:
  - текст и кнопки берутся из `server demo message`;
  - это настоящее автонапоминание «через 3 дня срок», собранное тем же кодом, что и рассылка (`notify.DebtReminder` + `bot.DebtKeyboard`, тексты из `texts_ru.go`);
  - пример — двое детей одного родителя в одном сообщении.
- PNG переводится в webp самим Chromium (`canvas.toDataURL('image/webp', 0.82)`), без новых зависимостей. Результат — `landing/img/*.webp`, 9 файлов по 41–69 КБ.
- Личных данных нет: только вымышленные имена демо и номера `+996 555 0XX XXX`.
- Проверки зелёные: gofmt, vet, sqlc diff, go test (плюс тест `SampleReminder`), typecheck, lint, vitest, build. `npm run screenshots` — 3/3.

### Решения на проверку
1. В макете чата центр называется «Пример», а не «Демо: учебный центр «Пример»»: иначе в тексте выходит «Центр «Демо: учебный центр «Пример»»». Имя бота в шапке — «Окуу», без логотипов мессенджера.
2. Эмодзи 🏫 и 💳 в макете остались: это настоящий текст бота. Правило «без эмодзи» соблюдаю в вёрстке лендинга, но не в снимке продукта. Если мешает — уберу их из картинки.
3. `server demo message` работает без БД и `.env` (как `healthcheck`) и печатает JSON. Дата — сегодня по Бишкеку, срок — через 3 дня.
4. Скриншоты коммитятся: лендинг статический и собирается без Node. Переснять — `npm run screenshots` (нужны Docker-контейнер `okuu-db` и свободный :8091).

### Файлы
- `frontend/playwright.screenshots.config.ts`, `frontend/e2e/screenshots/{demo.ts,landing.shot.ts,bot-chat.html}` (новые)
- `frontend/e2e/start-backend.mjs`, `frontend/package.json`, `frontend/tsconfig.node.json`
- `backend/internal/demo/message.go` (новый), `backend/internal/demo/demo.go`, `backend/internal/demo/demo_test.go`, `backend/cmd/server/main.go`
- `landing/img/*.webp` (9 картинок)
- `docs/landing-progress.md`

### Сообщение для коммита
```
Add landing screenshots: npm run screenshots from the demo center

- playwright.screenshots.config.ts: own DB okuu_screens, demo seed, :8091/:5175
- debtors, student card, today, report at 390 and 1280px; webp via canvas
- bot chat mockup with the real reminder text (server demo message)
```

## Часть C. Лендинг — готово

### Что сделано
- `landing/` — статический HTML и CSS, ни строчки JS, без фреймворков и npm-зависимостей:
  - `src/index.html` — главная, все её тексты в одном файле:
    1. первый экран: слоган, подзаголовок, «Попробовать бесплатно» → `{{appUrl}}/register`, «Написать в WhatsApp», «Должники» в рамке телефона;
    2. «Знакомо?» — 3 проблемы;
    3. «Как это работает» — 3 шага;
    4. 5 возможностей со скриншотами: на телефоне — снимки 390px в рамке, от 1024px — снимки 1280px через `<picture>`;
    5. цены — таблица и две плашки;
    6. 6 вопросов на `<details>`;
    7. призыв и подвал с контактами и ссылками на `/offer` и `/privacy`.
  - `src/offer.html`, `src/privacy.html` — черновики оферты и политики ПДн по праву КР: центр — держатель (обладатель) массива данных, мы — обработчик. Сверху крупная плашка «Черновик — требует проверки юристом», `noindex`; недостающее — в `[скобках]`.
  - `src/404.html`, `favicon.svg` (как у приложения), `robots.txt`, `sitemap.xml`.
  - `config.json` — название, слоган, description, адреса, контакты, реквизиты. Заглушки `[SITE_URL]`, `[APP_URL]`, `[WHATSAPP]`, `[TELEGRAM]`, `[EMAIL]`, `[ИСПОЛНИТЕЛЬ]` и др.
  - `build.mjs` (только стандартная библиотека Node):
    - подставляет `{{ключ}}` с экранированием; неизвестный ключ — ошибка сборки, заглушки — предупреждение;
    - `styles.css?v=<хэш>`;
    - `--serve[=PORT]` — локальный просмотр (`/offer` → `offer.html`, 404, gzip);
    - `--config=FILE` — другой файл настроек;
    - очищает содержимое `dist`, а не сам каталог (иначе контейнер Caddy не увидел бы новые файлы).
  - `fonts/` — Golos Text 400/600/700, кириллица и латиница, woff2, плюс `OFL.txt`; свой хостинг, без Google Fonts.
- Стиль как у приложения: фон #F4F5F1, белые карточки с радиусом 14, главный цвет #1E6B47, долг #A84B12, Golos Text, цифры tabular-nums. Mobile-first, кнопки и ссылки ≥ 44px, эмодзи нет.
- SEO и доступность:
  - title, description, canonical, Open Graph и Twitter card с `img/og.jpg` 1200×630 (рисует `npm run screenshots` из слогана в `config.json`), favicon, `lang="ru"`;
  - семантика: header/nav/main/section/footer, таблица с caption и scope, ссылка «Перейти к содержанию»;
  - трекеров, аналитики и cookies нет.
- Размеры:
  - HTML главной 22,7 КБ + CSS 12,6 КБ = 35,3 КБ (gzip 9,1 КБ) — меньше 50 КБ;
  - шрифты — 56,5 КБ всего, грузятся только нужные подмножества;
  - картинки — webp, кроме первого экрана все `loading="lazy"`, у всех заданы width/height (CLS 0).
- Проверки на превью :8091 со заполненным тестовым конфигом, Lighthouse 12 (npx, мобильный профиль):

  | Страница | Performance | Accessibility | Best Practices | SEO | Прочее |
  |---|---|---|---|---|---|
  | главная | 99 | 100 | 100 | 100 | LCP 1,8 с, TBT 0, CLS 0 |
  | оферта | 100 | 100 | 100 | 66 | — |
  | политика | 100 | 100 | 100 | 66 | — |

  - SEO 66 у оферты и политики — только из-за намеренного `noindex`;
  - axe (wcag2a/aa, 2.1, 2.2, best-practice) на 4 страницах, 390 и 1280px, вопросы раскрыты: нарушений нет;
  - горизонтальной прокрутки нет на 320/360/390/1280px; таблица цен влезает в 320px.
- Деплой:
  - `deploy/Caddyfile`: `{$LANDING_DOMAIN}` отдаёт `/srv/landing` с `try_files {path} {path}.html`, страницей 404, строгим CSP (`default-src 'none'`, только свои картинки, стили и шрифты), HSTS и кэшем: страницы `no-cache`, `styles.css` и шрифты — год `immutable`, картинки — неделя;
  - приложение — `{$APP_DOMAIN}` (было `{$DOMAIN}`);
  - compose монтирует `../landing/dist`; `deploy/.env.example` — `APP_DOMAIN`, `LANDING_DOMAIN`;
  - Caddyfile проверен `caddy validate` и запуском настоящего `caddy:2.8-alpine` на :8091: пути, 404, CSP, кэш; в браузере под этим CSP ошибок нет, все шрифты и картинки загрузились.
- README: раздел «Лендинг» — как посмотреть локально, переснять скриншоты и выложить; в деплое — два домена, сборка лендинга в контейнере `node:22-alpine` (команда проверена), переход со старого `DOMAIN`.
- CI: в задаче frontend добавлен шаг `node ../landing/build.mjs` — ловит неизвестные `{{ключи}}`.
- Итоговые проверки:
  - gofmt, vet, sqlc diff, `go test ./...`;
  - typecheck, lint, vitest 136/136, build;
  - сборка лендинга;
  - e2e 12/12;
  - `npm run screenshots` 4/4.

### Решения на проверку
1. Тексты главной — в `src/index.html`, контакты, адреса и SEO-строки — в `config.json`. Сборка подставляет их во все три страницы. Чистый статический HTML без сборки потребовал бы править контакты в трёх файлах.
2. Слоган «…и принимает оплату» оставлен как задан. Но онлайн-оплата Finik пока заглушка. Поэтому в тексте честно: «присылает ссылку на оплату», «платит по реквизитам центра». Когда Finik заработает, можно усилить.
3. Ответы, которые нужно подтвердить:
   - «резервные копии каждый день» — верно, если на сервере настроен cron из README;
   - «Как оплатить сервис?» — «переводом или по счёту для ИП и организаций»;
   - в оферте и политике в `[скобках]` — сроки, возвраты, НДС, место хранения данных.
4. Строка «Сеть» в таблице сокращена до «от 300, филиалы» и «от 5 000 сом / по договорённости»: в исходном виде таблица не помещалась на 390px.
5. Оферта и политика закрыты `noindex` и не входят в sitemap, пока черновики. В подвале и FAQ ссылки на них есть.
6. Переменная `DOMAIN` переименована в `APP_DOMAIN`. Серверу, настроенному раньше, нужно переименовать её в `deploy/.env` и добавить `LANDING_DOMAIN`, иначе compose не стартует с понятной ошибкой. Это описано в README.
7. Редирект с `www.` не настроен: без DNS-записи `www` Caddy постоянно пытался бы получить на неё сертификат. Если `www` нужен — две строки в Caddyfile.
8. Картинка для соцсетей — JPEG, не webp: превью ссылок в части мессенджеров webp не понимает.
9. `npx lighthouse` запускался разово через npx и в зависимости не добавлен. axe — из уже установленного `@axe-core/playwright`, скрипт проверки отдельно не сохранял.

### Файлы
- `landing/` (новое): `build.mjs`, `config.json`, `src/{index,offer,privacy,404}.html`, `src/styles.css`, `src/favicon.svg`, `src/robots.txt`, `src/sitemap.xml`, `fonts/*` (6 woff2 и `OFL.txt`), `img/og.jpg`
- `frontend/e2e/screenshots/landing.shot.ts` (картинка для соцсетей)
- `deploy/Caddyfile`, `deploy/docker-compose.prod.yml`, `deploy/.env.example`
- `.github/workflows/ci.yml`, `.gitignore` (`landing/dist/`), `README.md`, `docs/landing-progress.md`

### Сообщение для коммита
```
Add landing page: static HTML/CSS, offer and privacy drafts, Caddy on LANDING_DOMAIN

- landing/src: hero, problems, how it works, features with demo screenshots,
  pricing, FAQ; offer and privacy policy drafts (noindex, "needs lawyer review")
- landing/config.json holds name, SEO strings, URLs and contacts;
  build.mjs renders to landing/dist and serves it locally (--serve)
- self-hosted Golos Text, no JS, no trackers; Lighthouse mobile 99/100/100/100
- Caddyfile: LANDING_DOMAIN serves landing/dist with strict CSP,
  app moves to APP_DOMAIN (rename DOMAIN in deploy/.env)
- CI builds the landing to catch unknown {{keys}}
```

## Итог

Части A–C готовы. Все проверки зелёные. Коммиты не делал: можно одним коммитом на всё или тремя — по сообщениям выше. Файлы части A и B ещё раз в итоговом списке не повторяю.

Как посмотреть лендинг локально (нужен только Node 20+):
```sh
node landing/build.mjs --serve      # http://localhost:5180
```
Демо-центр для показа — `cd backend && go run ./cmd/server demo seed --phone=0555099001 --password='<пароль>'`. Переснять скриншоты — `cd frontend && npm run screenshots`.
