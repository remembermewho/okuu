-- Запросы Telegram-бота. Бот работает не от имени сотрудника: центр берётся из приглашения
-- (привязка) или из привязанных к чату родителей (остальные команды).
-- Привязан — telegram_chat_id задан и tg_status <> 'none'.

-- name: GetParentInviteByToken :one
-- Действующее приглашение родителя: не использовано, не отозвано, не просрочено (по часам БД).
SELECT i.id AS invite_id, i.center_id, p.id AS parent_id, p.name AS parent_name, p.phone AS parent_phone,
       c.name AS center_name
FROM invites i
JOIN parents p ON p.id = i.parent_id AND p.center_id = i.center_id
JOIN centers c ON c.id = i.center_id
WHERE i.token = @token AND i.kind = 'parent' AND NOT c.is_demo
  AND i.used_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > now();

-- name: GetParentInviteByID :one
SELECT i.id AS invite_id, i.center_id, p.id AS parent_id, p.name AS parent_name, p.phone AS parent_phone,
       c.name AS center_name
FROM invites i
JOIN parents p ON p.id = i.parent_id AND p.center_id = i.center_id
JOIN centers c ON c.id = i.center_id
WHERE i.id = @id AND i.kind = 'parent'
  AND i.used_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > now();

-- name: ClaimParentInvite :one
-- Атомарно помечает приглашение использованным (вызывать в транзакции привязки).
UPDATE invites
SET used_at = now()
WHERE id = @id AND kind = 'parent'
  AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()
RETURNING id;

-- name: ListParentChildNames :many
-- Дети родителя для приветствия: без ушедших.
SELECT full_name FROM students
WHERE center_id = @center_id AND parent_id = @parent_id AND status <> 'left'
ORDER BY full_name, id;

-- name: UpsertLinkState :exec
-- Новый /start с приглашением начинает привязку в этом чате заново.
INSERT INTO telegram_link_states (chat_id, user_id, invite_id)
VALUES (@chat_id, @user_id, @invite_id)
ON CONFLICT (chat_id) DO UPDATE
SET user_id = EXCLUDED.user_id, invite_id = EXCLUDED.invite_id, consent_at = NULL, updated_at = now();

-- name: GetLinkState :one
SELECT * FROM telegram_link_states WHERE chat_id = @chat_id;

-- name: SetLinkConsent :one
-- Согласие принимается только от того, кто открыл ссылку, и только по текущему приглашению чата.
UPDATE telegram_link_states
SET consent_at = COALESCE(consent_at, now()), updated_at = now()
WHERE chat_id = @chat_id AND user_id = @user_id AND invite_id = @invite_id
RETURNING *;

-- name: DeleteLinkState :exec
DELETE FROM telegram_link_states WHERE chat_id = @chat_id;

-- name: LinkParentTelegram :one
UPDATE parents
SET telegram_chat_id = @chat_id, tg_status = 'active', consent_at = @consent_at, lang = @lang
WHERE center_id = @center_id AND id = @id
RETURNING *;

-- name: ListChatParents :many
-- Родители, привязанные к чату, во всех центрах.
SELECT p.id, p.center_id, p.name, p.lang, p.tg_status, c.name AS center_name, c.timezone
FROM parents p
JOIN centers c ON c.id = p.center_id
WHERE p.telegram_chat_id = @chat_id AND p.tg_status <> 'none' AND NOT c.is_demo -- демо-центр боту не виден
ORDER BY c.name, c.id, p.id;

-- name: SetChatLang :exec
UPDATE parents SET lang = @lang
WHERE telegram_chat_id = @chat_id AND tg_status <> 'none';

-- name: SetChatStatus :many
-- Меняет tg_status у родителей чата со статусом из from_statuses; возвращает изменённых.
UPDATE parents SET tg_status = @status
WHERE telegram_chat_id = @chat_id AND tg_status = ANY(@from_statuses::text[])
RETURNING id, center_id;

-- name: ListParentChildrenSummary :many
-- Дети родителя для /balance: баланс (R13), посещения за [from, to] без отменённых занятий,
-- активные абонементы с остатком.
SELECT s.id, s.full_name, s.status,
       (SELECT COALESCE(sum(l.amount), 0) FROM ledger l
        WHERE l.center_id = s.center_id AND l.student_id = s.id AND l.cancelled_at IS NULL)::bigint AS balance,
       att.present, att.absent, att.excused,
       COALESCE((SELECT jsonb_agg(jsonb_build_object('group', g.name, 'lessons_left', e.lessons_left)
                                  ORDER BY g.name, e.id)
                 FROM enrollments e
                 JOIN tariffs t ON t.id = e.tariff_id
                 JOIN groups g ON g.id = e.group_id
                 WHERE e.center_id = s.center_id AND e.student_id = s.id AND e.end_date IS NULL
                   AND t.kind = 'package'), '[]'::jsonb)::jsonb AS packages
FROM students s
CROSS JOIN LATERAL (
  SELECT count(*) FILTER (WHERE a.status = 'present')::int AS present,
         count(*) FILTER (WHERE a.status = 'absent')::int AS absent,
         count(*) FILTER (WHERE a.status = 'excused')::int AS excused
  FROM attendance a
  JOIN lessons ls ON ls.id = a.lesson_id
  WHERE a.student_id = s.id AND ls.center_id = s.center_id AND NOT ls.cancelled
    AND ls.date BETWEEN @from_date AND @to_date
) att
WHERE s.center_id = @center_id AND s.parent_id = @parent_id
ORDER BY s.full_name, s.id;
