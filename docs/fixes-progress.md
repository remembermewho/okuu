# Правки по итогам ручного тестирования: журнал

Части A → I. По каждой: что сделано, решения на проверку, файлы, сообщение для коммита.
Проверки после каждой части: бэкенд — `gofmt`, `go vet ./...`, `make sqlc-diff`, `go test ./...`;
фронтенд — `npm run typecheck`, `npm run lint`, `npm test`, `npm run build`.

---

## Часть A. Показать / скрыть пароль

### Что сделано
- Компонент `PasswordInput` (`src/components/ui/Field.tsx`): поле пароля с кнопкой-глазом справа
  (иконки lucide `Eye` / `EyeOff`, зона нажатия 48×44). `aria-label` «Показать пароль» / «Скрыть пароль»,
  `aria-pressed`, `aria-controls` на поле. Кнопка не забирает фокус (`mousedown` → `preventDefault`), после
  смены `type` фокус и позиция курсора / выделение в поле восстанавливаются. Кнопка `type="button"` — форму не
  отправляет. Поле без автозамены и проверки орфографии.
- Подключено во всех полях пароля: вход, регистрация центра, принятие приглашения; в части C — смена пароля
  и сброс по ссылке.
- Тесты: переключение type, `aria-pressed`, фокус и курсор остаются, значение сохраняется; кнопка не отправляет форму.

### Решения на проверку
- Пароль по умолчанию скрыт на каждом открытии формы; состояние «показан» не запоминается.

### Файлы
`frontend/src/components/ui/{Field.tsx,Field.module.css,index.tsx,PasswordInput.test.tsx}`,
`frontend/src/features/auth/{LoginPage.tsx,RegisterPage.tsx}`, `frontend/src/features/staff/InvitePage.tsx`,
`frontend/src/i18n/ru.ts`, `docs/fixes-progress.md`.

### Сообщение для коммита
```
Add show/hide password toggle to all password fields

- PasswordInput with an eye button: aria-label and aria-pressed, keeps
  focus and caret position, never submits the form
- used on login, center registration and invite acceptance

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

## Часть B. Ожидающие приглашения

### Что сделано
- Бэкенд: `POST /c/{id}/staff/invites/{inviteId}/reissue` (owner) — `staff.ReissueInvite` в одной транзакции
  отзывает приглашение и создаёт новое с теми же ролью и именем (срок снова 7 дней), ответ `201` — как у создания,
  с новым `url`; токен только в ответе, в БД — hex(sha256). Отозванное / использованное / чужое — `404 not_found`.
  Аудит `invite.reissue` (entity — новое приглашение, в `before` — id и срок старого; без токенов).
- `docs/openapi.yaml`: новая операция `reissueInvite`; тест-сверка выполняет reissue и проверяет ответ по схеме.
- Тесты: `TestReissueInvite` (новый id и токен, роль и имя те же, в БД только хэш, в списке только новое, старая
  ссылка — `invite_invalid`, новая работает, повтор по старому id и чужой id — 404, аудит без токенов);
  `TestStaffPermissions` — admin и teacher получают 403; маршрут добавлен в сводный тест изоляции.
- Фронтенд: в «Сотрудниках» блок «Ожидают принятия» — имя (или «Без имени»), роль, «до <дата, время>», кнопки
  «Новая ссылка» (окно со ссылкой, «Скопировать», «Отправить в WhatsApp» и пояснением, что старая больше не
  работает) и «Отозвать» (с подтверждением). Окно ссылки вынесено в общий `ShareLinkDialog` (нужен и в части C).
- Тест: блок, «Новая ссылка» → новая ссылка и WhatsApp, «Отозвать» → DELETE.

### Решения на проверку
- «Новая ссылка» доступна для любого не принятого и не отозванного приглашения, в том числе просроченного (в
  списке просроченные не показываются, но API их перевыпускает).
- Ответ reissue — `201` (создаётся новое приглашение), как у `POST /staff/invites`.

### Файлы
`backend/internal/staff/service.go`, `backend/internal/audit/audit.go`, `backend/internal/httpapi/{staff.go,router.go}`,
`backend/internal/httpapi/{staff_test.go,isolation_test.go,openapi_test.go}`, `docs/openapi.yaml`,
`frontend/src/api/schema.d.ts`, `frontend/src/components/{ShareLinkDialog.tsx,shared.module.css}`,
`frontend/src/features/staff/{api.ts,StaffPage.tsx,Staff.module.css,staff.test.tsx}`, `frontend/src/i18n/ru.ts`,
`docs/fixes-progress.md`.

### Сообщение для коммита
```
Show pending staff invites with "new link" and revoke

- POST /c/{id}/staff/invites/{inviteId}/reissue (owner): revokes the invite
  and creates a new one with the same role and name in one transaction;
  the new url is returned once; audit invite.reissue
- staff screen: "Pending" block with name, role, expiry, "New link" and
  "Revoke"; shared dialog for one-time links (copy, WhatsApp)
- tests: reissue flow, permissions, isolation, spec check

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

## Часть C. Личный кабинет и сброс пароля

