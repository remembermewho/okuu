-- Рассылки центра и WhatsApp-напоминания по ссылке.

-- name: InsertWhatsAppNotification :exec
-- Сотрудник открыл ссылку wa.me: отправка на его стороне, поэтому статус — link_opened, а не sent.
INSERT INTO notifications (center_id, parent_id, kind, channel, status, student_ids, created_by, sent_at)
VALUES (@center_id, @parent_id, @kind, 'whatsapp_link', 'link_opened', @student_ids::uuid[], @created_by, @sent_at);

-- name: TouchParentWhatsApp :exec
UPDATE parents SET last_whatsapp_at = @opened_at WHERE center_id = @center_id AND id = @id;

-- name: GetBroadcastByKey :one
SELECT * FROM broadcasts WHERE center_id = @center_id AND idempotency_key = @idempotency_key;

-- name: BroadcastSentSince :one
-- Была ли рассылка центра после since — лимит одна в 5 минут.
SELECT EXISTS (SELECT 1 FROM broadcasts WHERE center_id = @center_id AND created_at > @since);

-- name: ListCenterGroupsByIDs :many
SELECT id, name FROM groups WHERE center_id = @center_id AND id = ANY(@ids::uuid[]) ORDER BY name, id;

-- name: ListBroadcastAudience :many
-- Родители учеников центра, кроме ушедших; by_groups — только с активной записью в одну из групп.
-- Дети одного родителя — одна строка, одно сообщение.
SELECT p.id AS parent_id, p.name AS parent_name, p.phone AS parent_phone, p.tg_status, p.telegram_chat_id, p.lang,
       array_agg(s.id ORDER BY s.full_name, s.id)::uuid[] AS student_ids,
       array_agg(s.full_name ORDER BY s.full_name, s.id)::text[] AS student_names
FROM students s
JOIN parents p ON p.id = s.parent_id
WHERE s.center_id = @center_id AND s.status <> 'left'
  AND (NOT @by_groups::bool OR EXISTS (
    SELECT 1 FROM enrollments e
    WHERE e.center_id = s.center_id AND e.student_id = s.id AND e.end_date IS NULL AND e.group_id = ANY(@group_ids::uuid[])
  ))
GROUP BY p.id
ORDER BY p.name, p.id;

-- name: CountAudienceWithoutParent :one
SELECT count(*)::int FROM students s
WHERE s.center_id = @center_id AND s.status <> 'left' AND s.parent_id IS NULL
  AND (NOT @by_groups::bool OR EXISTS (
    SELECT 1 FROM enrollments e
    WHERE e.center_id = s.center_id AND e.student_id = s.id AND e.end_date IS NULL AND e.group_id = ANY(@group_ids::uuid[])
  ));

-- name: InsertBroadcast :one
INSERT INTO broadcasts (center_id, created_by, idempotency_key, audience, group_ids, group_names, body, no_parent_students, created_at)
VALUES (@center_id, @created_by, @idempotency_key, @audience, @group_ids::uuid[], @group_names::text[], @body, @no_parent_students, @created_at)
RETURNING *;

-- name: InsertBroadcastRecipient :exec
INSERT INTO broadcast_recipients (broadcast_id, center_id, parent_id, status, student_ids, student_names)
VALUES (@broadcast_id, @center_id, @parent_id, @status, @student_ids::uuid[], @student_names::text[]);

-- name: ListBroadcasts :many
SELECT b.id, b.audience, b.group_names, b.body, b.no_parent_students, b.created_at, u.name AS author_name,
       r.telegram_sent, r.telegram_queued, r.whatsapp_total, r.whatsapp_opened, r.no_contact
FROM broadcasts b
JOIN users u ON u.id = b.created_by
CROSS JOIN LATERAL (
  SELECT count(*) FILTER (WHERE x.status = 'sent')::int AS telegram_sent,
         count(*) FILTER (WHERE x.status = 'queued')::int AS telegram_queued,
         count(*) FILTER (WHERE x.status IN ('whatsapp_pending', 'whatsapp_opened'))::int AS whatsapp_total,
         count(*) FILTER (WHERE x.status = 'whatsapp_opened')::int AS whatsapp_opened,
         count(*) FILTER (WHERE x.status = 'no_contact')::int AS no_contact
  FROM broadcast_recipients x WHERE x.broadcast_id = b.id
) r
WHERE b.center_id = @center_id
  AND (sqlc.narg(id)::uuid IS NULL OR b.id = sqlc.narg(id)::uuid)
ORDER BY b.created_at DESC, b.id DESC
LIMIT @lim;

-- name: ListBroadcastLinks :many
-- Кому писать вручную: сначала те, у кого есть WhatsApp-ссылка, в конце — без контакта.
SELECT r.parent_id, r.status, r.student_names, p.name AS parent_name, p.phone AS parent_phone, p.lang
FROM broadcast_recipients r
JOIN parents p ON p.id = r.parent_id AND p.center_id = r.center_id
WHERE r.center_id = @center_id AND r.broadcast_id = @broadcast_id
  AND r.status IN ('whatsapp_pending', 'whatsapp_opened', 'no_contact')
ORDER BY (r.status = 'no_contact'), p.name, p.id;

-- name: ListQueuedBroadcastRecipients :many
-- Очередь бота. Центр заблокировали — сообщения ждут.
SELECT r.broadcast_id, r.center_id, r.parent_id, r.student_ids, b.body, b.created_by, c.name AS center_name,
       p.phone AS parent_phone, p.tg_status, p.telegram_chat_id, p.lang
FROM broadcast_recipients r
JOIN broadcasts b ON b.id = r.broadcast_id
JOIN centers c ON c.id = r.center_id
JOIN parents p ON p.id = r.parent_id
WHERE r.status = 'queued' AND c.status = 'active' AND NOT c.is_demo
ORDER BY b.created_at, r.broadcast_id, r.parent_id
LIMIT @lim;

-- name: SetBroadcastRecipientStatus :exec
UPDATE broadcast_recipients SET status = @status WHERE broadcast_id = @broadcast_id AND parent_id = @parent_id;

-- name: GetBroadcastLinkRecipient :one
-- Получатель со ссылкой WhatsApp в рассылке этого центра.
SELECT status, student_ids FROM broadcast_recipients
WHERE center_id = @center_id AND broadcast_id = @broadcast_id AND parent_id = @parent_id
  AND status IN ('whatsapp_pending', 'whatsapp_opened');

-- name: OpenBroadcastRecipient :execrows
UPDATE broadcast_recipients SET status = 'whatsapp_opened', opened_at = @opened_at
WHERE center_id = @center_id AND broadcast_id = @broadcast_id AND parent_id = @parent_id AND status = 'whatsapp_pending';

-- name: LockCenterBroadcast :exec
-- Лимит рассылок и идемпотентность проверяются под блокировкой центра.
SELECT pg_advisory_xact_lock(@key::bigint);
