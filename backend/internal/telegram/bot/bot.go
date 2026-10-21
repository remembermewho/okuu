// Package bot — Telegram-бот для родителей: привязка по приглашению (согласие → свой номер
// телефона), команды /balance, /pay, /attendance, /stop, /help (и /lang, если BOT_LANGUAGES включает
// кыргызский), постоянная клавиатура-меню. Один бот на все центры: чат может быть
// привязан к родителям в нескольких центрах, и в каждом сообщении — название центра.
//
// Апдейты (из вебхука или long polling) идут в очередь и обрабатываются воркерами; апдейты
// одного чата — всегда одним воркером, по порядку.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/alerts"
	"okuu/internal/db/sqlc"
	"okuu/internal/notify"
	"okuu/internal/payments"
	"okuu/internal/telegram"
)

const (
	defaultWorkers   = 4
	defaultQueueSize = 256
	// updateTimeout — сколько можно обрабатывать один апдейт (с учётом ожидания в очереди отправки).
	updateTimeout = 2 * time.Minute
)

// ErrQueueFull — очередь апдейтов переполнена: вебхук отвечает 503, Telegram повторит позже.
var ErrQueueFull = errors.New("bot: очередь апдейтов переполнена")

// Options — зависимости бота.
type Options struct {
	Pool      *pgxpool.Pool
	Sender    *telegram.Sender
	Now       func() time.Time // «сегодня» в поясе центра для /balance; nil — time.Now
	Logger    *slog.Logger
	Payments  *payments.Service // кнопка «Оплатить»; nil — «оплата недоступна»
	Alerts    *alerts.Alerter   // panic при обработке — алерт команде
	Langs     notify.Langs      // BOT_LANGUAGES; по умолчанию — только русский, без /lang
	Workers   int               // по умолчанию 4
	QueueSize int               // на воркер; по умолчанию 256
}

// Bot — обработчик апдейтов.
type Bot struct {
	pool     *pgxpool.Pool
	q        *sqlc.Queries
	tg       *telegram.Sender
	now      func() time.Time
	logger   *slog.Logger
	payments *payments.Service
	alerts   *alerts.Alerter
	langs    notify.Langs
	queues   []chan telegram.Update
}

// New создаёт бота; воркеры запускает Run.
func New(o Options) *Bot {
	b := &Bot{pool: o.Pool, q: sqlc.New(o.Pool), tg: o.Sender, now: o.Now, logger: o.Logger, payments: o.Payments, alerts: o.Alerts, langs: o.Langs}
	if b.now == nil {
		b.now = time.Now
	}
	if b.logger == nil {
		b.logger = slog.Default()
	}
	workers, size := o.Workers, o.QueueSize
	if workers <= 0 {
		workers = defaultWorkers
	}
	if size <= 0 {
		size = defaultQueueSize
	}
	b.queues = make([]chan telegram.Update, workers)
	for i := range b.queues {
		b.queues[i] = make(chan telegram.Update, size)
	}
	return b
}

// Run обрабатывает очередь до отмены stop: новых апдейтов не берёт, текущие доделывает
// с контекстом work (его отменяют, если остановка затянулась). Возвращается, когда все
// воркеры закончили.
func (b *Bot) Run(stop, work context.Context) {
	done := make(chan struct{})
	for _, q := range b.queues {
		go func() {
			defer func() { done <- struct{}{} }()
			for {
				select {
				case <-stop.Done():
					return
				case u := <-q:
					b.Handle(work, u)
				}
			}
		}()
	}
	for range b.queues {
		<-done
	}
}

// Enqueue ставит апдейт в очередь его чата; ждёт место не дольше ctx.
func (b *Bot) Enqueue(ctx context.Context, u telegram.Update) error {
	chat := u.ChatID()
	if chat < 0 {
		chat = -chat
	}
	q := b.queues[chat%int64(len(b.queues))]
	select {
	case q <- u:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", ErrQueueFull, ctx.Err())
	}
}

// Handle обрабатывает один апдейт синхронно. Ошибки пишутся в лог (без текста сообщений
// и номеров телефонов) — родителю ответ просто не придёт.
func (b *Bot) Handle(ctx context.Context, u telegram.Update) {
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("telegram: паника при обработке апдейта", "update_id", u.UpdateID, "panic", r, "stack", string(debug.Stack()))
			b.alerts.Alert("panic:telegram", fmt.Sprintf("panic в боте при обработке апдейта %d: %v", u.UpdateID, r))
		}
	}()
	if err := b.handle(ctx, u); err != nil && !errors.Is(err, telegram.ErrBlocked) {
		b.logger.Error("telegram: ошибка обработки апдейта", "update_id", u.UpdateID, "err", err)
	}
}

// MarkBlocked — обработчик 403 для telegram.Sender (R19): у всех активных родителей с этим
// чатом связь становится blocked. Напоминания им не шлются, пока родитель снова не напишет боту.
func MarkBlocked(pool *pgxpool.Pool, logger *slog.Logger) func(ctx context.Context, chatID int64) {
	q := sqlc.New(pool)
	return func(ctx context.Context, chatID int64) {
		rows, err := q.SetChatStatus(ctx, sqlc.SetChatStatusParams{Status: "blocked", ChatID: &chatID, FromStatuses: []string{"active"}})
		if err != nil {
			logger.Error("telegram: не удалось пометить blocked", "err", err)
			return
		}
		if len(rows) > 0 {
			logger.Info("telegram: бот заблокирован родителем", "parents", len(rows))
		}
	}
}
