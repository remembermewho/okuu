-- Оплата родителем (B2): ссылки оплаты, вебхук провайдера, сверка.
-- Время сравнивается с часами приложения (@now) — так же, как срок ссылки при создании.

-- name: GetStudentForPayLink :one
-- Ученик и его баланс для новой ссылки оплаты.
SELECT s.id, s.full_name, s.parent_id,
       (SELECT COALESCE(sum(l.amount), 0) FROM ledger l
        WHERE l.center_id = s.center_id AND l.student_id = s.id AND l.cancelled_at IS NULL)::bigint AS balance
FROM students s
WHERE s.center_id = @center_id AND s.id = @id;

-- name: CreatePaymentLink :one
INSERT INTO payment_links (token, center_id, parent_id, student_id, amount, provider, expires_at)
VALUES (@token, @center_id, @parent_id, @student_id, @amount, @provider, @expires_at)
RETURNING id, amount, expires_at;

-- name: GetChatStudent :one
-- Ученик, если он ребёнок родителя, привязанного к этому чату (кнопка «Оплатить» в боте).
SELECT s.id, s.center_id, s.full_name, p.lang, c.name AS center_name
FROM students s
JOIN parents p ON p.id = s.parent_id AND p.center_id = s.center_id
JOIN centers c ON c.id = s.center_id
WHERE s.id = @student_id AND p.telegram_chat_id = @chat_id AND p.tg_status <> 'none' AND NOT c.is_demo;

-- name: GetPayLink :one
-- Ссылка для публичной страницы и старта оплаты; lock — отдельным запросом ниже.
SELECT l.id, l.center_id, l.student_id, l.amount, l.status, l.expires_at, l.provider, l.provider_payment_id,
       l.pay_url, l.qr_payload, s.full_name, c.name AS center_name, c.pay_details,
       (c.finik_creds IS NOT NULL)::bool AS finik_connected
FROM payment_links l
JOIN students s ON s.id = l.student_id
JOIN centers c ON c.id = l.center_id
WHERE l.token = @token;

-- name: LockPayLink :one
SELECT id, status, expires_at, provider, provider_payment_id, pay_url, qr_payload
FROM payment_links WHERE id = @id FOR UPDATE;

-- name: SetPayLinkPayment :exec
UPDATE payment_links
SET provider = @provider, provider_payment_id = @provider_payment_id, pay_url = @pay_url, qr_payload = @qr_payload
WHERE id = @id;

-- name: LockPayLinkByProviderPayment :one
SELECT l.id, l.center_id, l.student_id, l.amount, l.status, l.ledger_id
FROM payment_links l
WHERE l.provider = @provider AND l.provider_payment_id = @provider_payment_id
FOR UPDATE;

-- name: InsertProviderPayment :one
-- Оплата от провайдера. Тот же external_id в центре — строки нет: оплата уже записана (вебхук повторился).
INSERT INTO ledger (center_id, student_id, kind, amount, method, external_id, comment)
VALUES (@center_id, @student_id, 'payment', @amount, @method, @external_id, @comment)
ON CONFLICT (center_id, external_id) WHERE external_id IS NOT NULL DO NOTHING
RETURNING id;

-- name: MarkPayLinkPaid :exec
UPDATE payment_links SET status = 'paid', paid_at = @paid_at, ledger_id = @ledger_id WHERE id = @id;

-- name: ListPendingProviderLinks :many
-- Ссылки, по которым начат платёж у провайдера, а вебхука ещё не было.
SELECT l.id, l.center_id, l.provider_payment_id::text AS provider_payment_id, c.name AS center_name
FROM payment_links l
JOIN centers c ON c.id = l.center_id
WHERE l.status = 'pending' AND l.provider = @provider AND l.provider_payment_id IS NOT NULL
ORDER BY l.created_at
LIMIT 200;

-- name: ExpirePayLinks :execrows
UPDATE payment_links SET status = 'expired'
WHERE status = 'pending' AND expires_at <= @now;