### Что сделано
- **Бэкенд, личный кабинет** (`internal/auth/account.go`, `internal/httpapi/account.go`):
  - `PATCH /me {name}` — только имя (телефон — логин; лишнее поле → `400 bad_request`), ответ — как `/me`;
  - `POST /me/password {current_password, new_password}` — проверка текущего (неверный → `400 validation`,
    поле `current_password`), новый — по правилам регистрации (поле `new_password`), лимит неудачных попыток —
    тот же лимитер, что у входа (5 за 15 минут, ключи «пользователь» и «IP») → `429 too_many_attempts` с
    `Retry-After`; при успехе удаляются все ДРУГИЕ сессии, текущая остаётся;
  - `POST /auth/logout-all` — удаляет все сессии пользователя, включая текущую, и cookie.
- **Сброс пароля сотрудника владельцем:** миграция `0012_password_resets.sql` (таблица `password_resets`:
  token = hex(sha256), user, center — кто выдал (NULL — поддержка), created_by, expires_at = now()+24 ч,
  used_at, revoked_at).
  - `POST /c/{id}/staff/{userId}/password-reset` (owner) → `201 {url, name, expires_at}`, url =
    `{APP_ORIGIN}/reset/{token}`, токен только в ответе. Разрешено, только если сотрудник состоит ТОЛЬКО в
    этом центре и он не owner, иначе `409 reset_not_allowed`; не сотрудник центра → `404`. Новая ссылка
    отзывает прежние неиспользованные ссылки этого пользователя.
  - Публично: `GET /password-reset/{token}` → `{name, center_name}`; `POST /password-reset/{token}
    {new_password}` → новый пароль, удаление всех сессий, `used_at` (атомарно: из двух одновременных пройдёт
    один); неверный пароль ссылку не расходует. Неизвестная / просроченная / заменённая / использованная —
    одинаковый `404 reset_invalid`.
- **Поддержка:** `server user reset-link --phone=...` печатает одноразовую ссылку (24 ч), в том числе для
  владельца; раздел «Сброс пароля (поддержка)» в README. Проверено на временной базе `okuu_smoke` (создана и
  удалена): ссылка напечатана, в БД — только 64-символьный хэш; неизвестный телефон — «пользователь не найден».
- **Аудит** (без паролей и токенов): `user.password_change`, `user.logout_all`, `user.password_reset_issue`,
  `user.password_reset_done`.
- `docs/openapi.yaml`: 6 новых операций и схемы (`PatchMeRequest`, `ChangePasswordRequest`,
  `NewPasswordRequest`, `PasswordResetLink`, `PublicPasswordReset`); тест-сверка проходит их все.
  `docs/errors.md`: раздел «Пароль и сброс» (`reset_not_allowed`, `reset_invalid`, поля `current_password` /
  `new_password`, 429 для смены пароля). Сообщение `too_many_attempts` — «Слишком много неудачных попыток»
  (не только вход).
- **Тесты бэкенда:** имя (+ пустое, лишнее поле, без входа); смена пароля (неверный текущий, короткий новый,
  успех: текущая сессия жива, другая закрыта, старый пароль не работает, новый работает, аудит без паролей);
  лимит 5 попыток → 429; выход везде (все сессии, cookie, аудит, без входа 401); сброс владельцем (новая
  ссылка отменяет прежнюю, в БД хэш, публичная страница, короткий пароль не расходует ссылку, сессии закрыты,
  повтор → 404, аудит без токена); одинаковый 404 для просроченной / использованной / неизвестной / мусорной /
  хэша; `reset_not_allowed` (сотрудник двух центров — для обоих владельцев, владелец сам себе), чужой центр
  → 404, admin и teacher → 403; сброс поддержкой (владелец, центр не указан, аудит без автора). Маршрут —
  в сводном тесте изоляции. `migrate_test` знает про новую таблицу.
- **Фронтенд:**
  - `/account` «Профиль» (вход из меню аккаунта и из «Ещё»): имя (сохранение), телефон только для чтения с
    пояснением, центры с ролями (ссылки), «Сменить пароль» (окно: текущий, новый, повтор — все с глазом;
    «пароли не совпадают», «совпадает с текущим», ошибки сервера под полями), «Выйти на всех устройствах» (с
    подтверждением → экран входа). «Назад» ведёт туда, откуда пришли.
  - «Сотрудники → ⋮ → Сбросить пароль» (owner, не для владельца) → окно со ссылкой, «Скопировать», «Отправить
    в WhatsApp»; `reset_not_allowed` — тост «…обратитесь в поддержку».
  - `/reset/:token` (без входа): «<имя>, центр «…»», новый пароль и повтор → «Пароль изменён» + «Войти»;
    недействительная ссылка — понятный экран.
  - Тесты: профиль (имя, телефон readonly, центры, сохранение), смена пароля (несовпадение без запроса, глаз у
    трёх полей, ошибка текущего под полем, успех), выход везде → вход, пункт меню и «Назад», сброс по ссылке
    (короткий пароль, успех), недействительная ссылка, «Сбросить пароль» в сотрудниках и `reset_not_allowed`.

### Решения на проверку
- **Аудит событий аккаунта.** Журнал привязан к центру (`audit_log.center_id NOT NULL`), а смена пароля и выход
  везде — события пользователя. Пишем их в журнал КАЖДОГО центра пользователя (владельцам полезно видеть).
  Выдача ссылки владельцем — только в его центр; использование — в центр, выдавший ссылку (от поддержки — во
  все центры пользователя, автор — пустой).
