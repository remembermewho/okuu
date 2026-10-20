-- Ссылки оплаты для родителя (B2): одна ссылка — один ребёнок, сумма — его долг на момент
-- создания, срок 24 часа. token — hex(sha256) от токена из ссылки, сам токен не хранится.
-- provider — кто принимает оплату: manual (реквизиты центра, оплату отмечает администратор)
-- или finik; provider_payment_id — id платежа у провайдера (он же ledger.external_id).
-- Таблица до этой миграции не использовалась (пустая), поэтому student_id сразу NOT NULL.

-- +goose Up
ALTER TABLE payment_links RENAME COLUMN finik_id TO provider_payment_id;
ALTER TABLE payment_links
  ADD COLUMN id         uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  ADD COLUMN student_id uuid NOT NULL REFERENCES students(id),
  ADD COLUMN provider   text NOT NULL DEFAULT 'manual' CHECK (provider IN ('manual', 'finik')),
  ADD COLUMN pay_url    text,
  ADD COLUMN qr_payload text,
  ADD COLUMN created_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN paid_at    timestamptz,
  ADD COLUMN ledger_id  uuid REFERENCES ledger(id);
CREATE UNIQUE INDEX payment_links_provider_payment ON payment_links (provider, provider_payment_id)
  WHERE provider_payment_id IS NOT NULL;
CREATE INDEX payment_links_pending ON payment_links (expires_at) WHERE status = 'pending';
CREATE INDEX payment_links_student ON payment_links (center_id, student_id, created_at DESC);

-- +goose Down
DROP INDEX payment_links_student;
DROP INDEX payment_links_pending;
DROP INDEX payment_links_provider_payment;
ALTER TABLE payment_links
  DROP COLUMN ledger_id, DROP COLUMN paid_at, DROP COLUMN created_at, DROP COLUMN qr_payload,
  DROP COLUMN pay_url, DROP COLUMN provider, DROP COLUMN student_id, DROP COLUMN id;
ALTER TABLE payment_links RENAME COLUMN provider_payment_id TO finik_id;
