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

## Перед деплоем

- [ ] **TRUSTED_PROXIES**: за Caddy все запросы приходят с адреса прокси — лимит попыток входа по IP станет общим
  на всех пользователей. Брать IP клиента из `X-Forwarded-For` только от доверенного прокси
  (`clientIP` в `backend/internal/httpapi/auth.go`).
- [ ] `APP_ENV=prod`, `APP_ORIGIN=https://<домен>`, `ENCRYPTION_KEY` — в `.env` на сервере (в prod обязательны).