- **Неверный текущий пароль — `400 validation` с полем `current_password`**, а не новый код: так ошибка сразу
  встаёт под поле формы.
- **Лимит смены пароля** — тот же лимитер, что у входа; ключ IP общий со входом (неудачи смены пароля с того же
  IP приближают блокировку входа с него — осторожнее, но не мешает обычной работе).
- **После сброса по ссылке пользователь не входит автоматически** — экран «Пароль изменён» и кнопка «Войти».
- **Новая ссылка сброса отзывает прежние** неиспользованные ссылки того же пользователя.
- **`/account` — отдельная страница без меню центра** (свой заголовок с «Назад» туда, откуда пришли): профиль не
  принадлежит центру.
- **Сохранение имени — без аудита** (в задании его нет; имя видно в «Сотрудниках» сразу).

### Файлы
- Бэкенд: `backend/migrations/0012_password_resets.sql`, `backend/queries/{password_resets.sql,users.sql,sessions.sql}`,
  `backend/internal/db/sqlc/{password_resets.sql.go,users.sql.go,sessions.sql.go,models.go}` (sqlc),
  `backend/internal/auth/account.go`, `backend/internal/audit/audit.go`,
  `backend/internal/httpapi/{account.go,router.go,errors.go}`, `backend/cmd/server/main.go`,
  `backend/internal/httpapi/{account_test.go,isolation_test.go,openapi_test.go}`, `backend/internal/db/migrate_test.go`.
- Документы: `docs/openapi.yaml`, `docs/errors.md`, `README.md`.
- Фронтенд: `frontend/src/api/schema.d.ts`, `frontend/src/app/{router.tsx,AppLayout.tsx,pages.tsx}`,
  `frontend/src/components/ui/{index.tsx}`, `frontend/src/features/auth/{api.ts,schemas.ts,AccountPage.tsx,
  AccountPage.module.css,ResetPasswordPage.tsx,AuthLayout.module.css,account.test.tsx}`,
  `frontend/src/features/staff/{api.ts,StaffPage.tsx,staff.test.tsx}`, `frontend/src/i18n/ru.ts`.
- `docs/fixes-progress.md`.

### Сообщение для коммита
```
Add account page, password change and owner-issued password reset

- PATCH /me (name), POST /me/password (checks current password, login rate
  limit, closes other sessions), POST /auth/logout-all
- password_resets table: owner issues a one-time 24h link for a staff
  member who works only in this center and is not an owner (otherwise
  409 reset_not_allowed); public GET/POST /password-reset/{token}, same 404
  for unknown/expired/used links; all sessions closed after reset
- support command: server user reset-link --phone=...
- audit user.password_change, user.logout_all, user.password_reset_issue,
  user.password_reset_done (no passwords or tokens)
- frontend: /account profile, change password dialog, logout everywhere,
  "Reset password" in staff menu with copy/WhatsApp, public /reset/:token
- tests: all flows, reset_not_allowed, isolation, spec check

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

## Часть D. Скидка

### Что сделано
- **Бэкенд:** `PATCH /students/{sid}` — фиксированная скидка больше цены любого АКТИВНОГО тарифа ученика →
  `422 validation`, поле `discount`, текст «Скидка 3 001 с больше цены тарифа «Месяц» (3 000 с)». Процент — как
  раньше 0–100. Без активных записей — только общий предел. В `openapi.yaml` у `patchStudent` — описание и ответ 422.
- Тест `TestStudentPatchFixedDiscountLimit`: без записей можно; с месячным (3 000) и абонементом (4 000) —
  3 001 → 422 и скидка не меняется; 3 000, 0, процент 0 и 100 — можно; 101 % → 400; после завершения месячной
  записи 3 500 — можно.
- **Фронтенд:**
  - Проверка скидки до отправки (общая `checkDiscount`): процент 0–100 (было 1–100); фиксированная — сумма ≥ 0 и
    не больше цены тарифа: при добавлении — выбранного тарифа записи, при изменении — самого дешёвого активного
    тарифа ученика («Не больше цены тарифа «Месяц»: 3 000 с»). Ошибка — под полем.
  - **В карточке ученика не было редактирования данных** — добавлена кнопка «Изменить данные» → окно: ФИО, дата
    рождения, имя и телефон родителя (подсказка: новый телефон отключит Telegram, родитель общий у детей),
    скидка (вид и размер). Уходят только изменённые поля; ошибки сервера (`full_name`, `birth_date`,
    `parent.name`, `parent.phone`, `discount`) — под полями. Удаления учеников нет.
  - Тесты: форма заполнена текущими данными, в PATCH только изменения; 101 % и фиксированная больше цены —
    ошибки без запроса; 422 с сервера — под полем; при добавлении ученика — фиксированная больше цены тарифа.

### Решения на проверку
- **«Активный тариф» — запись без даты завершения** (как `active` в карточке).
- **Фиксированная скидка 0 разрешена** (как и процент 0) — то же, что «без скидки», но не ошибка.
- Добавление ученика без записи в группу — фиксированная скидка ограничена только общим пределом (тарифа ещё нет).
- Если у ученика нет родителя, поля родителя в форме необязательны, но заполнять нужно оба.

### Файлы
`backend/internal/students/students.go`, `backend/internal/httpapi/students_test.go`, `docs/openapi.yaml`,
`frontend/src/features/students/{AddStudentDialog.tsx,edit.test.tsx}`,
`frontend/src/features/students/card/{EditStudentDialog.tsx,StudentPage.tsx}`, `frontend/src/i18n/ru.ts`,
`docs/fixes-progress.md`.

### Сообщение для коммита
```
Validate discounts against tariff prices and add student editing

