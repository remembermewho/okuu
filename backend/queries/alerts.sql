-- Алерты по журналу: события, о которых нужно знать нам (не центру).

-- name: MaxAuditID :one
SELECT COALESCE(max(id), 0)::bigint FROM audit_log;

-- name: ListAlertAudit :many
-- Начисление с фиксированной скидкой больше цены (discount_capped) и оплата провайдера не на сумму ссылки.
SELECT a.id, a.center_id, c.name AS center_name, a.action, a.after
FROM audit_log a
JOIN centers c ON c.id = a.center_id
WHERE a.id > @after_id
  AND (a.action = 'payment.amount_mismatch'
       OR (a.action = 'ledger.charge' AND (a.after->>'discount_capped')::bool IS TRUE))
ORDER BY a.id
LIMIT 100;
