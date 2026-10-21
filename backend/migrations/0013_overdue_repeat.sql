-- Повтор напоминаний о просроченном долге (настройка центра): weekly — после 7-го дня просрочки, пока долг
-- не оплачен, ещё раз в 7 дней (14, 21, 28… день); off — после 7-го дня только вручную, как раньше.

-- +goose Up
ALTER TABLE centers
  ADD COLUMN overdue_repeat text NOT NULL DEFAULT 'weekly' CHECK (overdue_repeat IN ('weekly', 'off'));

-- +goose Down
ALTER TABLE centers DROP COLUMN overdue_repeat;
