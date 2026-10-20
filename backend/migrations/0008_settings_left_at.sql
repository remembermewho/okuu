-- Настройки центра и отчёт (S7, S8).
-- centers.billing_day_next / billing_day_next_from — новый день начисления действует со следующего
-- периода: до billing_day_next_from (1-е число месяца) работает старый billing_day.
-- students.left_at — когда ученик перешёл в «ушёл» (отчёт «ушло за месяц»); снимается при возврате.

-- +goose Up
ALTER TABLE centers
  ADD COLUMN billing_day_next smallint CHECK (billing_day_next BETWEEN 1 AND 28),
  ADD COLUMN billing_day_next_from date,
  ADD CONSTRAINT centers_billing_day_next_pair CHECK ((billing_day_next IS NULL) = (billing_day_next_from IS NULL));

ALTER TABLE students ADD COLUMN left_at timestamptz;
-- Уже ушедшие: момент ухода — из журнала (последнее изменение статуса на left).
UPDATE students s SET left_at = (
  SELECT max(a.at) FROM audit_log a
  WHERE a.entity = 'student' AND a.entity_id = s.id AND a.after->>'status' = 'left'
)
WHERE s.status = 'left';

-- +goose Down
ALTER TABLE students DROP COLUMN left_at;
ALTER TABLE centers
  DROP CONSTRAINT centers_billing_day_next_pair,
  DROP COLUMN billing_day_next_from,
  DROP COLUMN billing_day_next;
