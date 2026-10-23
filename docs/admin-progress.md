# Подключение центров, панель суперадмина, запуск одной командой — журнал

## Часть A. Режимы регистрации — готово

### Что сделано
- Миграция `0015_platform_admin.sql` (сразу на обе части):
  - `centers.status` (`pending | active | blocked`, существующие — `active`), `blocked_reason`, `paid_until`;
  - тариф `plan` с проверкой `free | start | center | network | founder`; новые центры — `free`, прежние пилотные (`pilot`) — `founder`;
  - `users.is_platform_admin`, `users.last_login_at` (заполнен из сессий);
  - таблицы `registration_links` и `platform_audit_log`.
- `REGISTRATION_MODE` = `invite | approval | open` (`internal/config`): по умолчанию в prod — `invite`, локально — `open`; `open` в prod — только если задать явно. Неизвестное значение — сервер не стартует с понятной ошибкой. Плюс `SUPPORT_WHATSAPP` — номер для кнопок WhatsApp.
- Регистрация (`auth.Register`):
  - `invite` без `registration_token` → `403 registration_closed`;
  - ссылка `{APP_ORIGIN}/register/{токен}`: одноразовая, 14 дней, в БД — hex(sha256);
  - ссылка помечается использованной в той же транзакции, поэтому занятый телефон её не «сжигает»;
  - недействительная ссылка → `404 registration_invalid`;
  - ссылка привязывается к созданному центру.
- `approval`: центр создаётся со `status = pending`; по ссылке — сразу `active` (в любом режиме).
- Middleware `RequireCenter` проверяет членство, затем статус: `pending` → `403 center_pending`, `blocked` → `403 center_blocked` на всё под `/c/{id}`. `/me`, смена пароля и выход работают. Чужой центр — по-прежнему `404`: существование не раскрывается.
- Бот, напоминания и оплата для `pending` и `blocked` не работают:
  - автонапоминания пропускают такие центры;
  - событийные уведомления удаляются из очереди;
  - бот не видит родителей и старые приглашения;
  - кнопка «Оплатить» в боте и страница оплаты `/pay/{токен}` не работают.
- API:
  - `GET /auth/registration` → `{mode, support_whatsapp}`;
  - `GET /auth/registration/{token}` → 200 или `404 registration_invalid`;
  - в `/me` — `centers[].status` и `is_platform_admin`;
  - коды описаны в `docs/errors.md` и `docs/openapi.yaml`, `Plan` и `CenterStatus` — схемы.
- Фронтенд:
  - `/register` и `/register/:token`;
  - режим `invite` без ссылки — «Регистрация по приглашению» и кнопка WhatsApp на номер из конфига с готовым текстом;
  - недействительная ссылка — «Ссылка недействительна»;
  - режим `approval` — пометка про проверку заявки и кнопка «Отправить заявку»;
  - центр `pending` — экран «Заявка на рассмотрении», `blocked` — «Доступ приостановлен, свяжитесь с поддержкой». На обоих: «Проверить снова», WhatsApp, «Другие центры», «Выйти»;
  - ответ `center_pending` или `center_blocked` во время работы перечитывает `/me`, и экран сразу переключается;
  - на выборе центра — пометки «Заявка» и «Приостановлен».
- Лендинг: в `landing/config.json` поле `registration: invite | open`:
  - `invite` (стоит сейчас) — кнопки «Оставить заявку» → WhatsApp с текстом `whatsappRequestText` и соответствующие подписи;
  - `open` — «Попробовать бесплатно» → `appUrl/register`;
  - тексты обоих вариантов — в `src/index.html` блоками `<!-- if registration=… -->`, сборка оставляет нужный.
- `.env.example`, `deploy/.env.example`: `REGISTRATION_MODE`, `SUPPORT_WHATSAPP`.
- Тесты:
  - `TestRegistrationInvite`, `TestRegistrationApproval`, `TestRegistrationOpenAndBlocked`;
  - `TestInactiveCenterSilent` (pending и blocked на фейковом Telegram: автонапоминания, очередь, `/balance`, старое приглашение);
  - таблица `TestLoad` для режимов;
  - vitest: закрытая регистрация, ссылка с токеном в запросе, недействительная ссылка, approval, экраны статуса.
