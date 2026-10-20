-- attendance.deducted — списала ли эта отметка занятие с абонемента (R9). Фиксируется в момент
-- отметки: смена настройки центра excused_deducts не пересчитывает историю. Смена отметки
-- пересчитывает остаток по разнице deducted.

-- +goose Up
ALTER TABLE attendance ADD COLUMN deducted boolean NOT NULL DEFAULT false;
CREATE INDEX ON lessons (center_id, date);
CREATE INDEX ON attendance (student_id);

-- +goose Down
DROP INDEX attendance_student_id_idx;
DROP INDEX lessons_center_id_date_idx;
ALTER TABLE attendance DROP COLUMN deducted;