- PATCH student: fixed discount above the price of any active tariff is
  422 validation (field discount)
- forms: percent 0-100, fixed not above the selected / cheapest active
  tariff price, error under the field before submit
- student card: "Edit details" dialog for full name, birth date, parent
  name and phone, discount; only changed fields are sent

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

## Часть E. Должники: «Просрочено» и «Ожидают оплаты»

### Что сделано
- **Бэкенд:** `GET /debtors?status=overdue|pending` (по умолчанию `overdue`; другое значение → `400`, поле
  `status`). `pending` — баланс < 0, срок самого раннего непогашенного списания ещё не наступил или сегодня;
  сортировка по ближайшему сроку, затем по сумме. В строке новые поля: `due_in_days` (дней до срока, 0 — сегодня;
  у просроченных 0), `reminded_today` (родителю уже напоминали сегодня в поясе центра) и
  `parent.last_reminder_at`. `summary` не изменился (по всему центру, одинаковый в обоих списках). Фильтр
  `group` — в обоих.
- `docs/openapi.yaml`: параметр `status`, поля `Debtor`; тест-сверка проверяет оба списка.
- Тест `TestDebtors` дополнен: `status=pending` (порядок, суммы, `due_in_days` 0 и 5, срок), фильтр по группе в
  pending, неверный status → 400, `reminded_today` (сегодня — да, двое суток назад — нет) и `last_reminder_at`.
- **Фронтенд:** две вкладки «Просрочено N» / «Ожидают оплаты N» (роль tablist, вкладка — в адресе `?tab=pending`);
  обе загружаются сразу, поэтому счётчики учитывают фильтр по группе. В «Ожидают оплаты» — сумма, «срок через N
  дней» / «срок сегодня» и дата, «Напомнить» у каждой строки и «Напомнить всем» (ученикам этого списка). Пометки
  «напоминали сегодня» и «Ушёл» (статус ученика) — в обеих вкладках. Свои пустые состояния. После «Напомнить» список
  перечитывается (пометка появляется сразу).
- Тесты: фильтр группы уходит в оба запроса; вкладки и счётчики, пометки, срок, «Напомнить» в «Ожидают оплаты»;
  «Напомнить всем» в «Ожидают оплаты» — `student_ids` списка.

### Решения на проверку
- **«Напоминали сегодня» — по `parents.last_reminder_at`** (как в задании): если у родителя двое детей и напоминали
  про одного, пометка будет у обоих (сообщение родителю одно на всех детей, R18).
- **«Напомнить всем» во вкладке «Ожидают оплаты» и при фильтре по группе** отправляет `student_ids` видимого
  списка (до 500), а не `all: true` — `all` означает всех должников центра.
- Срок, наступающий сегодня, — в «Ожидают оплаты» («срок сегодня»): по R14 должник — со следующего дня.
- Счётчики на вкладках — число строк с учётом фильтра; карточки итогов сверху — по всему центру, как раньше.

### Файлы
`backend/queries/ledger.sql`, `backend/internal/db/sqlc/ledger.sql.go` (sqlc), `backend/internal/billing/service.go`,
`backend/internal/httpapi/billing.go`, `backend/internal/httpapi/{billing_test.go,openapi_test.go}`, `docs/openapi.yaml`,
`frontend/src/api/{schema.d.ts,keys.ts}`, `frontend/src/features/money/{api.ts,DebtorsPage.tsx,DebtorsPage.module.css,debtors.test.tsx}`,
`frontend/src/i18n/ru.ts`, `docs/fixes-progress.md`.

