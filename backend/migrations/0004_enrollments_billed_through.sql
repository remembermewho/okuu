-- enrollments.billed_through — последний период (1-е число месяца), по которому решение о начислении
-- уже принято (R5 при записи или billing.monthly). billing.monthly начисляет период P, только если
-- billed_through IS NULL или < P, и в той же транзакции ставит billed_through = P.
-- Для абонементов не используется (NULL).

-- +goose Up
ALTER TABLE enrollments
  ADD COLUMN billed_through date
  CHECK (billed_through IS NULL OR billed_through = date_trunc('month', billed_through)::date);

-- Месячные записи, созданные до этой миграции, уже прошли выбор R5 за свой текущий месяц
-- (будущие — нет): иначе billing.monthly начислил бы им этот месяц ещё раз.
UPDATE enrollments e
SET billed_through = date_trunc('month', now() AT TIME ZONE c.timezone)::date
FROM tariffs t, centers c
WHERE t.id = e.tariff_id AND c.id = e.center_id AND t.kind = 'monthly'
  AND e.start_date < (date_trunc('month', now() AT TIME ZONE c.timezone) + interval '1 month')::date;

-- +goose Down
ALTER TABLE enrollments DROP COLUMN billed_through;
