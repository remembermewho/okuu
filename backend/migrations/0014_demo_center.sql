-- Демо-центр для показа (server demo seed): вымышленные ученики и родители. Для него не отправляются
-- напоминания и уведомления в Telegram, не создаются приглашения в бот; server demo reset удаляет только
-- центры с is_demo.

-- +goose Up
ALTER TABLE centers ADD COLUMN is_demo boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE centers DROP COLUMN is_demo;