### Сообщение для коммита
```
Split debtors into overdue and pending tabs

- GET /debtors?status=overdue|pending: pending = negative balance with
  the due date not reached yet, sorted by nearest due date; rows carry
  due_in_days, reminded_today and parent.last_reminder_at
- debtors screen: two tabs with counters (group filter applies to both),
  due date and "Remind" in pending, "reminded today" and "left" marks
- tests for both lists, group filter, reminded_today, spec check

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

## Часть F. Уход ученика с датой

### Что сделано
- **Бэкенд:** `PATCH /students/{sid}` принимает `left_date` вместе со `status = left` (без неё — сегодня). Активные
  записи закрываются `end_date = left_date`; `left_at` — начало этого дня в поясе центра. Проверки: не раньше
  начала самой поздней активной записи и не позже сегодня + 30 (`400`, поле `left_date`); `left_date` без перехода
  в «ушёл» (в том числе у уже ушедшего) — `400`. Состав занятия (запись активна на дату) — уже по `end_date`, поэтому
  в последний день ученик есть, на следующий — нет.
- Список учеников: параметр `hide_left=true` — без ушедших (не действует при заданном `status`); по умолчанию API
  отдаёт всех, как раньше.
- `docs/openapi.yaml`: `PatchStudentRequest.left_date`, параметр `hide_left`; тест-сверка выполняет оба.
- Тесты: `TestStudentLeftWithDate` — ошибки диапазона и без статуса (ученик не меняется), `end_date = left_date`,
  `left_at` = 00:00 Бишкека, повтор → 400, занятие в последний день — ученик есть, на следующий — нет, `hide_left`
  и `status=left`. `TestStudentLeftAt` обновлён: без даты `left_at` — начало сегодняшнего дня (было «сейчас»).
- **Фронтенд:** «Ещё действия → Ученик ушёл» — окно с полем «Последний день занятий» (по умолчанию «сегодня» центра,
  `min` — начало самой поздней активной записи, `max` — сегодня + 30; вне диапазона — ошибка до отправки, ошибка
  сервера — под полем). Список учеников: ушедшие скрыты по умолчанию, флажок «Показать ушедших» (в адресе
  `?show_left=1`); при фильтре по статусу флажок не нужен и скрыт. В должниках ушедшие с долгом остаются с
  пометкой «Ушёл» (часть E).
- Тесты: окно ухода (значения по умолчанию, диапазон, тело запроса), список (по умолчанию `hide_left=true`,
  флажок, фильтр статуса).

### Решения на проверку
- **`left_at` без даты — начало сегодняшнего дня**, а не текущий момент (как просили: «начало этого дня»). На отчёт
  «ушли за месяц» не влияет.
- **Будущая дата ухода (до +30 дней):** записи сразу получают `end_date` в будущем — ученик ходит на занятия до этой
  даты, но в списке групп и фильтре «группа» уже не виден (там — записи без `end_date`), а в карточке запись
  показана как завершённая с датой. Статус «Ушёл» ставится сразу.
- `hide_left` — отдельный параметр, а не смена поведения API по умолчанию: состав группы и импорт ждут прежний ответ.
- Вернуть ушедшего (статус «Учится») можно как раньше; закрытые записи не открываются — записать заново.

### Файлы
`backend/queries/students.sql`, `backend/internal/db/sqlc/students.sql.go` (sqlc), `backend/internal/students/students.go`,
`backend/internal/httpapi/students.go`, `backend/internal/httpapi/{students_test.go,reports_test.go,openapi_test.go}`,
`docs/openapi.yaml`, `frontend/src/api/schema.d.ts`, `frontend/src/features/students/{api.ts,StudentsPage.tsx,edit.test.tsx}`,
`frontend/src/features/students/card/StudentPage.tsx`, `frontend/src/components/shared.module.css`, `frontend/src/i18n/ru.ts`,
`docs/fixes-progress.md`.

### Сообщение для коммита
```
Record the last day of classes when a student leaves

- PATCH student accepts left_date with status=left: active enrollments end
  on that date, left_at is the start of that day in the center timezone;
  range: not before the latest active enrollment start, up to today+30
- students list: hide_left=true; the UI hides left students by default
  with a "Show left" toggle; debtors keep left students with a mark
- "Student left" dialog with the date; tests incl. lesson rosters on the
  last day and the day after

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

## Часть G. Бот: только русский

### Что сделано
- Настройка `BOT_LANGUAGES` (по умолчанию `ru`; можно `ru,ky`; без `ru` или с неизвестным языком сервер не
  стартует — ошибка конфигурации). Тип `notify.Langs`: нулевое значение — только русский.
- Режим `ru`: команды `/lang` нет (ответ — как на непонятный текст), кнопок выбора языка нет, старая кнопка
  `lang:ky` молча игнорируется; все сообщения бота, напоминания о долге и события (оплата получена, абонемент,
  «не был») — по-русски независимо от `parents.lang` и языка клиента Telegram. В подсказках («Готово!», «Вы
  подключены», /help) нет упоминания `/lang`. Сохранённый выбор `parents.lang` не трогается — заработает снова,
  когда включат кыргызский.
- Режим `ru,ky` — как раньше: `/lang`, кнопки, тексты на выбранном языке. Кыргызские тексты и код не удалены.
- `.env.example`, `deploy/.env.example`: `BOT_LANGUAGES=ru`.
- Тесты: `ParseLangs` (табличный), `Langs.For` / `Resolve` в обоих режимах (в режиме ru нет `/lang`, есть
  `/pay` и `/attendance`); конфигурация (ru,ky; без ru; неизвестный язык; по умолчанию — только ru); бот
  `TestBotRussianOnly` — клиент с кыргызским Telegram получает русский, `parents.lang` = ru, `/lang` → «не
  понял», в /help нет `/lang`, старая кнопка `lang:ky` без ответа, при сохранённом `lang=ky` напоминание о
  долге — по-русски. `TestBotLang` и `TestBotRateLimits` идут в режиме `ru,ky`. Тестовое окружение бота
  принимает `BOT_LANGUAGES` (по умолчанию — как в prod, только ru).

### Решения на проверку
- **Тексты приглашения для WhatsApp** (их отправляет администратор, а не бот) остались на двух языках, как и
  `whatsapp_url` на языке родителя — это не сообщения бота. Если нужно и их «только по-русски» — одна строка в
  `parents/service.go`.
- При привязке в режиме `ru` в `parents.lang` записывается `ru` (раньше — язык Telegram, если кыргызский).

### Файлы
`backend/internal/notify/{langs.go,langs_test.go,texts.go,texts_ru.go,texts_ky.go}`,
`backend/internal/config/{config.go,config_test.go}`, `backend/internal/telegram/bot/{bot.go,handlers.go}`,
`backend/internal/reminders/service.go`, `backend/cmd/server/main.go`, `backend/internal/httpapi/telegram_test.go`,
`.env.example`, `deploy/.env.example`, `docs/fixes-progress.md`.