- Проверки зелёные:
  - gofmt, vet, sqlc diff, `go test ./...`;
  - typecheck, lint, vitest 142/142, build;
  - сборка лендинга;
  - e2e 12/12.

### Решения на проверку
1. Ссылка регистрации открывает центр сразу активным и в режиме `approval`: вы её выдали — значит, одобрили.
2. Пилотные центры (`plan = 'pilot'`) получили тариф `founder`, новые — `free`. Если «founder» задуман иначе — поменяйте в панели.
3. Первую ссылку регистрации, пока суперадмина ещё нет, выдаёт консоль: `server admin registration-link --note=...`. Так вы зарегистрируете свой центр в режиме `invite` и потом выдадите себе права.
4. Причину блокировки владелец не видит. Экран общий: «приостановлен, свяжитесь с поддержкой»; причина видна только в панели.
5. Начисления `billing.monthly` у заблокированного центра продолжаются: после разблокировки учёт не «дырявый». Заявке начислять некого.
6. `GET /auth/registration/{token}` только говорит «действует / нет»; кому выдана ссылка, не раскрывает.
7. Номер WhatsApp поддержки — в `SUPPORT_WHATSAPP` на сервере. Лендинг берёт свой номер из `landing/config.json` (`whatsapp`). Это два места, держите их одинаковыми.

## Часть B. Панель суперадмина — готово

### Что сделано
- `users.is_platform_admin` (в миграции 0015). Выдаётся только командой `server admin grant --phone=...`, снимается `revoke`. Через API — никак: `PATCH /me` неизвестное поле отклоняет, тест это проверяет.
- `server admin registration-link --note=...` — ссылка регистрации из консоли (первый центр, пока суперадмина нет; аварии).
- Пакет `internal/platform` — все действия суперадмина в одной транзакции с записью в `platform_audit_log`: кто (или «консоль»), что, центр, подробности.
- Маршруты `/api/v1/admin/*` закрывает middleware `RequirePlatformAdmin`: кто не суперадмин, в том числе без входа, получает `404 not_found`. Это проверено на каждом маршруте. Маршруты:
  - `GET /admin/centers?q=&status=` — список с поиском (название, имя или телефон владельца, телефон в любом формате) и фильтром. Поля: название, владелец (имя, телефон), status, plan, paid_until, `payment_overdue`, активных учеников, дата создания, последний вход любого сотрудника (`users.last_login_at`, обновляется при каждом входе), is_demo. Заявки сверху;
  - `GET /admin/centers/{id}` — карточка: ученики по статусам, группы, сотрудники, родители в Telegram, журнал платформы по центру;
  - `POST .../approve`, `.../block` (причина обязательна), `.../unblock`; неподходящий статус → `409 center_status_conflict`;
  - `PATCH /admin/centers/{id}` — `plan` (free | start | center | network | founder) и `paid_until` (null — не задано). Просрочка только подсвечивается, автоматически центр не блокируется;
  - `POST .../owner-password-reset` — ссылка сброса пароля владельца на 24 часа. Консольная `server user reset-link` осталась для аварий;
  - `GET/POST /admin/registration-links`, `DELETE /admin/registration-links/{id}` — одноразовые ссылки с пометкой «для кого», статусы `active | used | revoked | expired`, у использованной — центр. Отозвать использованную → `409 registration_link_closed`;
  - `GET /admin/audit` — журнал платформы.
