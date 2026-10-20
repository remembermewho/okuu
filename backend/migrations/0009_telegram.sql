-- Telegram-бот: привязка родителей (R18–R19).
-- parents.last_reminder_at — когда родителю последний раз ушло напоминание (заполняет reminders, часть E).
-- telegram_link_states — шаг привязки в чате: по какому приглашению пришли, дано ли согласие.
-- Это состояние чата, а не данные центра: читается только ботом по chat_id.

-- +goose Up
ALTER TABLE parents ADD COLUMN last_reminder_at timestamptz;

-- Один чат может быть привязан к родителям в нескольких центрах: chat_id не уникален.
CREATE INDEX parents_telegram_chat_id ON parents (telegram_chat_id) WHERE telegram_chat_id IS NOT NULL;

CREATE TABLE telegram_link_states (
  chat_id    bigint PRIMARY KEY,
  user_id    bigint NOT NULL,      -- from.id того, кто открыл ссылку: контакт должен быть его
  invite_id  uuid NOT NULL REFERENCES invites(id) ON DELETE CASCADE,
  consent_at timestamptz,          -- нажал «Согласен»
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE telegram_link_states;
DROP INDEX parents_telegram_chat_id;
ALTER TABLE parents DROP COLUMN last_reminder_at;
