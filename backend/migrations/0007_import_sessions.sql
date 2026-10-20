-- Импорт из Excel/CSV (F4): превью хранится час, затем подтверждается одной транзакцией.

-- +goose Up
CREATE TABLE import_sessions (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  center_id    uuid NOT NULL REFERENCES centers(id),
  created_by   uuid NOT NULL REFERENCES users(id),
  filename     text,
  rows         jsonb NOT NULL,          -- разобранные строки превью со статусами
  created_at   timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,
  committed_at timestamptz,
  result       jsonb                    -- итоги commit (счётчики)
);
CREATE INDEX ON import_sessions (center_id, created_at DESC);

-- +goose Down
DROP TABLE import_sessions;