- Суперадмин видит только агрегаты. Запросы панели не выбирают ФИО учеников, имена и телефоны родителей, операции и журналы центров.
- OpenAPI: тег `admin`, все маршруты и схемы; `docs/errors.md` — раздел «Панель суперадмина».
- Фронтенд — раздел `/admin` со своим каркасом (шапка «Панель платформы», вкладки «Центры» и «Ссылки регистрации», «В приложение», выход):
  - без `is_platform_admin` — «Не найдено»;
  - вход в панель: пункт «Панель платформы» в меню аккаунта, в «Ещё» и на выборе центра; суперадмин без своих центров с главной попадает сразу в панель;
  - таблица центров: на телефоне — карточки с подписями; «оплачено до» в прошлом — оранжевое «просрочено»;
  - карточка центра: факты, действия (одобрить, приостановить с причиной, вернуть, ссылка сброса пароля владельца через диалог «скопировать / WhatsApp»), тариф и «оплачено до», журнал;
  - ссылки регистрации: создать с пометкой (ссылка показывается один раз), список, отозвать.
- Тесты:
  - `TestAdminAccess`: обычный владелец и гость получают 404 на каждый маршрут, ничего не меняется; revoke закрывает панель; grant на неизвестный телефон — ошибка; grant и revoke — в журнале как действия консоли;
  - `TestAdminCenterLifecycle` (все ответы сверяются со спецификацией): approve, block, unblock меняют доступ владельца (`center_pending`, `center_blocked`, 200); блокировка не задевает соседний центр; повторные действия → 409; тариф с прошедшей датой → `payment_overdue`, но доступ есть; ссылка сброса работает; журнал по центру; неизвестный и кривой id → 404;
  - `TestAdminRegistrationLinks`: в БД хэш, ссылка одноразовая, использованную не отозвать, отозванная недействительна;
  - `TestAdminNoPersonalData`: ни один GET-ответ панели не содержит ФИО учеников, имён и телефонов родителей при живых учениках и родителе в Telegram;
  - vitest: 6 тестов панели;
  - e2e `platform.spec.ts`: права «из консоли», блокировка из панели → у владельца экран «Доступ приостановлен» без причины, возврат доступа, одноразовая ссылка регистрации, axe без serious и critical на карточке центра.
- Смоук на :8091 (база `okuu_admincheck`, `REGISTRATION_MODE=invite`):
  - `demo seed`, `admin registration-link`, `admin grant` работают; grant на неизвестный телефон — понятная ошибка;
  - снимки панели и экрана «Регистрация по приглашению» на 390 и 1280px посмотрел глазами и поправил вёрстку: подпись «В приложение» на телефоне — иконкой, выравнивание фильтров, факты карточки на телефоне — подпись над значением;
  - горизонтальной прокрутки нет; процессы остановлены по PID.
- README: раздел «Подключение центров и панель суперадмина».
- Проверки зелёные:
  - gofmt, vet, sqlc diff, `go test ./...`;
  - typecheck, lint, vitest 148/148, build;
  - сборка лендинга;
  - e2e 14/14.

### Решения на проверку
1. Последний вход — новое поле `users.last_login_at`, ставится при каждой новой сессии: вход, регистрация, принятие приглашения. Сессии удаляются при выходе и сроке, по ним «последний вход» терялся бы. Для старых пользователей значение взято из существующих сессий.
2. Владелец в списке — первый по дате пользователь с ролью owner. Если владельца нет (ручная правка БД), имя и телефон пустые, а ссылка сброса отвечает `409 no_owner`.
3. «Просрочено» считается по дате Бишкека; срок в сам день оплаты ещё не просрочен.
4. Демо-центр виден в панели с пометкой «Демо», его тоже можно приостановить. Владелец демо может быть суперадмином, как в смоуке.
5. Списки панели без пагинации: до 500 центров и ссылок. На старте этого хватает, поиск сужает список.
6. Действия с центром отвечают обновлённой карточкой, а не `204` — экран обновляется без лишнего запроса.
7. Сброс пароля владельца пишет запись и в журнал его центров (как поддержка, `by: support`), и в журнал платформы.

## Часть C. Запуск одной командой — готово

