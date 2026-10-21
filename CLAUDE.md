# Окуу — сервис учёта оплат для учебных центров

Полное ТЗ: `docs/TZ.md`. Это источник правды: перед задачей прочитай нужные разделы
(«Бизнес-правила», «Схема БД», «API», «Фоновые задачи и структура проекта»).
Если код и ТЗ расходятся — спроси, не придумывай.

## Стек
- Backend: Go 1.23+, chi, pgx/v5 (pgxpool), sqlc, goose (миграции), argon2id.
- DB: PostgreSQL 16. Локально — `docker compose up -d db` (см. docker-compose.yml).
- Frontend: React + Vite + TypeScript (strict), React Router, TanStack Query, react-hook-form + zod — см. «Frontend».

## Структура
См. раздел «Фоновые задачи и структура проекта» в ТЗ. Backend живёт в `backend/`.
- `backend/cmd/server/main.go` — запуск: config → db pool → migrations → router → scheduler.
- `backend/internal/config` — чтение env (`DATABASE_URL`, `HTTP_ADDR`, `ENCRYPTION_KEY`, ...).
- `backend/internal/db` — создание pgxpool + сгенерированный sqlc-код.
- `backend/migrations` — SQL-миграции goose. `backend/queries` — SQL для sqlc.

## Правила кода
- Деньги — только `int64` в тыйынах. Никаких float.
- Баланс никогда не хранится — всегда сумма `ledger` без отменённых (правило R13–R15).
- Каждый запрос к данным центра фильтруется по `center_id` из пути `/c/{centerId}`
  после проверки членства и роли в middleware. Тест изоляции обязателен для каждого эндпоинта.
- Операции в `ledger` не удаляются и не редактируются — только отмена с причиной.
- Ошибки API: `{"error":{"code":"...","message":"..."}}`.
- Секреты только из env. `.env` в git не коммитить, есть `.env.example`.
- Тесты: `go test ./...`. Бизнес-правила R3–R16 — табличные тесты.
  Интеграционные тесты — на реальном Postgres (testcontainers или docker compose).
- Не добавляй зависимости без причины; предпочитай стандартную библиотеку.

## Команды
- `docker compose up -d db` — поднять локальную БД
- `make migrate` — применить миграции
- `make run` — запустить сервер
- `make test` — тесты
- `make sqlc` — сгенерировать код запросов

## Как работать
Делай задачи маленькими шагами. После каждого шага: код компилируется, `go test ./...` зелёный,
коротко опиши, что сделано и что дальше. Telegram и Finik пока не трогай, пока не попросят.

## Frontend (`frontend/`)
- Стек: Vite + React + TypeScript strict, React Router, TanStack Query, react-hook-form + zod,
  CSS Modules + CSS-переменные (`src/styles/global.css`), Radix — только диалоги и меню, иконки lucide-react.
- Клиент API — из `docs/openapi.yaml`: `npm run gen:api` (openapi-typescript → `src/api/schema.d.ts`,
  коммитится), запросы — openapi-fetch через `api` и `unwrap()` из `src/api/client.ts`. Типы API руками не писать.
- Деньги: в API тыйыны, на экране сомы («34 500 с»), ввод в сомах. Перевод — только `src/utils/money.ts`.
- Даты: в API `YYYY-MM-DD`, на экране «19 октября» / «19.10.2026» — `src/utils/date.ts`.
- Тексты — только `src/i18n/ru.ts` (ESLint `react/jsx-no-literals` не пустит строку в JSX).
- Ошибки — по `code` (docs/errors.md) через `errorMessage()` / `applyFieldError()` из `src/utils/errors.ts`;
  401 → вход, сеть → тост «Повторить».
- Цвета: фон #F4F5F1, карточки #FFF (радиус 14px), текст #17201B / #56605A, линии #E3E6E0,
  главный #1E6B47 / #E3F0E8, долг #A84B12 / #FBEDE3, «уважительная» #3E5C8A. Шрифт Golos Text, цифры tabular-nums.
- Mobile-first (390px), нижнее меню; ≥ 1024px — боковое меню, контент до 1100px. Кнопки и поля ≥ 44px.
- Меню и кнопки — по роли из `/me` (`src/app/permissions.ts`); teacher видит «Сегодня» и свои группы.
- Команды (в `frontend/`): `npm run dev` (http://localhost:5173, proxy /api → :8080), `npm run gen:api`,
  `npm run typecheck`, `npm run lint`, `npm test`, `npm run build` (→ `frontend/dist`), `npm run e2e`.
- Структура: `src/features/<раздел>/` — экран, его `api.ts` (хуки TanStack Query) и тесты рядом; общие
  компоненты — `src/components`, ключи кэша — `src/api/keys.ts`, тестовые данные — `src/test/fixtures.ts`.
- E2E (`frontend/e2e`, Playwright): `npm run e2e` сам создаёт чистую базу `okuu_e2e` в контейнере `okuu-db`,
  API на :8091 (`APP_ORIGIN` = фронтенд на :5174), Telegram выключен; dev-сервер :8080/:5173 не трогает.
  Ссылку оплаты для теста вставляет `e2e/helpers.ts` прямо в базу (её выдаёт только бот).
  В CI вместо контейнера — `E2E_PG_URL` (сервис postgres) и `psql`; даты в e2e — в поясе Бишкека.
- «Сегодня» — только из `useCenter().today` (дата в поясе центра из `/me`, освежается ответом `/lessons`),
  не из часов браузера.

## Проверки на живом сервере
- Свой сервер для смоук-проверок запускай только на порту 8091, никогда не на 8080 (8080 — мой dev-сервер).
- Никогда не останавливай процессы по имени (taskkill /IM, pkill). Останавливай только процесс, который запустил сам, по его PID.