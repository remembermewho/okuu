-- Одноразовые ссылки сброса пароля сотрудника ({APP_ORIGIN}/reset/<токен>), срок 24 часа.
-- token — hex(sha256) от токена из ссылки, сам токен не хранится. center_id — центр владельца, который
-- выдал ссылку; NULL — выдана поддержкой (server user reset-link). Новая ссылка отзывает прежние
-- неиспользованные ссылки этого пользователя (revoked_at).

-- +goose Up
CREATE TABLE password_resets (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  token      text NOT NULL UNIQUE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  center_id  uuid REFERENCES centers(id),
  created_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  used_at    timestamptz,
  revoked_at timestamptz
);
CREATE INDEX password_resets_user_active ON password_resets (user_id)
  WHERE used_at IS NULL AND revoked_at IS NULL;

-- +goose Down
DROP TABLE password_resets;
