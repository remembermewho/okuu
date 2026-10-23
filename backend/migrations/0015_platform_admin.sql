-- Подключение центров и панель суперадмина: статус центра, тариф сервиса и оплата, ссылки регистрации,
-- права суперадмина (только командой server admin grant), журнал действий платформы.

-- +goose Up
ALTER TABLE centers
  ADD COLUMN status         text NOT NULL DEFAULT 'active' CHECK (status IN ('pending', 'active', 'blocked')),
  ADD COLUMN blocked_reason text,
  ADD COLUMN paid_until     date;

-- Тариф сервиса: free | start | center | network | founder. Пилотные центры — «founder» (первые клиенты).
ALTER TABLE centers ALTER COLUMN plan SET DEFAULT 'free';
UPDATE centers SET plan = 'founder' WHERE plan NOT IN ('free', 'start', 'center', 'network', 'founder');
ALTER TABLE centers ADD CONSTRAINT centers_plan_check CHECK (plan IN ('free', 'start', 'center', 'network', 'founder'));

ALTER TABLE users
  ADD COLUMN is_platform_admin boolean NOT NULL DEFAULT false,
  ADD COLUMN last_login_at     timestamptz;
UPDATE users u SET last_login_at = (SELECT max(s.created_at) FROM sessions s WHERE s.user_id = u.id);

-- Одноразовые ссылки регистрации центра ({APP_ORIGIN}/register/{токен}); в БД — hex(sha256) токена.
CREATE TABLE registration_links (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  token      text NOT NULL UNIQUE,
  note       text NOT NULL,                       -- для кого (видно только суперадмину)
  created_by uuid REFERENCES users(id) ON DELETE SET NULL, -- NULL — консоль (server admin registration-link)
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  used_at    timestamptz,
  center_id  uuid REFERENCES centers(id) ON DELETE SET NULL, -- центр, созданный по ссылке
  revoked_at timestamptz
);
CREATE INDEX ON registration_links (created_at DESC);

-- Журнал действий суперадмина и консольных команд — отдельно от журналов центров.
CREATE TABLE platform_audit_log (
  id        bigserial PRIMARY KEY,
  admin_id  uuid REFERENCES users(id) ON DELETE SET NULL, -- NULL — консоль
  action    text NOT NULL,                                -- center.approve, center.block, admin.grant …
  center_id uuid REFERENCES centers(id) ON DELETE SET NULL,
  details   jsonb,
  at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON platform_audit_log (at DESC);
CREATE INDEX ON platform_audit_log (center_id, at DESC);

-- +goose Down
DROP TABLE platform_audit_log;
DROP TABLE registration_links;
ALTER TABLE users DROP COLUMN last_login_at, DROP COLUMN is_platform_admin;
ALTER TABLE centers DROP CONSTRAINT centers_plan_check;
UPDATE centers SET plan = 'pilot';
ALTER TABLE centers ALTER COLUMN plan SET DEFAULT 'pilot';
ALTER TABLE centers DROP COLUMN paid_until, DROP COLUMN blocked_reason, DROP COLUMN status;
