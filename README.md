# Окуу

Сервис учёта оплат для учебных центров. ТЗ — [docs/TZ.md](docs/TZ.md), правила работы с кодом — [CLAUDE.md](CLAUDE.md).

## Локальный запуск

```sh
cp .env.example .env   # и заполнить ENCRYPTION_KEY
make db                # PostgreSQL 16 в Docker (порт 5433)
make migrate           # миграции (сервер применяет их и сам при старте)
make run               # API на http://localhost:8080
```

Фронтенд (Node 20+), во втором терминале:

```sh
cd frontend
npm ci
npm run dev            # http://localhost:5173, /api и /webhooks проксируются на :8080
```

Проверки фронтенда: `npm run typecheck`, `npm run lint`, `npm test`, `npm run build` (сборка — в `frontend/dist`).
Клиент API генерируется из `docs/openapi.yaml`: после правки спецификации — `npm run gen:api` (файл
`src/api/schema.d.ts` коммитится). E2E на настоящем бэкенде: `npm run e2e:install` (один раз — Chromium), затем
`npm run e2e` — сам создаёт чистую базу `okuu_e2e` в контейнере из `make db`, поднимает API на :8091 и фронтенд
на :5174 (dev-сервер на :8080/:5173 не трогает); отчёт со скриншотами 390 и 1280px — `npx playwright show-report`.
Без контейнера `okuu-db` (так в CI) — `E2E_PG_URL=postgres://user:pass@host:port npm run e2e` и клиент `psql` на машине.
В CI e2e — отдельная задача; при падении отчёт Playwright лежит в артефакте `playwright-report` прогона.

Тесты: `make test` — с интеграционными на PostgreSQL из `make db`; `make test-short` — только юнит-тесты.
Код запросов: `make sqlc` (sqlc в Docker, сгенерированный код коммитится).
Ручная проверка API: [requests/](requests/) — файлы для расширения VS Code REST Client.
Спецификация API для фронтенда — [docs/openapi.yaml](docs/openapi.yaml) (OpenAPI 3.1; тест сверяет её с роутером
и проверяет по ней реальные ответы), коды ошибок — [docs/errors.md](docs/errors.md).

## Ручная проверка Telegram-бота на dev

