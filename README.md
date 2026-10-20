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

## Перед деплоем

- [ ] **TRUSTED_PROXIES**: за Caddy все запросы приходят с адреса прокси — лимит попыток входа по IP станет общим
  на всех пользователей. Брать IP клиента из `X-Forwarded-For` только от доверенного прокси
  (`clientIP` в `backend/internal/httpapi/auth.go`).
- [ ] `APP_ENV=prod`, `APP_ORIGIN=https://<домен>`, `ENCRYPTION_KEY` — в `.env` на сервере (в prod обязательны).
- [ ] Telegram в prod: `TELEGRAM_WEBHOOK_SECRET` (`openssl rand -hex 32`); вебхук ставится сам на
  `APP_ORIGIN/webhooks/telegram/<секрет>` — API и фронтенд должны быть на одном домене за Caddy.
- [ ] Finik: провайдер `backend/internal/payments/finik` — заглушка (TODO). До реализации `FINIK_ENABLED=false`.
- [ ] Тексты бота на кыргызском (`backend/internal/notify/texts_ky.go`) проверить носителем языка.