### Что сделано
- `make dev` и `npm run dev` в корне (новый `package.json` без зависимостей) запускают `scripts/dev.mjs`, только стандартная библиотека Node:
  1. проверяет Docker и Go; нет `.env` — копирует `.env.example`; нет `frontend/node_modules` — один раз `npm ci`;
  2. `docker compose up -d db` и ожидание `pg_isready` (до 60 с, иначе понятная ошибка);
  3. параллельно API (`go run ./cmd/server`, :8080), приложение (Vite, :5173), лендинг (`landing/build.mjs --serve`, :5180);
  4. логи с префиксами `[api]`, `[app]`, `[landing]`; адреса печатаются в начале;
  5. Ctrl+C или падение любого процесса останавливают все. На Windows — `taskkill /T` по PID своих детей (вместе с сервером, который собрал `go run`, и Vite), на macOS и Linux — по группе процессов.
- Порты меняются через окружение (`HTTP_ADDR`, `APP_ORIGIN`, `VITE_PORT`, `VITE_API_TARGET`, `LANDING_PORT`), база — через `DATABASE_URL`, как обычно.
- README: раздел «Запуск для разработки» (одна команда и адреса); прежний ручной порядок — в «Локальный запуск по частям».
- Проверка из Git Bash на Windows на альтернативных портах (:8091, :5177, :5181), с отдельной базой и выключенным Telegram — ваши :8080 и бот не задеты:
  - все три поднялись: приложение 200, `/api` через прокси Vite 200, лендинг 200, `/readyz` 200;
  - завершил процесс лендинга — скрипт остановил остальные; порты освободились, собранный `go run` сервер не остался висеть;
  - временные базы `okuu_devcheck`, `okuu_admincheck`, `okuu_demo_smoke` удалил.
- Проверки зелёные:
  - gofmt, vet, sqlc diff, `go test ./...`;
  - typecheck, lint, vitest 148/148, build;
  - сборка лендинга;
  - e2e 14/14 (после части B; часть C код приложения не меняет).

### Решения на проверку
1. Ctrl+C в настоящем окне PowerShell я проверить не смог: из скрипта Windows не даёт послать консольный Ctrl+C. Тот же путь остановки проверен через падение процесса. Проверьте, пожалуйста, руками (шаг 1 ниже).
2. Если один процесс упал, останавливаются все: так не остаётся «полуживого» стенда. Перезапуск — той же командой.
3. Скрипт сам создаёт `.env` из `.env.example`, если его нет, и один раз ставит зависимости фронтенда. Существующий `.env` не трогает.
4. `CLAUDE.md` не менял. Если хотите — добавьте в «Команды» строку `make dev` / `npm run dev`.

### Сообщение для коммита (часть C)
```
Add one-command dev start: make dev / npm run dev

- scripts/dev.mjs: docker compose db + readiness wait, then API (:8080),
  Vite (:5173) and landing (:5180) in parallel with prefixed logs;
  Ctrl+C or any crash stops everything (taskkill /T on Windows)
- README: «Запуск для разработки»
```

### Файлы (части A–C)
- Миграция и запросы:
  - `backend/migrations/0015_platform_admin.sql` (новый);
  - `backend/queries/platform.sql` (новый);
  - `backend/queries/{centers,memberships,payments,reminders,sessions,telegram}.sql`;
  - `backend/internal/db/sqlc/*` (сгенерировано, `platform.sql.go` — новый).
- Бэкенд:
  - `backend/internal/auth/registration.go` (новый), `backend/internal/auth/{service,middleware}.go`;
  - `backend/internal/platform/service.go` (новый);
  - `backend/internal/config/{config,config_test}.go`;
  - `backend/internal/httpapi/admin.go` (новый), `backend/internal/httpapi/{auth,account,errors,router}.go`;
  - `backend/internal/reminders/service.go`;
  - `backend/cmd/server/main.go`.
- Тесты бэкенда:
  - `backend/internal/httpapi/{admin_test,registration_test}.go` (новые);
  - `backend/internal/httpapi/{auth_test,settings_test}.go`;
  - `backend/internal/db/{migrate_test,queries_test}.go`.