### Сообщение для коммита
```
Bot: Russian only unless BOT_LANGUAGES enables Kyrgyz

- BOT_LANGUAGES=ru (default) or ru,ky; in ru mode there is no /lang and
  no language buttons, all bot messages and reminders are in Russian
  regardless of parents.lang; Kyrgyz texts and code are kept
- tests for both modes, config validation

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

## Часть H. Бот: навигация

### Что сделано
- **Постоянная клавиатура** внизу чата после привязки (и в ответах на /start, /help, непонятный текст):
  «💰 Баланс · 💳 Оплатить · 📅 Посещения · ❓ Помощь» (`is_persistent`, `resize_keyboard`). Нажатие кнопки — то же,
  что команда (тексты кнопок распознаются на обоих языках).
- **setMyCommands при старте** (`bot.SetupCommands`): /start, /balance, /pay, /attendance, /help, /stop с русскими
  описаниями; /lang — только при `BOT_LANGUAGES=ru,ky`. Ошибка — предупреждение в логе, бот работает.
- **/pay** — дети с долгом, кнопка «💳 Оплатить: <ФИО> — <сумма>» на каждого (как раньше, ведёт на ссылку оплаты);
  долга нет — «Долгов нет — оплачивать нечего». **/attendance** — посещения за текущий месяц (в поясе центра) по
  каждому ребёнку: был / не был / по уважительной, с названием центра.
- **Несколько детей** — /balance и /attendance сначала показывают выбор ребёнка (inline-кнопки, «Все дети»; при
  нескольких центрах на кнопке ещё центр). В ответах — кнопка «« Меню» (разделы inline-кнопками). Кнопка с чужим
  или устаревшим id — снова выбор, без чужих данных. Кнопка «Баланс и посещения» из напоминаний работает как раньше.
- **Непонятный текст** от привязанного родителя — короткая подсказка и клавиатура-меню; от непривязанного (в том
  числе /balance, /pay, /attendance, кнопки меню) — объяснение, как получить ссылку у центра. Тексты «/start» и
  «не подключены» тоже объясняют это.
- Telegram-клиент: `SetMyCommands`, поле `is_persistent`; фейковый Telegram в тестах понимает `setMyCommands`.
- Тексты меню — в `notify` (ru и ky; кыргызские — на проверку носителем, как и остальные).
- Тесты (фейковый Telegram): `TestBotNavigation` — /pay (только должник, «Оплатить» + «« Меню»), кнопка
  «💳 Оплатить», /attendance (выбор, один ребёнок, «Все дети», чужой id), /balance (выбор → один ребёнок), «« Меню»
  и каждая кнопка меню, непонятный текст → подсказка + клавиатура, после оплаты «оплачивать нечего»,
  непривязанный → как получить ссылку; `TestBotCommandsMenu` — состав меню команд в обоих режимах. Обновлены
  прежние: после привязки — клавиатура-меню вместо удаления клавиатуры; два ребёнка в двух центрах — сначала выбор;
  «💰 Баланс» = /balance; оплата — через /pay.

### Решения на проверку
- **/balance с одним ребёнком** — сразу ответ (без выбора). С несколькими — выбор и кнопка «Все дети» (прежний вид
  «все центры одним сообщением»).
- Под ответом /balance — «Оплатить» на должников из показанных и «« Меню»; кнопку «Баланс и посещения» убрал из
  ответа самого баланса (она остаётся в напоминаниях и «оплата получена»).
- «« Меню» открывает разделы inline-кнопками (постоянная клавиатура и так видна) — так меню доступно и там, где
  клиент Telegram клавиатуру скрыл.
- Непонятный текст непривязанному — сразу «как получить ссылку» (раньше — «не понял, /help»).

### Файлы
`backend/internal/telegram/{types.go,client.go}`, `backend/internal/telegram/telegramtest/server.go`,
`backend/internal/telegram/bot/{menu.go,handlers.go,bot.go}`, `backend/internal/notify/{texts.go,texts_ru.go,texts_ky.go}`,
`backend/cmd/server/main.go`, `backend/internal/httpapi/{telegram_test.go,payments_test.go}`, `README.md`,
`docs/fixes-progress.md`.

### Сообщение для коммита
```
Bot navigation: persistent menu, /pay, /attendance, child picker

- persistent reply keyboard after linking: Balance, Pay, Attendance, Help
- setMyCommands on start with Russian descriptions
- /pay lists children with debt (or "nothing to pay"), /attendance shows
  this month's attendance per child; several children -> pick a child
  first, answers carry a "« Menu" button
- unknown text: short hint and the menu; unlinked users get instructions
  on getting an invite link from the center
- tests on the fake Telegram

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

## Часть I. Повтор напоминаний о долге

### Что сделано
- Миграция `0013_overdue_repeat.sql`: `centers.overdue_repeat text NOT NULL DEFAULT 'weekly' CHECK (weekly | off)`.
- `reminders.auto`: новый вид `overdue_weekly` — при `weekly`, пока долг не оплачен, на 14, 21, 28… день просрочки
  (раз в 7 дней после 7-го). Ключ дедупликации — с номером недели просрочки:
  `overdue_weekly:<родитель>:<срок>:w<неделя>`. Ограничения прежние: 9:00–20:00 по поясу центра, не больше одного
  напоминания о долге в день на родителя (R17) — новый вид добавлен в этот лимит (`DebtNotifiedSince`), иначе
  лимит бы его не видел. Текст — «оплата просрочена» и «просрочено на N дней». `off` — после 7-го дня только вручную.
