# Окуу

Сервис учёта оплат для учебных центров. ТЗ — [docs/TZ.md](docs/TZ.md), правила работы с кодом — [CLAUDE.md](CLAUDE.md).

## Локальный запуск

```sh
cp .env.example .env   # и заполнить ENCRYPTION_KEY
make db                # PostgreSQL 16 в Docker (порт 5433)
make migrate           # миграции (сервер применяет их и сам при старте)
make run               # API на http://localhost:8080
```

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
6. **Команды:** `/balance` — баланс, абонемент, посещения за месяц и кнопки «💳 Оплатить» по детям с долгом;
   `/lang` — русский / кыргызча; `/stop` — уведомления отключены (`tg_status = stopped`); `/start` — снова включены.
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

## Деплой на VPS

Один сервер (Ubuntu 22.04/24.04, 2 vCPU, 2–4 ГБ) и Docker Compose: `caddy` (HTTPS, статика фронтенда) →
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
| `DOMAIN`, `APP_ORIGIN`, `ACME_EMAIL` | домен приложения, `https://<домен>`, e-mail для Let's Encrypt |

Сервер в prod не стартует, если чего-то не хватает: в логе будет список — «APP_ORIGIN: в prod нужен https://»,
«TELEGRAM_MODE: в prod только webhook», «ENCRYPTION_KEY: нужно минимум 32 байт» и т. п.

### 3. Домен

DNS-запись `A` (и `AAAA`, если есть IPv6) домена → IP сервера. Порты 80 и 443 должны быть открыты:
Caddy сам получит и будет продлевать сертификат Let's Encrypt.

### 4. Запуск

```sh
cd /opt/okuu
mkdir -p deploy/frontend        # сюда — сборка фронтенда (vite build), пока можно пусто
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
```

При остановке сервер дожидается текущих запросов и фоновых задач (до 20 с) — начисления и рассылки
не обрываются на середине. Откат — `git checkout <прошлый коммит>` и та же команда (миграции назад сами
не откатываются: делайте их обратно совместимыми).

### Перед пилотом

- [ ] Finik: провайдер `backend/internal/payments/finik` — заглушка (TODO). До реализации `FINIK_ENABLED=false`.
- [ ] Тексты бота на кыргызском (`backend/internal/notify/texts_ky.go`) проверить носителем языка.
- [ ] Бэкап сделан и восстановлен через `deploy/restore.sh`; алерт приходит в служебный чат.
