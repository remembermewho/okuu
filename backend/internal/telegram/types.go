// Package telegram — тонкий клиент Telegram Bot API на net/http и очередь отправки с лимитами.
// Здесь только транспорт: что отвечать родителю, решает пакет telegram/bot.
package telegram

// Только нужные поля Bot API (https://core.telegram.org/bots/api).

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// ChatID — чат, из которого пришёл апдейт (0 — не из чата).
func (u Update) ChatID() int64 {
	switch {
	case u.Message != nil:
		return u.Message.Chat.ID
	case u.CallbackQuery != nil && u.CallbackQuery.Message != nil:
		return u.CallbackQuery.Message.Chat.ID
	}
	return 0
}

type User struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot,omitempty"`
	FirstName    string `json:"first_name"`
	LanguageCode string `json:"language_code,omitempty"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"` // private, group, supergroup, channel
}

type Message struct {
	MessageID int64    `json:"message_id"`
	From      *User    `json:"from,omitempty"`
	Chat      Chat     `json:"chat"`
	Date      int64    `json:"date"`
	Text      string   `json:"text,omitempty"`
	Contact   *Contact `json:"contact,omitempty"`
}

type Contact struct {
	PhoneNumber string `json:"phone_number"`
	FirstName   string `json:"first_name"`
	UserID      int64  `json:"user_id,omitempty"` // есть, если контакт — пользователь Telegram
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

// ReplyMarkup — InlineKeyboardMarkup, ReplyKeyboardMarkup или ReplyKeyboardRemove.
type ReplyMarkup interface{ replyMarkup() }

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

type ReplyKeyboardMarkup struct {
	Keyboard        [][]KeyboardButton `json:"keyboard"`
	IsPersistent    bool               `json:"is_persistent,omitempty"` // клавиатура видна всегда, не сворачивается
	ResizeKeyboard  bool               `json:"resize_keyboard,omitempty"`
	OneTimeKeyboard bool               `json:"one_time_keyboard,omitempty"`
}

// BotCommand — команда в меню бота (кнопка «Меню» рядом с полем ввода).
type BotCommand struct {
	Command     string `json:"command"` // без «/»
	Description string `json:"description"`
}

type SetMyCommandsParams struct {
	Commands []BotCommand `json:"commands"`
}

type KeyboardButton struct {
	Text           string `json:"text"`
	RequestContact bool   `json:"request_contact,omitempty"`
}

type ReplyKeyboardRemove struct {
	RemoveKeyboard bool `json:"remove_keyboard"`
}

func (*InlineKeyboardMarkup) replyMarkup() {}
func (*ReplyKeyboardMarkup) replyMarkup()  {}
func (*ReplyKeyboardRemove) replyMarkup()  {}

// RemoveKeyboard убирает клавиатуру «Поделиться номером».
var RemoveKeyboard = &ReplyKeyboardRemove{RemoveKeyboard: true}

type SendMessageParams struct {
	ChatID      int64       `json:"chat_id"`
	Text        string      `json:"text"`
	ReplyMarkup ReplyMarkup `json:"reply_markup,omitempty"`
}

type EditMessageTextParams struct {
	ChatID      int64                 `json:"chat_id"`
	MessageID   int64                 `json:"message_id"`
	Text        string                `json:"text"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type AnswerCallbackQueryParams struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
}

type GetUpdatesParams struct {
	Offset         int64    `json:"offset,omitempty"`
	Timeout        int      `json:"timeout,omitempty"` // секунды long polling
	AllowedUpdates []string `json:"allowed_updates,omitempty"`
}

type SetWebhookParams struct {
	URL            string   `json:"url"` // пустой — снять вебхук (нужно для getUpdates)
	SecretToken    string   `json:"secret_token,omitempty"`
	AllowedUpdates []string `json:"allowed_updates,omitempty"`
}

// AllowedUpdates — какие апдейты боту нужны.
var AllowedUpdates = []string{"message", "callback_query"}
