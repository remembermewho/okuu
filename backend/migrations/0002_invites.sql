-- Приглашения сотрудников: публичный id для отзыва и аудита, имя для формы принятия,
-- кто и когда создал, отзыв. В token по-прежнему hex(sha256) от токена из ссылки.

-- +goose Up
ALTER TABLE invites
  ADD COLUMN id         uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  ADD COLUMN name       text,
  ADD COLUMN created_by uuid REFERENCES users(id),
  ADD COLUMN created_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN revoked_at timestamptz;
CREATE INDEX ON invites (center_id, created_at DESC);

-- +goose Down
ALTER TABLE invites
  DROP COLUMN revoked_at,
  DROP COLUMN created_at,
  DROP COLUMN created_by,
  DROP COLUMN name,
  DROP COLUMN id;
