-- Схема из раздела «Схема БД» ТЗ (docs/TZ.md) + таблица sessions для входа сотрудников.

-- +goose Up
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

-- Сессии сотрудников. Токен (32 случайных байта) хранится только в httpOnly cookie,
-- в БД — sha256 от него. Срок 30 дней, продление при активности не чаще раза в сутки.
CREATE TABLE sessions (
  token_hash   bytea PRIMARY KEY,
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,
  last_seen_at timestamptz,
  user_agent   text
);
CREATE INDEX ON sessions (user_id);
CREATE INDEX ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
DROP TABLE audit_log;
DROP TABLE notifications;
DROP TABLE invites;
DROP TABLE payment_links;
DROP TABLE ledger;
DROP TABLE attendance;
DROP TABLE lessons;
DROP TABLE enrollments;
DROP TABLE groups;
DROP TABLE tariffs;
DROP TABLE students;
DROP TABLE parents;
DROP TABLE memberships;
DROP TABLE users;
DROP TABLE centers;