- Настройки: `overdue_repeat` в `GET/PATCH /settings` (другие значения → `400`, поле `overdue_repeat`), аудит
  `center.update`; `docs/openapi.yaml` — в схемах `Settings` и `PatchSettingsRequest`.
- «Настройки» на фронтенде: «После 7-го дня просрочки» — «Напоминать раз в неделю, пока не оплатят» / «Больше не
  напоминать (только вручную)» с пояснением всего расписания.
- Тесты: `TestAutoKind` (табличный: -3, 0, 3, 7, 8, 13, 14, 21, 28, 29, 70 дней, weekly / off);
  `TestRemindersOverdueWeekly` — по умолчанию weekly, неверное значение → 400; 7-й день у обоих центров; 13-й —
  тишина; 14-й — `overdue_weekly` с ключом `:w2` и текстом «просрочено на 14 дней», повторный запуск в тот же день —
  ничего (лимит дня); центр с `off` — ничего; 15-й — тишина; 21-й — `:w3`; после оплаты 28-й — тишина; итого два
  повтора. Прежний `TestRemindersAutoKindsAndTiming` (дни 0–16) проходит без изменений. Фронтенд: в PATCH уходит
  только `overdue_repeat`, выбор и сохранение.

### Решения на проверку
- **Отдельный вид `overdue_weekly`**, а не продолжение `overdue_7`: в журнале уведомлений видно, что это повтор.
- **По умолчанию `weekly` для всех центров**, в том числе существующих (так в задании): после миграции родители с
  долгом старше 14 дней получат напоминание в ближайший подходящий день (14, 21, 28…), а не сразу.
- Повтор считается от срока самого раннего непогашенного начисления (как остальные автонапоминания); возвраты и
  поправки (в том числе начальный долг из импорта) по-прежнему не напоминаются автоматически.

### Файлы
`backend/migrations/0013_overdue_repeat.sql`, `backend/queries/{settings.sql,reminders.sql}`,
`backend/internal/db/sqlc/{settings.sql.go,reminders.sql.go,centers.sql.go,models.go}` (sqlc), `backend/internal/settings/service.go`,
`backend/internal/httpapi/settings.go`, `backend/internal/reminders/service.go`, `backend/internal/notify/{outbox.go,texts_ru.go,texts_ky.go}`,
`backend/internal/httpapi/{reminders_test.go,openapi_test.go}`, `docs/openapi.yaml`, `frontend/src/api/schema.d.ts`,
`frontend/src/features/admin/{SettingsPage.tsx,admin.test.tsx}`, `frontend/src/i18n/ru.ts`, `docs/fixes-progress.md`.

### Сообщение для коммита
```
Repeat overdue debt reminders weekly until paid

- centers.overdue_repeat weekly|off (default weekly); after day 7 the
  reminder repeats on days 14, 21, 28... while the debt is unpaid
- same limits: 9:00-20:00, at most one debt reminder per parent per day
  (the new kind counts toward it), dedup key carries the overdue week
- settings field in the API and on the settings screen
- tests: days 14 and 21, off, payment stops repeats, rerun does not duplicate

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

## E2E (Playwright) — обновление под правки

### Что сделано
- Поле пароля теперь с кнопкой «Показать пароль» — в шагах e2e поиск поля «Пароль» / «Новый пароль» точный.
- `teacher.spec.ts`: глаз в поле пароля при принятии приглашения; владелец «Сбросить пароль» → ссылка (с WhatsApp) →
  преподаватель задаёт новый пароль → «Пароль изменён», старая сессия закрыта, повтор ссылки — «недействительна»,
  вход с новым паролем.
- Новый `account.spec.ts`: профиль из меню аккаунта, телефон только для чтения, смена имени; смена пароля (неверный
  текущий — ошибка под полем) — вторая сессия закрыта, текущая жива; «Выйти на всех устройствах».
- `money.spec.ts`: вкладки «Просрочено 1» / «Ожидают оплаты» (пусто); «Ученик ушёл» с последним днём (по умолчанию —
  сегодня центра) → скрыт в «Учениках», виден с «Показать ушедших», в должниках с пометкой «Ушёл».
- `a11y.spec.ts`: ещё вкладка «Ожидают оплаты» и «Профиль» — критических нарушений нет.
- **Найдена и исправлена ошибка** (e2e на настоящем бэкенде): после смены пароля по ссылке очистка кэша
  перезапрашивала уже использованную ссылку, и вместо «Пароль изменён» показывалось «Ссылка недействительна».
  Теперь кэш ссылки не трогается, а «готово» проверяется первым; юнит-тест сделан строже (после смены GET отвечает 404).
- Итог: **12 из 12** (6 сценариев × 390 и 1280px). Отчёт со скриншотами — `npx playwright show-report`.

### Файлы
`frontend/e2e/{helpers.ts,teacher.spec.ts,account.spec.ts,money.spec.ts,a11y.spec.ts}`,
`frontend/src/features/auth/{api.ts,ResetPasswordPage.tsx,account.test.tsx}` (исправление), `docs/fixes-progress.md`.
Эти файлы удобно добавить в коммит части C (или отдельным коммитом ниже).

### Сообщение для коммита
```
Update e2e for password reset, account, debtors tabs and leaving students

