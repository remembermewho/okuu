-- Ручные операции (оплата, возврат, поправка) приходят с заголовком Idempotency-Key:
-- повтор запроса с тем же ключом не создаёт вторую операцию.

-- +goose Up
ALTER TABLE ledger ADD COLUMN idempotency_key text;
CREATE UNIQUE INDEX ledger_idempotency_key ON ledger (center_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
-- История ученика: новые сверху.
CREATE INDEX ON ledger (student_id, created_at DESC, id DESC);

-- +goose Down
DROP INDEX ledger_student_id_created_at_id_idx;
DROP INDEX ledger_idempotency_key;
ALTER TABLE ledger DROP COLUMN idempotency_key;
