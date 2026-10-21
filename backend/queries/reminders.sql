-- Напоминания и уведомления родителям (R17–R19).

-- name: EnqueueOutbox :exec
-- Событие (paid, package_low, absent) — в транзакции самого события. Повтор ключа — ничего.
INSERT INTO notification_outbox (center_id, kind, dedup_key, student_id, ref_id)
VALUES (@center_id, @kind, @dedup_key, @student_id, @ref_id)
ON CONFLICT (dedup_key) DO NOTHING;

-- name: NotifyOutbox :exec
-- Будит диспетчер (LISTEN okuu_notify); доставляется после COMMIT.
SELECT pg_notify('okuu_notify', '');

-- name: ListOutbox :many
-- expired — событие старше 3 дней (по часам БД): такое уже не отправляем.
SELECT o.id, o.center_id, o.kind, o.dedup_key, o.student_id, o.ref_id, o.attempts,
       (o.created_at < now() - interval '3 days')::bool AS expired,
       c.name AS center_name, c.timezone, c.reminders_on, c.notify_absent
FROM notification_outbox o
JOIN centers c ON c.id = o.center_id
ORDER BY o.id
LIMIT @lim;

-- name: DeleteOutbox :exec
DELETE FROM notification_outbox WHERE id = @id;

-- name: BumpOutboxAttempts :one
UPDATE notification_outbox SET attempts = attempts + 1 WHERE id = @id RETURNING attempts;

-- name: ClaimNotification :one
-- Запись об отправке до самой отправки: ключ уже занят — сообщение уже ушло (строки нет).
-- Статус потом уточняет SetNotificationResult.
INSERT INTO notifications (center_id, parent_id, kind, status, dedup_key, student_ids, created_by, sent_at)
VALUES (@center_id, @parent_id, @kind, 'sent', sqlc.narg(dedup_key), @student_ids::uuid[], sqlc.narg(created_by), @sent_at)
ON CONFLICT (dedup_key) DO NOTHING
RETURNING id;

-- name: SetNotificationResult :exec
-- failed — ключ снимается: следующий запуск попробует снова.
UPDATE notifications
SET status = @status, error = sqlc.narg(error),
    dedup_key = CASE WHEN @status::text = 'failed' THEN NULL ELSE dedup_key END
WHERE id = @id;

-- name: DebtNotifiedSince :one
-- Было ли родителю напоминание о долге (авто или ручное) с момента since — лимит R17.
SELECT EXISTS (
  SELECT 1 FROM notifications
  WHERE parent_id = @parent_id AND status = 'sent' AND sent_at >= @since
    AND kind IN ('due_soon', 'due_today', 'overdue_3', 'overdue_7', 'overdue_weekly', 'manual')
);

-- name: RecentManualStudents :many
-- Ученики из ids, о которых ручное напоминание уже ушло после since (лимит 1 в час, R17).
SELECT DISTINCT x.student_id::uuid AS student_id
FROM notifications n
CROSS JOIN LATERAL unnest(n.student_ids) AS x(student_id)
WHERE n.center_id = @center_id AND n.kind = 'manual' AND n.status = 'sent' AND n.sent_at > @since
  AND x.student_id = ANY(@ids::uuid[]);

-- name: LockCenterReminders :exec
-- Ручные напоминания центра — по одному: проверка лимита и запись идут под этой блокировкой.
-- key считает код из id центра.
SELECT pg_advisory_xact_lock(@key::bigint);

-- name: TouchParentReminder :exec
UPDATE parents SET last_reminder_at = @reminded_at WHERE id = @id;

-- name: ListCentersForReminders :many
SELECT id, name, timezone, pay_details, (finik_creds IS NOT NULL)::bool AS finik_connected, overdue_repeat
FROM centers
WHERE reminders_on
ORDER BY id;

-- name: ListReminderStudents :many
-- Ученики центра с балансом и родителем. ids NULL — все; only_debt — только с отрицательным балансом.
SELECT s.id, s.full_name, s.parent_id, p.tg_status AS parent_tg_status, p.telegram_chat_id AS parent_chat_id,
       p.lang AS parent_lang, b.balance
FROM students s
LEFT JOIN parents p ON p.id = s.parent_id
CROSS JOIN LATERAL (
  SELECT COALESCE(sum(l.amount), 0)::bigint AS balance FROM ledger l
  WHERE l.center_id = s.center_id AND l.student_id = s.id AND l.cancelled_at IS NULL
) b
WHERE s.center_id = @center_id
  AND (sqlc.narg(ids)::uuid[] IS NULL OR s.id = ANY(sqlc.narg(ids)::uuid[]))
  AND (NOT @only_debt::bool OR b.balance < 0)
ORDER BY s.full_name, s.id;

-- name: GetPaidEvent :one
-- Оплата для уведомления paid: неотменённая; баланс — после неё (и всего остального).
SELECT l.amount, s.full_name, s.parent_id, p.tg_status AS parent_tg_status, p.telegram_chat_id AS parent_chat_id,
       p.lang AS parent_lang,
       (SELECT COALESCE(sum(x.amount), 0) FROM ledger x
        WHERE x.center_id = s.center_id AND x.student_id = s.id AND x.cancelled_at IS NULL)::bigint AS balance
FROM ledger l
JOIN students s ON s.id = l.student_id AND s.center_id = l.center_id
JOIN parents p ON p.id = s.parent_id
WHERE l.center_id = @center_id AND l.id = @id AND l.kind = 'payment' AND l.cancelled_at IS NULL;

-- name: GetPackageLowEvent :one
-- Абонемент для package_low: запись активна.
SELECT e.lessons_left, g.name AS group_name, s.full_name, s.parent_id, p.tg_status AS parent_tg_status,
       p.telegram_chat_id AS parent_chat_id, p.lang AS parent_lang
FROM enrollments e
JOIN groups g ON g.id = e.group_id
JOIN students s ON s.id = e.student_id AND s.center_id = e.center_id
JOIN parents p ON p.id = s.parent_id
WHERE e.center_id = @center_id AND e.id = @id AND e.end_date IS NULL;

-- name: GetAbsentEvent :one
-- Отметка «не был» для absent: всё ещё «не был», занятие не отменено.
SELECT ls.date, ls.start_time, g.name AS group_name, s.full_name, s.parent_id, p.tg_status AS parent_tg_status,
       p.telegram_chat_id AS parent_chat_id, p.lang AS parent_lang
FROM attendance a
JOIN lessons ls ON ls.id = a.lesson_id
JOIN groups g ON g.id = ls.group_id
JOIN students s ON s.id = a.student_id AND s.center_id = ls.center_id
JOIN parents p ON p.id = s.parent_id
WHERE ls.center_id = @center_id AND a.lesson_id = @lesson_id AND a.student_id = @student_id
  AND a.status = 'absent' AND NOT ls.cancelled;
