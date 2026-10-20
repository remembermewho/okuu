-- Записи в группы: не больше одной активной записи ученика в группу (гарантия БД
-- поверх проверки в коде) и быстрый доступ к записям ученика для карточки.

-- +goose Up
CREATE UNIQUE INDEX enrollments_one_active_per_group
  ON enrollments (student_id, group_id) WHERE end_date IS NULL;
CREATE INDEX ON enrollments (student_id);

-- +goose Down
DROP INDEX enrollments_student_id_idx;
DROP INDEX enrollments_one_active_per_group;
