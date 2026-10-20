-- name: InsertParentIfAbsent :one
-- Нет строки в ответе — родитель с этим телефоном в центре уже есть (берите GetParentByPhone).
INSERT INTO parents (center_id, name, phone)
VALUES (@center_id, @name, @phone)
ON CONFLICT (center_id, phone) DO NOTHING
RETURNING *;

-- name: GetParentByPhone :one
SELECT * FROM parents
WHERE center_id = @center_id AND phone = @phone;

-- name: GetParentForUpdate :one
SELECT * FROM parents
WHERE center_id = @center_id AND id = @id
FOR UPDATE;

-- name: UpdateParent :one
-- Смена телефона отвязывает Telegram: привязку подтверждали старым номером
-- (в SET phone — ещё старое значение). Родителю нужно новое приглашение.
UPDATE parents
SET name = @name, phone = @phone,
    telegram_chat_id = CASE WHEN phone = @phone THEN telegram_chat_id END,
    tg_status = CASE WHEN phone = @phone THEN tg_status ELSE 'none' END,
    consent_at = CASE WHEN phone = @phone THEN consent_at END
WHERE center_id = @center_id AND id = @id
RETURNING *;

-- name: GetParent :one
SELECT p.*, c.name AS center_name
FROM parents p
JOIN centers c ON c.id = p.center_id
WHERE p.center_id = @center_id AND p.id = @id;

-- name: CreateParentInvite :one
-- token = hex(sha256) от токена из ссылки t.me/<bot>?start=<токен>. Срок — 30 дней.
INSERT INTO invites (token, center_id, kind, parent_id, created_by, expires_at)
VALUES (@token, @center_id, 'parent', @parent_id, @created_by, now() + interval '30 days')
RETURNING id, expires_at;
