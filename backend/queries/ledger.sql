-- ledger: операции не удаляются и не редактируются (R15). Знак: charge/refund < 0, payment > 0.

-- name: InsertCharge :one
INSERT INTO ledger (center_id, student_id, enrollment_id, kind, amount, period, due_date,
                    list_price, discount, comment, created_by)
VALUES (@center_id, @student_id, @enrollment_id, 'charge', @amount, @period, @due_date,
        @list_price, @discount, @comment, @created_by)
RETURNING *;

-- name: StudentBalance :one
SELECT COALESCE(sum(amount), 0)::bigint
FROM ledger
WHERE center_id = @center_id AND student_id = @student_id AND cancelled_at IS NULL;

-- name: InsertOperation :one
-- Ручная операция (payment / refund / correction). Тот же Idempotency-Key в центре —
-- строки нет в ответе: возьмите существующую через GetOperationByKey.
INSERT INTO ledger (center_id, student_id, kind, amount, method, comment, created_by, idempotency_key)
VALUES (@center_id, @student_id, @kind, @amount, @method, @comment, @created_by, @idempotency_key)
ON CONFLICT (center_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
RETURNING *;

-- name: GetOperationByKey :one
SELECT * FROM ledger
WHERE center_id = @center_id AND idempotency_key = @idempotency_key;

-- name: GetLedgerForUpdate :one
SELECT * FROM ledger
WHERE center_id = @center_id AND id = @id
FOR UPDATE;

-- name: CancelOperation :one
-- R15: операции не удаляются и не редактируются — только отмена с причиной.
UPDATE ledger
SET cancelled_at = now(), cancelled_by = @cancelled_by, cancel_reason = @cancel_reason
WHERE center_id = @center_id AND id = @id AND cancelled_at IS NULL
RETURNING *;

-- name: ListStudentLedger :many
-- История ученика: новые сверху, с отменёнными. after_* — последняя строка предыдущей страницы.
SELECT * FROM ledger
WHERE center_id = @center_id AND student_id = @student_id
  AND (sqlc.narg(after_created_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: ListDebtCandidates :many
-- Ученики с отрицательным балансом — кандидаты в должники (R14 решает код по срокам).
-- group_id: была запись в эту группу (в том числе завершённая — долг ушедших тоже виден, R7).
SELECT s.id, s.full_name, s.status,
       p.name AS parent_name, p.phone AS parent_phone, p.tg_status AS parent_tg_status,
       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', g.id, 'name', g.name) ORDER BY g.name, g.id)
                 FROM enrollments e JOIN groups g ON g.id = e.group_id
                 WHERE e.student_id = s.id AND e.end_date IS NULL), '[]'::jsonb)::jsonb AS groups,
       b.balance
FROM students s
LEFT JOIN parents p ON p.id = s.parent_id
CROSS JOIN LATERAL (
  SELECT COALESCE(sum(l.amount), 0)::bigint AS balance FROM ledger l
  WHERE l.center_id = s.center_id AND l.student_id = s.id AND l.cancelled_at IS NULL
) b
WHERE s.center_id = @center_id AND b.balance < 0
  AND (sqlc.narg(group_id)::uuid IS NULL OR EXISTS (
        SELECT 1 FROM enrollments e WHERE e.student_id = s.id AND e.group_id = sqlc.narg(group_id)::uuid));

-- name: ListActiveOperations :many
-- Неотменённые операции учеников для расчёта долга по FIFO (R14).
SELECT student_id, id, kind, amount, due_date, created_at
FROM ledger
WHERE center_id = @center_id AND student_id = ANY(@student_ids::uuid[]) AND cancelled_at IS NULL;

-- name: MonthSummary :one
-- Итоги месяца P в часовом поясе центра:
-- charged — начисления за период P и абонементы (period NULL), созданные в этом месяце;
-- collected — оплаты минус возвраты, созданные в этом месяце. Отменённые не считаются.
SELECT
  COALESCE(-sum(amount) FILTER (WHERE kind = 'charge' AND (period = @period::date OR (period IS NULL AND
      created_at >= (@period::date)::timestamp AT TIME ZONE @tz::text AND
      created_at < (@next_period::date)::timestamp AT TIME ZONE @tz::text))), 0)::bigint AS charged,
  COALESCE(sum(amount) FILTER (WHERE kind IN ('payment', 'refund') AND
      created_at >= (@period::date)::timestamp AT TIME ZONE @tz::text AND
      created_at < (@next_period::date)::timestamp AT TIME ZONE @tz::text), 0)::bigint AS collected
FROM ledger
WHERE center_id = @center_id AND cancelled_at IS NULL;

-- name: StudentExists :one
SELECT EXISTS (SELECT 1 FROM students WHERE center_id = @center_id AND id = @id);

-- --- billing.monthly (R3, R4, R6, R7) ---

-- name: ListCentersForBilling :many
SELECT id, timezone, billing_day FROM centers ORDER BY id;

-- name: ListMonthlyBillingCandidates :many
-- Месячные записи, по которым решение за период P ещё не принято. Ушедшим не начисляем (R7).
SELECT e.id
FROM enrollments e
JOIN tariffs t ON t.id = e.tariff_id
JOIN students s ON s.id = e.student_id
WHERE e.center_id = @center_id AND t.kind = 'monthly' AND s.status <> 'left'
  AND e.start_date <= @period_end::date
  AND (e.end_date IS NULL OR e.end_date >= @period::date)
  AND (e.billed_through IS NULL OR e.billed_through < @period::date)
ORDER BY e.id;

-- name: LockEnrollmentForBilling :one
-- Перепроверка под блокировкой: параллельный запуск уже мог принять решение за P.
SELECT e.id, e.student_id, e.tariff_id, s.status, s.discount_kind, s.discount_value
FROM enrollments e
JOIN students s ON s.id = e.student_id
WHERE e.center_id = @center_id AND e.id = @id
  AND (e.billed_through IS NULL OR e.billed_through < @period::date)
FOR UPDATE OF e;

-- name: HasChargeForPeriod :one
SELECT EXISTS (
  SELECT 1 FROM ledger
  WHERE enrollment_id = @enrollment_id AND kind = 'charge' AND period = @period::date AND cancelled_at IS NULL
);

-- name: SetBilledThrough :exec
UPDATE enrollments SET billed_through = @period::date
WHERE center_id = @center_id AND id = @id;
