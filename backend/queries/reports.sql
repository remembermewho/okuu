-- Отчёт за месяц (F7). Границы месяца — моменты [@from, @to) в часовом поясе центра (считает код),
-- @period — 1-е число месяца, @period_end — последний день месяца.

-- name: ReportMoney :one
-- charged — начисления за период и абонементы (period NULL), созданные в месяце;
-- оплаты по способам, возвраты и поправки — созданные в месяце. Отменённые не считаются.
SELECT
  COALESCE(-sum(amount) FILTER (WHERE kind = 'charge' AND (period = @period::date
      OR (period IS NULL AND created_at >= @from_at::timestamptz AND created_at < @to_at::timestamptz))), 0)::bigint AS charged,
  COALESCE(sum(amount) FILTER (WHERE kind = 'payment' AND method = 'cash' AND created_at >= @from_at::timestamptz AND created_at < @to_at::timestamptz), 0)::bigint AS paid_cash,
  COALESCE(sum(amount) FILTER (WHERE kind = 'payment' AND method = 'transfer' AND created_at >= @from_at::timestamptz AND created_at < @to_at::timestamptz), 0)::bigint AS paid_transfer,
  COALESCE(sum(amount) FILTER (WHERE kind = 'payment' AND method = 'card' AND created_at >= @from_at::timestamptz AND created_at < @to_at::timestamptz), 0)::bigint AS paid_card,
  COALESCE(sum(amount) FILTER (WHERE kind = 'payment' AND method = 'finik' AND created_at >= @from_at::timestamptz AND created_at < @to_at::timestamptz), 0)::bigint AS paid_finik,
  COALESCE(-sum(amount) FILTER (WHERE kind = 'refund' AND created_at >= @from_at::timestamptz AND created_at < @to_at::timestamptz), 0)::bigint AS refunds,
  COALESCE(sum(amount) FILTER (WHERE kind = 'correction' AND created_at >= @from_at::timestamptz AND created_at < @to_at::timestamptz), 0)::bigint AS corrections
FROM ledger
WHERE center_id = @center_id AND cancelled_at IS NULL;

-- name: ReportDebtorsAt :many
-- Отрицательные балансы на момент @to_at: операции, созданные до него, без отменённых
-- (статус отмены — текущий: отменённую позже операцию считаем отменённой и в прошлом).
-- Списания — только за этот месяц и раньше: начисление с периодом — period ≤ @period_end, без периода
-- (абонемент, поправка, возврат) — срок (due_date) ≤ @period_end; начисления за следующие месяцы,
-- сделанные заранее, в долг на конец месяца не входят.
SELECT s.id, s.full_name, s.status, p.name AS parent_name, p.phone AS parent_phone, b.balance
FROM students s
LEFT JOIN parents p ON p.id = s.parent_id
CROSS JOIN LATERAL (
  SELECT COALESCE(sum(l.amount), 0)::bigint AS balance FROM ledger l
  WHERE l.center_id = s.center_id AND l.student_id = s.id AND l.cancelled_at IS NULL AND l.created_at < @to_at::timestamptz
    AND (l.amount >= 0
      OR (l.period IS NOT NULL AND l.period <= @period_end::date)
      OR (l.period IS NULL AND (l.due_date IS NULL OR l.due_date <= @period_end::date)))
) b
WHERE s.center_id = @center_id AND b.balance < 0
ORDER BY b.balance, s.full_name, s.id;

-- name: ReportStudents :one
-- active_end — ученики с записью, активной в последний день месяца;
-- joined — первая запись ученика началась в этом месяце; left — ушли (left_at) в этом месяце.
SELECT
  (SELECT count(DISTINCT e.student_id) FROM enrollments e
   WHERE e.center_id = @center_id AND e.start_date <= @period_end::date
     AND (e.end_date IS NULL OR e.end_date >= @period_end::date))::bigint AS active_end,
  (SELECT count(*) FROM (
     SELECT e.student_id FROM enrollments e WHERE e.center_id = @center_id
     GROUP BY e.student_id HAVING min(e.start_date) BETWEEN @period::date AND @period_end::date) j)::bigint AS joined,
  (SELECT count(*) FROM students s
   WHERE s.center_id = @center_id AND s.left_at >= @from_at::timestamptz AND s.left_at < @to_at::timestamptz)::bigint AS left_count;

-- name: ReportOperations :many
-- Все операции месяца (включая отменённые — с пометкой) для выгрузки.
SELECT l.id, l.created_at, l.kind, l.amount, l.method, l.comment, l.period, l.due_date,
       l.cancelled_at, l.cancel_reason, s.full_name AS student_name, u.name AS created_by_name
FROM ledger l
JOIN students s ON s.id = l.student_id
LEFT JOIN users u ON u.id = l.created_by
WHERE l.center_id = @center_id AND l.created_at >= @from_at::timestamptz AND l.created_at < @to_at::timestamptz
ORDER BY l.created_at, l.id;

-- name: ListAudit :many
-- Журнал (F8): новые сверху. action_prefix — 'ledger.' и т.п. (с точкой на конце) или точное действие.
SELECT a.id, a.at, a.user_id, u.name AS user_name, a.action, a.entity, a.entity_id, a.before, a.after
FROM audit_log a
LEFT JOIN users u ON u.id = a.user_id
WHERE a.center_id = @center_id
  AND (sqlc.narg(user_id)::uuid IS NULL OR a.user_id = sqlc.narg(user_id)::uuid)
  AND (sqlc.narg(action)::text IS NULL OR a.action = sqlc.narg(action)::text)
  AND (sqlc.narg(action_prefix)::text IS NULL OR starts_with(a.action, sqlc.narg(action_prefix)::text))
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR a.at >= sqlc.narg(from_at)::timestamptz)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR a.at < sqlc.narg(to_at)::timestamptz)
  AND (sqlc.narg(before_id)::bigint IS NULL OR a.id < sqlc.narg(before_id)::bigint)
ORDER BY a.id DESC
LIMIT @lim;
