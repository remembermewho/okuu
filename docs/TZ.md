# ТЗ: сервис учёта для учебных центров (MVP)

Oct 19, 2026 · @Asman

Версия 1 — полное ТЗ для разработки MVP, до интервью с центрами. Правила оплат и пропусков сверяем с первыми пилотными центрами и правим здесь.

## Цель и границы

MVP должен делать одно: показывать, кто и сколько должен, и сам собирать эти деньги через напоминания родителям в Telegram с оплатой по QR. Срок — 4 недели по вечерам, первые пользователи — 3–5 центров на пилоте.

**Делаем:** ученики и группы, посещаемость, начисления и оплаты, долги, бот для родителей, оплата Finik, простой отчёт владельцу, импорт из Excel.

**Не делаем:** LMS (видео, домашки, тесты), воронку продаж и заявки, зарплату преподавателей, расписание с кабинетами, мобильные приложения, WhatsApp, бухгалтерию и ЭСФ. Деньги родителей никогда не проходят через наш счёт.

## Роли

Четыре роли; все, кроме родителя, работают в веб-интерфейсе (адаптирован под телефон).

| Роль | Где работает | Что может |
| --- | --- | --- |
| Владелец | веб | всё: настройки центра, сотрудники, тарифы, отчёты, подключение Finik |
| Администратор | веб | ученики, группы, оплаты, долги, приглашения родителей; без отчёта по выручке и настроек |
| Преподаватель | веб с телефона | только свои группы и отметка посещаемости; деньги не видит |
| Родитель | Telegram-бот | баланс и посещения своих детей, оплата, напоминания |

Вход сотрудников — по телефону и паролю (или код в Telegram). Один аккаунт может быть в нескольких центрах (преподаватель в двух местах).

## Экраны