- teacher scenario: eye toggle, owner-issued password reset end to end
- account scenario: name, password change closes other sessions,
  logout everywhere
- money scenario: overdue/pending tabs, student leaves with a date,
  hidden by default in the students list
- fix: reset page showed "invalid link" right after a successful reset

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

## Итог

**Готово:** части A–I полностью; проверки после последней части — `gofmt`, `go vet`, `make sqlc-diff`,
`go test ./...`, `npm run typecheck`, `lint`, `test` (136), `build` — зелёные; e2e 12 из 12. Порты 8091/5174
после прогонов свободны, сервер на :8080 не трогался. Миграции новые: `0012_password_resets`, `0013_overdue_repeat`.

**Решения на проверку (сводно):**
1. A — пароль скрыт при каждом открытии формы, состояние «показан» не запоминается.
2. B — «Новая ссылка» работает и для просроченного (не принятого) приглашения; ответ `201`.
3. C — события аккаунта (смена пароля, выход везде, сброс от поддержки) пишутся в журнал каждого центра пользователя.
4. C — неверный текущий пароль — `400 validation` с полем `current_password` (без нового кода).
5. C — лимит смены пароля — тот же лимитер, что у входа; ключ IP общий со входом.
6. C — после сброса по ссылке автоматического входа нет; новая ссылка отзывает прежние; имя без аудита.
7. C — `/account` — отдельная страница без меню центра, «Назад» — туда, откуда пришли.
8. D — фиксированная скидка 0 разрешена; без записи в группу — только общий предел.
9. D — в карточке не было редактирования данных ученика — добавлено окно «Изменить данные».
10. E — «напоминали сегодня» — по `parents.last_reminder_at` (у всех детей родителя).
11. E — «Напомнить всем» во вкладке «Ожидают оплаты» и при фильтре группы — `student_ids` видимого списка.
12. E — срок «сегодня» — в «Ожидают оплаты».
13. F — без даты `left_at` = начало сегодняшнего дня; будущая дата ухода сразу закрывает записи датой в будущем.
14. F — `hide_left` — отдельный параметр, API по умолчанию отдаёт всех.
15. G — тексты WhatsApp-приглашений остались двуязычными (это не бот); при привязке в режиме ru `parents.lang = ru`.
16. H — с одним ребёнком /balance сразу; с несколькими — выбор и «Все дети»; «« Меню» — inline-разделы.
17. H — непонятный текст непривязанному — сразу «как получить ссылку».
18. I — отдельный вид `overdue_weekly`; `weekly` по умолчанию и для существующих центров.

**Как проверить руками:**
1. `docker compose up -d db`, `make run` (API :8080), в `frontend/`: `npm run dev` → http://localhost:5173.
2. **A:** вход / регистрация — глаз в поле пароля: текст виден, курсор на месте, Enter отправляет форму.
3. **B:** «Сотрудники → Пригласить» → закрыть окно → в «Ожидают принятия» «Новая ссылка» (старая ссылка в другом
   браузере — «недействительно», новая — работает) и «Отозвать».
4. **C:** меню аккаунта → «Профиль»: имя, «Сменить пароль» (в другом браузере сессия закроется), «Выйти на всех
   устройствах». «Сотрудники → ⋮ → Сбросить пароль» у преподавателя → открыть ссылку в инкогнито → новый пароль.
   Для владельца или сотрудника двух центров — «обратитесь в поддержку»; поддержка:
   `cd backend && go run ./cmd/server user reset-link --phone=0700...` (см. README).
5. **D:** карточка ученика → «Изменить данные»: ФИО, дата рождения, родитель, скидка (101 % и фиксированная больше
   цены тарифа — ошибка под полем).
6. **E:** «Должники»: вкладки «Просрочено» / «Ожидают оплаты» (ученик со сроком в будущем — во второй, «срок через N
   дней»), фильтр группы в обеих, «Напомнить» → пометка «напоминали сегодня».
7. **F:** карточка → «Ещё действия → Ученик ушёл» → дата; «Сегодня» на дату после неё — ученика нет; «Ученики» —
   скрыт, «Показать ушедших» — виден; в должниках — «Ушёл».
8. **G/H (нужен тестовый бот, README → «Ручная проверка Telegram-бота»):** после привязки внизу клавиатура «Баланс ·
   Оплатить · Посещения · Помощь», в «Меню» у поля ввода — команды; `/lang` не работает (только русский);
   `BOT_LANGUAGES=ru,ky` в `.env` → `/lang` снова есть. С двумя детьми — выбор ребёнка, «« Меню».
9. **I:** «Настройки → После 7-го дня просрочки». Проверить повтор быстрее всего тестом
   `go test ./internal/httpapi -run TestRemindersOverdueWeekly`; вручную — ученику со сроком 14 дней назад
   (поправка с датой через SQL в dev-базе) напоминание придёт в ближайший запуск задачи с 9:00 до 20:00.
10. E2E: `cd frontend && npm run e2e`, отчёт — `npx playwright show-report`.
