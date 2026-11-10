-- WhatsApp-напоминания по ссылке wa.me и рассылки центра.
-- notifications.channel — как сообщение дошло до родителя: telegram (бот) или whatsapp_link (сотрудник открыл
--   ссылку wa.me и отправляет со своего WhatsApp). status = link_opened — ссылка открыта; это не гарантия доставки.
-- parents.last_whatsapp_at — когда последний раз открыли WhatsApp-напоминание (отметка «открыто в WhatsApp»);
--   last_reminder_at остаётся про Telegram: то, что точно ушло.
-- broadcasts — объявления центра; broadcast_recipients — по строке на родителя:
--   queued → sent (бот); whatsapp_pending → whatsapp_opened (ссылка); no_contact — написать некуда.
--   Родитель, которому бот не смог отправить (заблокировал, /stop, ошибка Telegram), переходит в whatsapp_pending.

-- +goose Up
ALTER TABLE notifications
  ADD COLUMN channel text NOT NULL DEFAULT 'telegram' CHECK (channel IN ('telegram', 'whatsapp_link')),
  DROP CONSTRAINT notifications_status_check,
  ADD CONSTRAINT notifications_status_check CHECK (status IN ('sent', 'failed', 'blocked', 'link_opened'));

ALTER TABLE parents ADD COLUMN last_whatsapp_at timestamptz;

CREATE TABLE broadcasts (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  center_id          uuid NOT NULL REFERENCES centers(id),
  created_by         uuid NOT NULL REFERENCES users(id),
  idempotency_key    text NOT NULL,
  audience           text NOT NULL CHECK (audience IN ('center', 'groups')),
  group_ids          uuid[] NOT NULL DEFAULT '{}',
  group_names        text[] NOT NULL DEFAULT '{}',  -- как группы назывались в момент рассылки
  body               text NOT NULL,
  no_parent_students int NOT NULL DEFAULT 0,        -- ученики без родителя: написать некому
  created_at         timestamptz NOT NULL,
  UNIQUE (center_id, idempotency_key)
);
CREATE INDEX broadcasts_center_created ON broadcasts (center_id, created_at DESC, id DESC);

CREATE TABLE broadcast_recipients (
  broadcast_id  uuid NOT NULL REFERENCES broadcasts(id) ON DELETE CASCADE,
  center_id     uuid NOT NULL REFERENCES centers(id),
  parent_id     uuid NOT NULL REFERENCES parents(id),
  status        text NOT NULL CHECK (status IN ('queued', 'sent', 'whatsapp_pending', 'whatsapp_opened', 'no_contact')),
  student_ids   uuid[] NOT NULL DEFAULT '{}',
  student_names text[] NOT NULL DEFAULT '{}',  -- как учеников звали в момент рассылки
  opened_at     timestamptz,
  PRIMARY KEY (broadcast_id, parent_id)
);
CREATE INDEX broadcast_recipients_queued ON broadcast_recipients (broadcast_id) WHERE status = 'queued';

-- +goose Down
DROP TABLE broadcast_recipients;
DROP TABLE broadcasts;
ALTER TABLE parents DROP COLUMN last_whatsapp_at;
DELETE FROM notifications WHERE channel = 'whatsapp_link';
ALTER TABLE notifications
  DROP CONSTRAINT notifications_status_check,
  ADD CONSTRAINT notifications_status_check CHECK (status IN ('sent', 'failed', 'blocked')),
  DROP COLUMN channel;