Нужен свой тестовый бот (не боевой): создайте его у [@BotFather](https://t.me/BotFather) и пропишите в `.env`
`TELEGRAM_BOT_TOKEN` и `TELEGRAM_BOT_USERNAME` (без `@`). Токен никому не показывайте и не коммитьте.
В dev бот работает через long polling — публичный адрес и вебхук не нужны. Один токен — один запущенный
сервер: второй процесс с тем же токеном получит от Telegram 409 Conflict.

1. **Запустить сервер:** `make db`, затем `make run`. В логе должно быть `telegram: бот запущен (polling)`.
2. **Владелец:** `requests/auth.http` → register (или войдите существующим).
3. **Родитель со своим номером:** в `requests/telegram.http` впишите в `@myPhone` номер своего Telegram-аккаунта
   и выполните запросы 1–2 — появится ученик «Тестов Бот» с родителем на ваш номер (`parent.tg_status = none`).
4. **Приглашение:** запрос 3 — в ответе `bot_url` (`https://t.me/<бот>?start=…`), тексты для WhatsApp
   на русском и кыргызском и `whatsapp_url`.
5. **Привязка в Telegram** (с телефона, на котором ваш номер):
   откройте `bot_url` → «Запустить» → бот покажет центр, детей и текст согласия → «Согласен» →
   «📱 Поделиться номером» → «Готово! Вы подключены к центру…».
   Запрос 4 в `telegram.http` — у родителя `tg_status = active`; в журнале (`audit?action=parent.`) —
   `parent.telegram_link` с `consent_at`.
6. **Команды и меню:** после привязки внизу чата — постоянная клавиатура «💰 Баланс · 💳 Оплатить · 📅 Посещения ·
   ❓ Помощь»; те же команды — в кнопке «Меню» у поля ввода (`setMyCommands` при старте). `/balance` — баланс и
   абонемент, `/pay` — «Оплатить» на каждого ребёнка с долгом (долга нет — «оплачивать нечего»), `/attendance` —
   посещения за месяц; если детей несколько — сначала выбор ребёнка, в ответах кнопка «« Меню». `/stop` —
   уведомления отключены (`tg_status = stopped`); `/start` — снова включены. Непонятный текст — подсказка и меню.
   Язык: по умолчанию бот только русский (`BOT_LANGUAGES=ru`, команды `/lang` нет); `BOT_LANGUAGES=ru,ky`
   включает выбор языка `/lang` (кыргызские тексты сначала проверить носителем языка).
7. **Напоминания и оплата** — `requests/reminders.http`: запись в группу с начислением → «Напомнить»
   (сообщение с суммой, реквизитами и кнопкой «Оплатить») → «Оплатить» в боте присылает ссылку
   `http://localhost:5173/pay/<токен>` (в dev — текстом: Telegram не принимает http-ссылки в кнопках).
   Страницу проверяет `requests/payments.http`: `GET /api/v1/pay/<токен>` — имя ребёнка, сумма, реквизиты.
   Ручная оплата (`/students/{id}/payments`) — в Telegram сразу «спасибо, оплата получена» (9:00–20:00 по Бишкеку;
   ночью — утром).
8. **Чужой номер:** пригласите родителя с другим телефоном и поделитесь своим — бот вежливо откажет,
   приглашение останется действительным.

Что не проверить вручную на dev: блокировку бота (403 → `tg_status = blocked`) и лимиты Telegram удобнее
смотреть в тестах (`backend/internal/httpapi/telegram_test.go`, `backend/internal/telegram`) — там фейковый Telegram.

## Лендинг

Главная страница сервиса — статический HTML и CSS без фреймворков и без JS, в [landing/](landing/):

- `landing/src/index.html` — все тексты главной; `offer.html` и `privacy.html` — **черновики** оферты и политики
  персональных данных (сверху плашка «Черновик — требует проверки юристом», для поисковиков — `noindex`);
- `landing/config.json` — название, слоган, описание для поисковиков, адреса (`siteUrl`, `appUrl`), контакты
  (`whatsapp`, `telegram`, `email`) и реквизиты. В HTML они стоят как `{{ключ}}`. Значения в `[скобках]` —
  заглушки: сборка напоминает, что их пора заполнить;
- `landing/img` — скриншоты демо-центра и картинка для соцсетей, `landing/fonts` — Golos Text (лицензия OFL).

Посмотреть локально (нужен только Node 20+, `npm install` не нужен):

```sh
node landing/build.mjs --serve        # собрать в landing/dist и открыть http://localhost:5180
```

Ссылки «Попробовать бесплатно» и «Войти» ведут на `appUrl`, пока он не заполнен, — это заглушка `[APP_URL]`.
Переснять скриншоты после изменений в приложении — `cd frontend && npm run screenshots` (демо-центр в отдельной
базе `okuu_screens`, свой API на :8091; нужен контейнер из `make db`): картинки обновятся в `landing/img`.

Выложить: заполнить `landing/config.json`, на сервере собрать (`node build.mjs` в контейнере — см. «Запуск»
ниже) и задать `LANDING_DOMAIN` в `deploy/.env`. Caddy отдаёт `landing/dist` на `LANDING_DOMAIN`, приложение —
на `APP_DOMAIN`. Перезапускать Caddy после пересборки не нужно.

## Деплой на VPS

Один сервер (Ubuntu 22.04/24.04, 2 vCPU, 2–4 ГБ) и Docker Compose: `caddy` (HTTPS, статика фронтенда и лендинга) →
`app` (API, вебхуки, фоновые задачи) → `postgres` (том `pgdata`, порт наружу не открыт). Файлы — в [deploy/](deploy/).
CI ([.github/workflows/ci.yml](.github/workflows/ci.yml)) проверяет код и собирает образ; сам деплой пока ручной.

### 1. Сервер и Docker

```sh
# под root или через sudo
apt update && apt install -y ca-certificates curl git
curl -fsSL https://get.docker.com | sh          # Docker Engine + compose plugin
adduser --disabled-password okuu && usermod -aG docker okuu
ufw allow OpenSSH && ufw allow 80/tcp && ufw allow 443 && ufw enable   # наружу только SSH, 80, 443
```

### 2. Код и `.env`

```sh
su - okuu
git clone <репозиторий> /opt/okuu && cd /opt/okuu
cp deploy/.env.example deploy/.env && chmod 600 deploy/.env
chmod +x deploy/backup.sh deploy/restore.sh
```

Заполните `deploy/.env`. Пароли и секреты генерируйте, а не придумывайте:

| Переменная | Как получить |
| --- | --- |
| `POSTGRES_PASSWORD` | `openssl rand -hex 24` (hex — без спецсимволов, их не надо экранировать в URL) |
| `ENCRYPTION_KEY` | `openssl rand -base64 32` — **сохраните отдельно**: без него не расшифровать ключи Finik |
| `TELEGRAM_WEBHOOK_SECRET` | `openssl rand -hex 32` |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_BOT_USERNAME` | боевой бот у @BotFather (не тот, что для dev) |
| `ALERT_TELEGRAM_CHAT_ID` | добавьте бота в служебную группу, напишите в ней сообщение и возьмите `chat.id` из `https://api.telegram.org/bot<токен>/getUpdates` (до запуска сервера — после он снимет updates вебхуком) |
| `APP_DOMAIN`, `APP_ORIGIN`, `ACME_EMAIL` | домен приложения, `https://<домен приложения>`, e-mail для Let's Encrypt |
| `LANDING_DOMAIN` | домен лендинга (например, `okuu.kg`; приложение — `app.okuu.kg`) |

Сервер в prod не стартует, если чего-то не хватает: в логе будет список — «APP_ORIGIN: в prod нужен https://»,
«TELEGRAM_MODE: в prod только webhook», «ENCRYPTION_KEY: нужно минимум 32 байт» и т. п.

### 3. Домен

DNS-записи `A` (и `AAAA`, если есть IPv6) обоих доменов — приложения (`APP_DOMAIN`) и лендинга
(`LANDING_DOMAIN`) — → IP сервера. Порты 80 и 443 должны быть открыты: Caddy сам получит и будет продлевать
сертификаты Let's Encrypt.

Сервер, настроенный до появления лендинга: в `deploy/.env` переименуйте `DOMAIN` в `APP_DOMAIN` и добавьте
`LANDING_DOMAIN`, иначе compose не запустится и напишет, чего не хватает.

### 4. Запуск

Сначала — сборка фронтенда в `frontend/dist` (Caddy отдаёт её как статику, неизвестные пути → `index.html`).
Node на сервер ставить не нужно — собираем в контейнере:

```sh
cd /opt/okuu
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-alpine sh -c "npm ci && npm run build"
ls frontend/dist                # index.html и assets/
docker run --rm -v "$PWD/landing:/landing" -w /landing node:22-alpine node build.mjs   # лендинг → landing/dist
```

Фронтенд обращается к API по тому же домену (`/api/...`), отдельных переменных окружения у него нет.
Лендинг перед первой сборкой — заполнить `landing/config.json` (см. «Лендинг» ниже).

```sh
docker compose -f deploy/docker-compose.prod.yml up -d --build
docker compose -f deploy/docker-compose.prod.yml ps        # app — healthy
docker compose -f deploy/docker-compose.prod.yml logs -f app
```

Миграции применяются при старте. Healthcheck контейнера — `GET /readyz` (БД, миграции, планировщик);
наружу `/healthz` и `/readyz` Caddy не отдаёт. Логи app — JSON, по строке на запрос: `request_id`, метод,
шаблон маршрута (без токенов и телефонов), статус, длительность; тот же `request_id` — в заголовке `X-Request-ID` ответа.

### 5. Webhook Telegram

Ставится сам при старте на `https://<домен>/webhooks/telegram/<TELEGRAM_WEBHOOK_SECRET>` — в логе
`telegram: бот запущен (webhook)`. Проверка (токен не светите в истории shell — начните строку с пробела):

```sh
 curl -s "https://api.telegram.org/bot<токен>/getWebhookInfo"   # url — ваш домен, last_error_message пустой
```

Если сервер в логе пишет «не удалось установить вебхук» — проверьте DNS и сертификат (`docker compose … logs caddy`).

### 6. Бэкапы

`deploy/backup.sh` — `pg_dump | gzip` в `BACKUP_DIR` (по умолчанию `/var/backups/okuu`), архивы старше 30 дней
удаляются. Папки для архивов и лога (один раз, под root), затем cron (`crontab -e` под пользователем `okuu`):

```sh
mkdir -p /var/backups/okuu /var/log/okuu && chown okuu: /var/backups/okuu /var/log/okuu
```

```cron
0 3 * * * /opt/okuu/deploy/backup.sh >> /var/log/okuu/backup.log 2>&1
```

Копию архивов держите вне сервера (ТЗ: «на другое хранилище»), например `rclone sync /var/backups/okuu remote:okuu-backups`
следующей строкой cron.

**Проверка восстановления** — до запуска пилота и потом раз в месяц:

```sh
deploy/restore.sh /var/backups/okuu/okuu-<дата>.sql.gz
```

Скрипт накатывает архив во временную базу `okuu_restore_check`, печатает версию схемы и число строк
(центры, пользователи, ученики, ledger, …) и удаляет её — рабочая база не трогается. Восстановление в рабочую
базу после аварии — `CONFIRM=yes deploy/restore.sh <архив> okuu` при остановленных `app` и `caddy`
(подробно — в начале скрипта).

### 7. Обновление

```sh
cd /opt/okuu
deploy/backup.sh                                            # бэкап перед обновлением
git pull
docker compose -f deploy/docker-compose.prod.yml up -d --build app
docker compose -f deploy/docker-compose.prod.yml ps        # app снова healthy
# если менялся фронтенд — пересобрать; Caddy подхватит новые файлы без перезапуска
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-alpine sh -c "npm ci && npm run build"
# если менялся лендинг (landing/) — так же
docker run --rm -v "$PWD/landing:/landing" -w /landing node:22-alpine node build.mjs
```

Сборка фронтенда заменяет `frontend/dist` целиком; `index.html` отдаётся с `Cache-Control: no-cache`, файлы из
`assets/` (с хэшем в имени) кэшируются навсегда — пользователи получают новую версию при следующем открытии.

При остановке сервер дожидается текущих запросов и фоновых задач (до 20 с) — начисления и рассылки
не обрываются на середине. Откат — `git checkout <прошлый коммит>` и та же команда (миграции назад сами
не откатываются: делайте их обратно совместимыми).

### Сброс пароля (поддержка)

Сотруднику, который работает только в одном центре, пароль сбрасывает владелец: «Сотрудники → ⋮ → Сбросить
пароль» — одноразовая ссылка на 24 часа. Владельцу центра и сотруднику, который работает в нескольких центрах,
ссылку выдаёт поддержка — после того как убедилась, что это он (звонок с его номера, знакомый голос и т. п.):

```sh
cd /opt/okuu
docker compose -f deploy/docker-compose.prod.yml exec app /server user reset-link --phone=0700123456
# Ссылка для смены пароля (Айгуль Асанова), действует до 22.10.2026 15:04:
# https://<домен>/reset/<токен>
```

Локально — `cd backend && go run ./cmd/server user reset-link --phone=0700123456`. Ссылку передайте человеку
лично (в мессенджер на его номер). Она работает один раз; прежние неиспользованные ссылки этого человека
перестают работать; после смены пароля все его сессии закрываются. В журнал его центров пишется
`user.password_reset_issue` и `user.password_reset_done` (без паролей и токенов).

### Демо для показа

Демо-центр «Демо: учебный центр «Пример»» — чтобы показать сервис клиенту без его данных: 40 вымышленных
учеников в 4 группах (3 тарифа, 2 преподавателя и администратор), история оплат за три прошлых месяца разными
способами, 3 отменённые оплаты, 8 должников с разной просрочкой, 5 «ожидают оплаты», 2 ушедших, посещаемость
за две недели (сегодня не отмечено) и журнал. Телефоны — только тестовые `+996 555 0XX XXX`.

```sh
# на сервере
docker compose -f deploy/docker-compose.prod.yml exec app /server demo seed --phone=0555099001 --password='<пароль>'
# локально
cd backend && go run ./cmd/server demo seed --phone=0555099001 --password='<пароль>'
```

Вход — этим телефоном и паролем (владелец). Сотрудники демо с тем же паролем — чтобы показать другие роли:
`0555090001` и `0555090002` — преподаватели, `0555090003` — администратор.
После показа (или перед следующим) — `server demo reset`: демо-центр пересоздаётся с нуля с тем же владельцем
и паролем, даты истории — от сегодняшнего дня. Повторный `demo seed` делает то же, но с новым телефоном и паролем.

- Удаляются и пересоздаются только центры с флагом `is_demo`; настоящие центры и их пользователи не трогаются.
  Телефон, который уже работает в настоящем центре, для демо не принимается (его пароль не меняем).
- В демо-центре не работают Telegram и напоминания: бот его не видит, автонапоминания и уведомления
  («оплата получена», «не был») не отправляются, «Напомнить» и «Пригласить в Telegram» отвечают `demo_center`.
- Остальное работает как обычно: можно принять оплату, отметить занятие, добавить ученика — до следующего `reset`.
- Пароль в командной строке остаётся в истории оболочки: для демо задайте отдельный пароль.

### Перед пилотом

- [ ] Finik: провайдер `backend/internal/payments/finik` — заглушка (TODO). До реализации `FINIK_ENABLED=false`.
- [ ] Тексты бота на кыргызском (`backend/internal/notify/texts_ky.go`) проверить носителем языка.
- [ ] Бэкап сделан и восстановлен через `deploy/restore.sh`; алерт приходит в служебный чат.