- Документация API: `docs/openapi.yaml`, `docs/errors.md`.
- Фронтенд:
  - `frontend/src/features/platform/*` (новое), `frontend/src/utils/whatsapp.ts` (новый);
  - `frontend/src/features/auth/{RegisterPage,api,auth.test}.tsx`;
  - `frontend/src/app/{AppLayout,guards,pages,router}.tsx`, `frontend/src/app/{queryClient.ts,layout.module.css}`;
  - `frontend/src/api/{keys,schema.d}.ts`, `frontend/src/i18n/ru.ts`, `frontend/src/test/fixtures.ts`.
- e2e: `frontend/e2e/platform.spec.ts` (новый), `frontend/e2e/helpers.ts`.
- Лендинг: `landing/build.mjs`, `landing/config.json`, `landing/src/index.html`.
- Запуск и деплой:
  - `scripts/dev.mjs`, `package.json` (новые);
  - `Makefile`, `.env.example`, `deploy/.env.example`, `README.md`.
- Журнал: `docs/admin-progress.md`.

## Итог и как проверить руками

1. **Запуск.** В PowerShell в корне: `npm run dev` (или `make dev`). Дождитесь трёх адресов. Ctrl+C должен погасить всё; проверьте, что :8080, :5173 и :5180 освободились (`netstat -ano | findstr ":8080 :5173 :5180"`).
2. **Права суперадмина себе.** Ваш пользователь уже есть: войдите в приложение под своим телефоном. Затем во втором окне:
   ```sh
   cd backend
   go run ./cmd/server admin grant --phone=<ваш телефон>
   ```
   Обновите страницу: в меню аккаунта появится «Панель платформы» (`/admin`). На сервере то же самое: `docker compose -f deploy/docker-compose.prod.yml exec app /server admin grant --phone=...`.
3. **Панель.**
   - «Центры» — ваш центр и демо (если создан `demo seed`). Откройте карточку чужого центра (например, демо).
   - «Приостановить доступ» с причиной → войдите владельцем этого центра в другом браузере: экран «Доступ приостановлен», причины нет. «Вернуть доступ» → «Проверить снова» → работа.
   - Тариф «Старт» и «оплачено до» вчерашней датой → в списке «просрочено», но центр работает.
   - «Ссылка сброса пароля владельца» → откройте её в приватном окне.
4. **Ссылки регистрации.**
   - «Ссылки регистрации» → «Для кого: проверка» → «Создать ссылку» → откройте её в приватном окне и зарегистрируйте центр.
   - Повторно ссылка покажет «Ссылка недействительна».
   - В списке у ссылки статус «Использована» и центр.
5. **Режимы.** В `.env` поставьте `REGISTRATION_MODE=invite` и `SUPPORT_WHATSAPP=<номер>`, перезапустите:
   - `/register` → «Регистрация по приглашению» и WhatsApp;
   - `REGISTRATION_MODE=approval` → новый центр показывает «Заявка на рассмотрении»; одобрите его в панели;
   - уберите переменную — снова `open`.
6. **Лендинг.** `registration: "invite"` в `landing/config.json` (стоит сейчас) — кнопки «Оставить заявку» ведут в WhatsApp; `"open"` — «Попробовать бесплатно».
7. **Перед деплоем** в `deploy/.env`: `REGISTRATION_MODE=invite` (или не задавать — в prod это значение по умолчанию), `SUPPORT_WHATSAPP=...`. Первый центр на чистом сервере — через `server admin registration-link`, затем `admin grant`.

### Сообщение для коммита (части A и B)
```
Add registration modes, center statuses and platform admin panel

- REGISTRATION_MODE invite|approval|open (prod: invite): one-time registration
  links (14 days, sha256 in DB), approval creates pending centers
- centers.status pending|active|blocked: /c/* answers center_pending /
  center_blocked; bot, reminders and pay links skip inactive centers
- users.is_platform_admin via `server admin grant|revoke` only;
  /api/v1/admin/*: centers list and card (aggregates only), approve, block,
  unblock, plan and paid_until, owner password reset link, registration
  links; everything logged to platform_audit_log; non-admins get 404
- frontend: invite-only and status screens, /admin panel
- landing: registration invite|open switches CTA to «Оставить заявку»
```
