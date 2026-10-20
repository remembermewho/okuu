-- Напоминания родителям (R17–R19).
-- centers.notify_absent — сообщать родителю «ребёнок не пришёл» (по умолчанию выключено).
-- notifications.dedup_key — ключ события (overdue_3:{parent}:{due_date}, paid:{ledger_id}, ...):
--   повторный запуск задачи не шлёт то же сообщение второй раз. У неудачных отправок ключ снимается,
--   чтобы следующий запуск повторил попытку.
-- notifications.student_ids — дети, о которых сообщение (лимит ручных напоминаний — по ученику).
-- notification_outbox — событийные сообщения (paid, package_low, absent), записанные в той же
--   транзакции, что и само событие; отправляет их диспетчер, ночью — утром.

-- +goose Up
ALTER TABLE centers ADD COLUMN notify_absent boolean NOT NULL DEFAULT false;

ALTER TABLE notifications
  ADD COLUMN dedup_key   text UNIQUE,
  ADD COLUMN student_ids uuid[] NOT NULL DEFAULT '{}',
  ADD COLUMN created_by  uuid REFERENCES users(id);  -- ручное напоминание: кто нажал
CREATE INDEX notifications_center_sent ON notifications (center_id, sent_at DESC);

CREATE TABLE notification_outbox (
  id         bigserial PRIMARY KEY,
  center_id  uuid NOT NULL REFERENCES centers(id),
  kind       text NOT NULL CHECK (kind IN ('paid', 'package_low', 'absent')),
  dedup_key  text NOT NULL UNIQUE,
  student_id uuid NOT NULL REFERENCES students(id),
  ref_id     uuid NOT NULL,              -- ledger.id | enrollment.id | lesson.id
  attempts   int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE notification_outbox;
DROP INDEX notifications_center_sent;
ALTER TABLE notifications DROP COLUMN created_by, DROP COLUMN student_ids, DROP COLUMN dedup_key;
ALTER TABLE centers DROP COLUMN notify_absent;