Девять экранов веба + бот; вид — как в [кликабельном макете](https://claude.ai/artifact/LiheHhQjXpCh8ceDu6jHpi). Все экраны верстаются mobile-first (ширина 390 px), на компьютере — та же вёрстка с боковым меню.

| # | Экран | Роли | Что на нём |
| --- | --- | --- | --- |
| S1 | Вход / регистрация центра | все | телефон + пароль; «Создать центр» (название, ваше имя); вход по ссылке-приглашению |
| S2 | Должники (главная) | владелец, админ | собрано / долг за месяц; «Напомнить всем»; список с суммой, днями просрочки и кнопкой «Напомнить»; фильтр по группе |
| S3 | Сегодня (посещаемость) | преподаватель, админ | занятия на сегодня; внутри — ученики с «Был / Не был / Уважит.», счётчик отмеченных, «Сохранить»; выбор другой даты (до 7 дней назад) |
| S4 | Ученики | владелец, админ | поиск, фильтры (группа, статус, «не в Telegram»), «Добавить», «Импорт из Excel» |
| S5 | Карточка ученика | владелец, админ | баланс, родитель и статус Telegram, группы и тарифы, история операций и посещений; «Напомнить», «Отметить оплату», «Пригласить родителя», «Отменить операцию» |
| S6 | Группы | владелец, админ | список, создание: название, преподаватель, дни недели и время, тариф по умолчанию; состав группы |
| S7 | Отчёт | владелец | месяц: начислено, собрано по способам, долг, пришло/ушло; экспорт xlsx |
| S8 | Настройки | владелец | центр, день начисления и срок оплаты, тарифы, сотрудники и приглашения, напоминания (вкл/выкл, текст), реквизиты и Finik, правило списания при пропуске |
| S9 | Журнал действий | владелец | кто, когда, что изменил; фильтр по сотруднику и типу |
| B1 | Telegram-бот | родитель | см. раздел «Telegram-бот и оплата» |
| B2 | Страница оплаты | родитель | сумма, QR / кнопка банка, статус «оплата получена»; открывается по одноразовой ссылке из бота, без входа |

Нижнее меню: владелец и админ — Долги · Сегодня · Ученики · Ещё (группы, отчёт, настройки); преподаватель видит только «Сегодня» и свои группы.

## Функции MVP

Восемь функций; каждая считается готовой, когда выполнены её критерии.

**F1. Регистрация центра**

- [ ] Владелец создаёт центр: название, телефон, валюта — сом, день начисления (по умолчанию 1-е число).
- [ ] Приглашает администратора и преподавателей по ссылке.

**F2. Тарифы обучения**

- [ ] Два типа: «в месяц» (фиксированная сумма) и «абонемент на N занятий» (сумма + число занятий).
- [ ] У ученика можно задать скидку в сомах или процентах (братья, сестры).

**F3. Ученики и группы**

- [ ] Ученик: ФИО, дата рождения (необяз.), родитель (имя, телефон), группа, тариф, дата начала, статус (учится / пауза / ушёл).
- [ ] Группа: название, преподаватель, дни и время занятий.
- [ ] Поиск ученика по имени и телефону родителя.
- [ ] У одного родителя может быть несколько детей.

**F4. Импорт из Excel**

- [ ] Загрузка .xlsx/.csv по шаблону (ФИО, телефон родителя, группа, тариф, текущий долг).
- [ ] Перед сохранением — предпросмотр и список ошибок по строкам.

**F5. Посещаемость**

- [ ] Преподаватель открывает «Сегодня», видит свои занятия, ставит «был / не был / уважительная» за ≤3 нажатия на ученика.
- [ ] Отметку можно исправить в течение 7 дней; все изменения пишутся в журнал.
- [ ] Для абонемента «был» и «не был» списывают занятие, «уважительная» — нет (правило настраивается в центре).

**F6. Начисления, оплаты, баланс**

- [ ] В день начисления система сама создаёт начисление каждому активному ученику на месячном тарифе; абонемент начисляется при покупке.
- [ ] Оплата вносится вручную (наличные, перевод, карта) или приходит автоматически из Finik.
- [ ] Баланс = сумма оплат − сумма начислений; отрицательный = долг. Операции не удаляются, а отменяются с причиной.
- [ ] Экран «Должники»: список с суммой и днями просрочки, кнопка «напомнить сейчас».

**F7. Отчёт владельцу**

- [ ] За выбранный месяц: начислено, собрано (с разбивкой по способу), долг на конец месяца, активных учеников, пришло и ушло.
- [ ] Экспорт оплат и долгов в Excel.

**F8. Журнал действий**

- [ ] Кто, когда и что изменил в оплатах и посещаемости — защита от «потерянных» наличных.

## Бизнес-правила

Деньги считаем по принципу счёта: баланс ученика — сумма всех неотменённых операций в `ledger`, никаких хранимых «итогов». Номера правил (R1…) используем в тестах.

**Деньги и время**

- **R1.** Все суммы — целые тыйыны (`bigint`), в интерфейсе округляем до сома. Валюта одна — KGS.
- **R2.** Часовой пояс центра по умолчанию Asia/Bishkek; даты занятий и периодов — `date`, события — `timestamptz`.

**Начисления по месячному тарифу**

- **R3.** Период — календарный месяц. В `billing_day` (1–28, по умолчанию 1) создаётся одно начисление на каждую активную запись в группу (`enrollment`) с месячным тарифом. Уникальность (enrollment, период) гарантирует БД — повторный запуск задачи не создаёт дублей.
- **R4.** Срок оплаты = `billing_day` + `due_days` (по умолчанию 5 дней).
- **R5.** Новый ученик в середине месяца: при записи админ выбирает «за текущий месяц: полностью / сумма вручную / не начислять»; следующие месяцы — по R3.
- **R6.** Скидка ученика (сом или %) применяется при создании начисления; в операции храним и цену тарифа, и скидку. Изменение цены тарифа действует со следующего начисления.
- **R7.** Статус «пауза» на дату начисления — начисления нет. Статус «ушёл» — больше не начисляем, долг остаётся и виден в списке с пометкой «ушёл».

**Абонементы**

- **R8.** Покупка абонемента = начисление на его цену + `lessons_left` = N. Срок оплаты — дата покупки + `due_days`.
- **R9.** Отметка «был» и «не был» списывает 1 занятие; «уважительная» — по настройке центра (по умолчанию не списывает). Изменение отметки пересчитывает остаток.
- **R10.** Осталось 1 занятие — родителю напоминание. Остаток 0 — ученик помечается «абонемент закончился»; в посещаемости его можно отметить, остаток уходит в минус, админ видит предупреждение. При продлении минусовые занятия вычитаются из нового.

**Занятия и посещаемость**

- **R11.** Занятие создаётся при первом открытии даты по расписанию группы (уникально по группе + дате + времени). Можно добавить внеплановое занятие или отменить занятие (праздник) — отменённое ничего не списывает.
- **R12.** Преподаватель меняет отметки только своих групп и только 7 дней; владелец и админ — без ограничений. Каждое изменение — в журнал.

**Оплаты, баланс, долг**

- **R13.** Оплата зачисляется на баланс ученика, а не на конкретное начисление. Частичная оплата и переплата разрешены; положительный баланс = аванс, он автоматически покрывает следующие начисления.
- **R14.** Должник = баланс < 0 и срок самого старого непокрытого начисления прошёл. Покрытие считаем FIFO: оплаты гасят самые старые начисления. «Дней просрочки» = сегодня − срок этого начисления.
- **R15.** Операции не удаляются и не редактируются. Ошибка — «отменить» с обязательной причиной (`cancelled_at`, `cancelled_by`, `cancel_reason`); отменённые не входят в баланс. Оплату из Finik отменить может только владелец.
- **R16.** Возврат денег родителю — операция `refund` (уменьшает баланс); ручная поправка — `correction` со знаком и причиной. Обе только владелец и админ.

**Напоминания**

- **R17.** Автонапоминания — по расписанию из раздела «Telegram-бот», не больше 1 в день на родителя, только 9:00–20:00. Ручное «Напомнить» — не чаще 1 раза в час на ученика.
- **R18.** Родитель с несколькими детьми получает одно сообщение со списком детей и суммами. После /stop напоминания не шлём, админ видит «отключил уведомления».
- **R19.** Каждое отправленное сообщение пишется в `notifications` со статусом (sent / failed / blocked). При ошибке Telegram 403 (бот заблокирован) связь с родителем помечается неактивной.

## Telegram-бот и оплата

Родитель ничего не устанавливает: получает ссылку, подключается к боту и платит по QR прямо из напоминания. Один общий бот для всех центров.

**Подключение родителя**

1. Администратор нажимает «Пригласить родителя» — система даёт ссылку `t.me/<bot>?start=<одноразовый токен>` и готовый текст для WhatsApp.
2. Родитель открывает ссылку, бот просит поделиться номером телефона; если номер совпадает — привязка готова.
3. Связь показывается администратору («в Telegram ✓»); по кнопке можно выгрузить список неподключённых.

**Что видит родитель**

- Баланс по каждому ребёнку, остаток занятий в абонементе.
- Посещения за месяц; по желанию центра — уведомление «ребёнок не пришёл на занятие».
- История оплат и кнопка «Оплатить».

**Напоминания** (расписание настраивается в центре):

- за 3 дня до дня оплаты;
- в день оплаты, если баланс отрицательный;
- через 3 и 7 дней просрочки, дальше только вручную;
- когда в абонементе осталось 1 занятие;
- не раньше 9:00 и не позже 20:00 по Бишкеку; текст на русском или кыргызском (выбор родителя).

**Оплата через Finik**

1. Владелец подключает свой счёт мерчанта Finik в настройках (ключи центра, не наши).
2. По кнопке «Оплатить» система создаёт платёж на сумму долга (можно изменить) и отдаёт ссылку или QR.
3. Finik присылает вебхук — оплата записывается автоматически, родитель получает «Спасибо, оплата получена».
4. Вебхук проверяется по подписи и обрабатывается один раз (идемпотентность по ID платежа).

Пока Finik не подключён, бот показывает реквизиты центра (номер MBank и т.п.), а администратор отмечает оплату вручную. MVP можно запускать так.

## Модель данных

Десять таблиц в PostgreSQL; все данные центра помечены `center_id`, деньги — целые тыйыны (`bigint`), не `float`.

| Таблица | Ключевые поля | Заметки |
| --- | --- | --- |
| `centers` | name, phone, billing\_day, timezone, finik\_credentials (зашифр.), plan | наш клиент |
| `users` | phone, password\_hash, telegram\_id, name | сотрудники |
| `memberships` | user\_id, center\_id, role (owner/admin/teacher) | роль в центре |
| `parents` | center\_id, name, phone, telegram\_chat\_id, lang | без пароля |
| `students` | center\_id, parent\_id, full\_name, birth\_date, status, discount |  |
| `groups` | center\_id, name, teacher\_id, schedule (дни, время) |  |
| `enrollments` | student\_id, group\_id, tariff\_id, start\_date, end\_date, lessons\_left | ученик в группе |
| `tariffs` | center\_id, name, type (monthly/package), price, lessons\_count |  |
| `lessons` + `attendance` | group\_id, date; student\_id, status (present/absent/excused) |  |
| `ledger` | center\_id, student\_id, type (charge/payment/refund/correction), amount, method, external\_id, created\_by, cancelled\_at | баланс = сумма по ledger |

Дополнительно: `notifications` (что и когда отправлено, чтобы не слать дважды), `audit_log` (F8), `invites` (одноразовые токены).

## Схема БД

Первая миграция PostgreSQL 16 (`migrations/0001_init.sql`, инструмент — goose или golang-migrate). ID — `uuid` (gen\_random\_uuid), везде `center_id` для изоляции данных.

```sql
CREATE TABLE centers (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name          text NOT NULL,
  phone         text,
  timezone      text NOT NULL DEFAULT 'Asia/Bishkek',
  billing_day   smallint NOT NULL DEFAULT 1 CHECK (billing_day BETWEEN 1 AND 28),
  due_days      smallint NOT NULL DEFAULT 5,
  excused_deducts boolean NOT NULL DEFAULT false,
  reminders_on  boolean NOT NULL DEFAULT true,
  pay_details   text,                -- реквизиты, пока нет Finik
  finik_creds   bytea,               -- зашифровано (AES-GCM)
  plan          text NOT NULL DEFAULT 'pilot',
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  phone         text NOT NULL UNIQUE,
  name          text NOT NULL,
  password_hash text NOT NULL,
  telegram_id   bigint UNIQUE,
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
  user_id   uuid NOT NULL REFERENCES users(id),
  center_id uuid NOT NULL REFERENCES centers(id),
  role      text NOT NULL CHECK (role IN ('owner','admin','teacher')),
  PRIMARY KEY (user_id, center_id)
);

CREATE TABLE parents (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  center_id        uuid NOT NULL REFERENCES centers(id),
  name             text NOT NULL,
  phone            text NOT NULL,
  telegram_chat_id bigint,
  tg_status        text NOT NULL DEFAULT 'none' CHECK (tg_status IN ('none','active','stopped','blocked')),
  lang             text NOT NULL DEFAULT 'ru' CHECK (lang IN ('ru','ky')),
  consent_at       timestamptz,
  UNIQUE (center_id, phone)
);

CREATE TABLE students (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  center_id   uuid NOT NULL REFERENCES centers(id),
  parent_id   uuid REFERENCES parents(id),
  full_name   text NOT NULL,
  birth_date  date,
  status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused','left')),
  discount_kind  text CHECK (discount_kind IN ('fixed','percent')),
  discount_value bigint,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON students (center_id, status);

CREATE TABLE tariffs (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  center_id     uuid NOT NULL REFERENCES centers(id),
  name          text NOT NULL,
  kind          text NOT NULL CHECK (kind IN ('monthly','package')),
  price         bigint NOT NULL CHECK (price >= 0),   -- тыйыны
  lessons_count int,                                  -- для package
  archived      boolean NOT NULL DEFAULT false
);

CREATE TABLE groups (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  center_id   uuid NOT NULL REFERENCES centers(id),
  name        text NOT NULL,
  teacher_id  uuid REFERENCES users(id),
  schedule    jsonb NOT NULL DEFAULT '[]',  -- [{"dow":1,"start":"16:00","end":"17:30"}]
  default_tariff_id uuid REFERENCES tariffs(id),
  archived    boolean NOT NULL DEFAULT false
);

CREATE TABLE enrollments (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  center_id    uuid NOT NULL REFERENCES centers(id),
  student_id   uuid NOT NULL REFERENCES students(id),
  group_id     uuid NOT NULL REFERENCES groups(id),
  tariff_id    uuid NOT NULL REFERENCES tariffs(id),
  start_date   date NOT NULL,
  end_date     date,
  lessons_left int
);
CREATE INDEX ON enrollments (group_id) WHERE end_date IS NULL;

CREATE TABLE lessons (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  center_id  uuid NOT NULL REFERENCES centers(id),
  group_id   uuid NOT NULL REFERENCES groups(id),
  date       date NOT NULL,
  start_time time NOT NULL,
  cancelled  boolean NOT NULL DEFAULT false,
  UNIQUE (group_id, date, start_time)
);

CREATE TABLE attendance (
  lesson_id  uuid NOT NULL REFERENCES lessons(id),
  student_id uuid NOT NULL REFERENCES students(id),
  status     text NOT NULL CHECK (status IN ('present','absent','excused')),
  marked_by  uuid NOT NULL REFERENCES users(id),
  marked_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (lesson_id, student_id)
);

CREATE TABLE ledger (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  center_id     uuid NOT NULL REFERENCES centers(id),
  student_id    uuid NOT NULL REFERENCES students(id),
  enrollment_id uuid REFERENCES enrollments(id),
  kind          text NOT NULL CHECK (kind IN ('charge','payment','refund','correction')),
  amount        bigint NOT NULL,       -- charge/refund < 0, payment > 0
  period        date,                  -- 1-е число месяца для monthly
  due_date      date,
  list_price    bigint,
  discount      bigint,
  method        text CHECK (method IN ('cash','transfer','card','finik')),
  external_id   text,                  -- id платежа Finik
  comment       text,
  created_by    uuid REFERENCES users(id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  cancelled_at  timestamptz,
  cancelled_by  uuid REFERENCES users(id),
  cancel_reason text
);
CREATE INDEX ON ledger (center_id, student_id) WHERE cancelled_at IS NULL;
CREATE UNIQUE INDEX ON ledger (enrollment_id, period) WHERE kind = 'charge' AND period IS NOT NULL AND cancelled_at IS NULL;
CREATE UNIQUE INDEX ON ledger (center_id, external_id) WHERE external_id IS NOT NULL;

CREATE TABLE payment_links (
  token      text PRIMARY KEY,          -- 32 случайных байта, base64url
  center_id  uuid NOT NULL REFERENCES centers(id),
  parent_id  uuid NOT NULL REFERENCES parents(id),
  amount     bigint NOT NULL,
  finik_id   text,
  status     text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','paid','expired')),
  expires_at timestamptz NOT NULL
);

CREATE TABLE invites (
  token      text PRIMARY KEY,
  center_id  uuid NOT NULL REFERENCES centers(id),
  kind       text NOT NULL CHECK (kind IN ('staff','parent')),
  role       text,                      -- для staff
  parent_id  uuid REFERENCES parents(id),
  expires_at timestamptz NOT NULL,
  used_at    timestamptz
);

CREATE TABLE notifications (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  center_id  uuid NOT NULL REFERENCES centers(id),
  parent_id  uuid NOT NULL REFERENCES parents(id),
  kind       text NOT NULL,             -- due_soon, due_today, overdue_3, overdue_7, manual, package_low, absent, paid
  status     text NOT NULL CHECK (status IN ('sent','failed','blocked')),
  error      text,
  sent_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON notifications (parent_id, sent_at DESC);

CREATE TABLE audit_log (
  id         bigserial PRIMARY KEY,
  center_id  uuid NOT NULL REFERENCES centers(id),
  user_id    uuid REFERENCES users(id),
  action     text NOT NULL,             -- attendance.mark, ledger.payment, ledger.cancel, student.update ...
  entity     text NOT NULL,
  entity_id  uuid,
  before     jsonb,
  after      jsonb,
  at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON audit_log (center_id, at DESC);
```

## API

REST + JSON, префикс `/api/v1`. Сессия — httpOnly cookie (таблица сессий или подписанный токен), все запросы к данным центра — под `/c/{centerId}`, middleware проверяет членство и роль. Ошибки: `{"error":{"code":"...","message":"..."}}`; списки — с `limit`/`cursor`.

| Метод и путь | Роли | Что делает |
| --- | --- | --- |
| `POST /auth/register` | — | создать пользователя + центр, роль owner |
| `POST /auth/login`, `POST /auth/logout`, `GET /me` | — | вход, выход, профиль и список центров |
| `POST /invites/{token}/accept` | — | принять приглашение сотрудника |
| `GET/PATCH /c/{id}/settings` | owner | настройки центра (чтение — admin тоже) |
| `GET/POST /c/{id}/staff`, `DELETE /c/{id}/staff/{userId}` | owner | сотрудники, создание приглашения |
| `GET/POST/PATCH /c/{id}/tariffs` | owner, admin | тарифы (удаление = archived) |
| `GET/POST/PATCH /c/{id}/groups` | owner, admin | группы; teacher читает только свои |
| `GET /c/{id}/students?q=&group=&status=&tg=` | owner, admin | список с балансом |
| `POST /c/{id}/students`, `GET/PATCH /c/{id}/students/{sid}` | owner, admin | создать / карточка / изменить (вкл. родителя) |
| `POST /c/{id}/students/{sid}/enrollments`, `PATCH .../enrollments/{eid}` | owner, admin | записать в группу (с выбором R5), завершить, продлить абонемент |
| `POST /c/{id}/import/preview`, `POST /c/{id}/import/commit` | owner, admin | импорт xlsx/csv: разбор с ошибками → сохранение |
| `GET /c/{id}/lessons?date=` | все | занятия на дату (создаёт по расписанию, R11) |
| `POST /c/{id}/lessons`, `PATCH /c/{id}/lessons/{lid}` | owner, admin | внеплановое занятие, отмена |
| `GET/PUT /c/{id}/lessons/{lid}/attendance` | все (R12) | список и пакетное сохранение отметок |
| `GET /c/{id}/students/{sid}/ledger` | owner, admin | история операций |
| `POST /c/{id}/students/{sid}/payments` | owner, admin | ручная оплата / возврат / поправка; заголовок `Idempotency-Key` |
| `POST /c/{id}/ledger/{lid}/cancel` | owner, admin (R15) | отмена с причиной |
| `GET /c/{id}/debtors?group=` | owner, admin | должники по R14 + итоги месяца |
| `POST /c/{id}/reminders` | owner, admin | `{"student_ids":[...]}` или `{"all":true}`; лимиты R17 |
| `POST /c/{id}/parents/{pid}/invite` | owner, admin | ссылка на бота + текст для WhatsApp |
| `GET /c/{id}/reports/month?m=2026-10`, `GET .../export.xlsx` | owner | отчёт и выгрузка |
| `GET /c/{id}/audit?user=&action=` | owner | журнал |
| `GET /pay/{token}`, `POST /pay/{token}/start` | — | страница оплаты родителя, создание платежа Finik |
| `POST /webhooks/finik` | — | проверка подписи, идемпотентно по `external_id` |
| `POST /webhooks/telegram/{secret}` | — | апдейты бота, секрет в пути + заголовок `X-Telegram-Bot-Api-Secret-Token` |

Бот: команды `/start <token>` (привязка), `/balance`, `/lang`, `/stop`, `/help`; inline-кнопки «Оплатить» (ссылка на `/pay/{token}`) и «Баланс и посещения».

## Фоновые задачи и структура проекта

Один Go-бинарник: HTTP API, вебхуки и планировщик в одном процессе; фронтенд — отдельная сборка, отдаётся статикой через Caddy.

**Фоновые задачи** (каждая идемпотентна, запуск под `pg_try_advisory_lock`, чтобы не выполниться дважды):

| Задача | Когда | Что делает |
| --- | --- | --- |
| `billing.monthly` | каждый час | для центров, у которых наступил `billing_day` (в их часовом поясе), создаёт начисления по R3–R7 |
| `reminders.auto` | каждый час, 9:00–20:00 | ищет родителей по расписанию напоминаний, шлёт с учётом R17–R19 |
| `payments.reconcile` | каждые 15 минут | перепроверяет платежи Finik со статусом pending (если вебхук потерялся), истёкшие ссылки → expired |
| `ops.backup` | ежедневно 03:00 | `pg_dump` → внешнее хранилище (cron на сервере, не в приложении) |

**Структура репозитория**

```text
okuu/
  backend/
    cmd/server/main.go          # запуск: config, DB, router, scheduler
    internal/
      config/                   # env-переменные
      db/                       # pgx pool, sqlc-сгенерированные запросы
      auth/                     # сессии, хэши паролей, middleware ролей
      billing/                  # начисления, баланс, FIFO-долги (R3–R16)
      attendance/               # занятия, отметки, абонементы
      students/  groups/  tariffs/  staff/
      importer/                 # xlsx/csv (excelize)
      notify/                   # тексты ru/ky, очередь, лимиты
      telegram/                 # вебхук, команды, клавиатуры
      payments/finik/           # клиент Finik за интерфейсом PaymentProvider
      jobs/                     # планировщик и задачи
      audit/
      httpapi/                  # роутер (chi), хендлеры, DTO
    migrations/
    queries/                    # SQL для sqlc
  frontend/                     # React + Vite + TypeScript, TanStack Query
    src/pages/  src/components/  src/api/  src/i18n/
  deploy/
    docker-compose.yml  Caddyfile  .env.example
  Makefile                      # dev, test, migrate, build
```

Библиотеки: chi, pgx/v5, sqlc, goose, excelize, go-telegram-bot-api (или свой тонкий клиент), golang.org/x/crypto/argon2. Finik спрятан за интерфейсом `PaymentProvider` — пока нет ключей, работает заглушка «реквизиты».

## Нефункциональные требования

Стек — то, что мы знаем, инфраструктура — один VPS за \~$10 в месяц.

- **Бэкенд:** Go, REST API, PostgreSQL, фоновые задачи (начисления, напоминания) по расписанию.
- **Фронтенд:** React + Vite + TypeScript, mobile-first; интерфейс на русском, строки вынесены для перевода на кыргызский.
- **Бот:** Telegram Bot API через вебхук, в том же Go-сервисе.
- **Развёртывание:** Docker Compose, HTTPS (Caddy или nginx + Let's Encrypt).
- **Безопасность:** данные одного центра недоступны другому (проверка `center_id` в каждом запросе + тесты), пароли — bcrypt/argon2, ключи Finik зашифрованы, права по ролям проверяются на сервере.
- **Бэкапы:** `pg_dump` каждую ночь на другое хранилище, храним 30 дней; восстановление проверяем до запуска пилота.
- **Персональные данные:** храним данные детей и родителей, поэтому нужны договор-оферта с центром и согласие родителя при подключении к боту. Требования закона КР «Об информации персонального характера» — в открытых вопросах.
- **Производительность:** страницы открываются за <1 с на мобильном интернете для центра на 500 учеников.
- **Мониторинг:** логи ошибок + алерт в наш Telegram, если не отработали начисления или напоминания.

## Тесты, деплой и готовность

MVP готов, когда пилотный центр прожил месяц без Excel: начисления прошли сами, родители получили напоминания, долги сходятся с реальностью.

**Тесты**

- Юнит-тесты на `billing` и `attendance` — по одному на каждое правило R3–R16 (табличные тесты Go): частичная оплата, переплата, отмена, FIFO-долг, пауза, абонемент в минус, смена отметки.
- Интеграционные тесты на настоящем PostgreSQL (testcontainers или docker compose): повторный запуск `billing.monthly` не дублирует, повторный вебхук Finik не дублирует оплату.
- **Тест изоляции:** для каждого эндпоинта `/c/{id}/...` пользователь центра A получает 404 на данные центра B, преподаватель — 403 на деньги.
- Ручной чек-лист перед каждым релизом: импорт тестового Excel, отметка с телефона, напоминание в тестовый Telegram, оплата и её отмена.

**Окружения и деплой**

- `dev` — локально `docker compose up` (Postgres + backend + Vite), тестовый бот через long polling.
- `prod` — один VPS (2 vCPU, 2–4 ГБ), Docker Compose: `caddy` (HTTPS), `app`, `postgres` с томом; деплой — GitHub Actions: тесты → образ → `ssh` + `docker compose pull && up -d`; миграции при старте.
- Секреты только в `.env` на сервере и в GitHub Secrets: `DATABASE_URL`, `SESSION_KEY`, `ENCRYPTION_KEY`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEBHOOK_SECRET`, `FINIK_*`. В репозиторий — только `.env.example`.

**Чек-лист готовности к пилоту**

- [ ] F1–F8 выполнены по критериям выше
- [ ] Тесты R3–R16 и тест изоляции зелёные
- [ ] Бэкап сделан и восстановлен на чистую БД
- [ ] HTTPS, алерты ошибок в наш Telegram
- [ ] Оферта для центра и текст согласия родителя в боте
- [ ] Тексты бота на кыргызском проверены носителем языка
- [ ] Данные первого центра перенесены и сверены с их Excel

## Вне MVP

Делаем только после 5–10 платящих центров и в порядке, который покажут клиенты: зарплата преподавателей, расписание с кабинетами и заменами, заявки и пробные уроки, отток («не ходил 2 недели»), филиалы, WhatsApp, садики (питание, больничные, фото-отчёт), наш собственный биллинг.

## Открытые вопросы

- [ ] Finik: есть ли API для веба (ссылка на оплату + вебхук), какая комиссия, можно ли нам работать с ключами каждого центра.
- [ ] Альтернативы Finik: QR MBank, O!Деньги, Элсом — есть ли API для бизнеса.
- [ ] Персональные данные: что требует закон КР (согласие, регистрация держателя, где должен стоять сервер).
- [ ] Кто платит нам и как: ИП, счёт, перевод — решить до первого платного клиента.
- [ ] Правила центров: переносятся ли пропуски, перерасчёт при болезни, оплата вперёд или по факту — уточнить в интервью.
- [ ] Telegram или WhatsApp: что реально читают родители — уточнить в интервью.

## План по неделям

1. **Неделя 0 (сейчас):** 10–20 интервью, кликабельный макет, запрос в Finik, правки ТЗ.
2. **Неделя 1:** база, авторизация, роли, центры, ученики, группы, тарифы, импорт Excel (F1–F4).
3. **Неделя 2:** посещаемость, ledger, начисления, должники (F5–F6).
4. **Неделя 3:** Telegram-бот: привязка, баланс, напоминания; реквизиты вместо Finik.
5. **Неделя 4:** отчёт, журнал, бэкапы, деплой; запуск 3–5 пилотных центров. Finik подключаем, когда ответят.
