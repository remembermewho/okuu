// Package alerts — алерты команде Окуу в служебный Telegram-чат (ALERT_TELEGRAM_CHAT_ID):
// panic, ошибки фоновых задач, discount_capped, payment.amount_mismatch. Без чата — только лог.
// Одинаковый алерт (тот же ключ) — не чаще раза в Window; пропущенные считаются и упоминаются
// в следующем.
package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/db/sqlc"
	"okuu/internal/notify"
	"okuu/internal/telegram"
)

// DefaultWindow — один и тот же алерт не чаще раза в 10 минут.
const DefaultWindow = 10 * time.Minute

const (
	sendTimeout = 30 * time.Second
	maxText     = 3500
)

// Sender — очередь отправки Telegram (*telegram.Sender).
type Sender interface {
	Send(ctx context.Context, p telegram.SendMessageParams) (telegram.Message, error)
}

// Options — настройки; Sender или ChatID пустые — алерты только в лог.
type Options struct {
	Sender Sender
	ChatID int64
	Env    string // dev | prod — в тексте алерта
	Logger *slog.Logger
	Now    func() time.Time
	Window time.Duration
}

// Alerter шлёт алерты. Методы безопасны для nil (ничего не делают) и для одновременных вызовов.
type Alerter struct {
	sender Sender
	chatID int64
	env    string
	logger *slog.Logger
	now    func() time.Time
	window time.Duration

	mu         sync.Mutex
	last       map[string]time.Time
	suppressed map[string]int
	wg         sync.WaitGroup
}

// New создаёт алертер.
func New(o Options) *Alerter {
	a := &Alerter{sender: o.Sender, chatID: o.ChatID, env: o.Env, logger: o.Logger, now: o.Now, window: o.Window,
		last: map[string]time.Time{}, suppressed: map[string]int{}}
	if a.logger == nil {
		a.logger = slog.Default()
	}
	if a.now == nil {
		a.now = time.Now
	}
	if a.window <= 0 {
		a.window = DefaultWindow
	}
	return a
}

// Alert — алерт с ключом key (одинаковые ключи склеиваются) и текстом без персональных данных.
// Всегда пишется в лог; в чат — асинхронно, чтобы не задерживать того, кто сообщил.
func (a *Alerter) Alert(key, text string) {
	if a == nil {
		return
	}
	a.logger.Error("алерт", "alert_key", key, "alert", text)

	a.mu.Lock()
	now := a.now()
	if last, ok := a.last[key]; ok && now.Sub(last) < a.window {
		a.suppressed[key]++
		a.mu.Unlock()
		return
	}
	skipped := a.suppressed[key]
	delete(a.suppressed, key)
	a.last[key] = now
	a.mu.Unlock()

	if a.sender == nil || a.chatID == 0 {
		return
	}
	msg := fmt.Sprintf("⚠️ Окуу (%s): %s", a.env, text)
	if skipped > 0 {
		msg += fmt.Sprintf("\n\n(ещё таких же за %d мин: %d — не отправлялись)", int(a.window.Minutes()), skipped)
	}
	if len(msg) > maxText {
		msg = msg[:maxText] + "…"
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		if _, err := a.sender.Send(ctx, telegram.SendMessageParams{ChatID: a.chatID, Text: msg}); err != nil {
			a.logger.Warn("алерт не отправлен в Telegram", "alert_key", key, "err", err)
		}
	}()
}

// Wait ждёт отправки уже начатых алертов (остановка сервера, тесты).
func (a *Alerter) Wait() {
	if a != nil {
		a.wg.Wait()
	}
}

// WatchAudit раз в interval проверяет новые записи журнала, о которых нужен алерт:
// discount_capped (скидка больше цены) и payment.amount_mismatch. Смотрит только записи,
// появившиеся после старта, — только закоммиченные изменения.
func (a *Alerter) WatchAudit(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	if a == nil {
		return
	}
	q := sqlc.New(pool)
	last, err := q.MaxAuditID(ctx)
	if err != nil && ctx.Err() == nil {
		a.logger.Error("алерты: журнал недоступен", "err", err)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if last, err = a.CheckAudit(ctx, q, last); err != nil && ctx.Err() == nil {
			a.logger.Error("алерты: проверка журнала", "err", err)
		}
	}
}

// CheckAudit — один проход по журналу после afterID; возвращает последний просмотренный id.
func (a *Alerter) CheckAudit(ctx context.Context, q *sqlc.Queries, afterID int64) (int64, error) {
	rows, err := q.ListAlertAudit(ctx, afterID)
	if err != nil {
		return afterID, err
	}
	for _, r := range rows {
		afterID = r.ID
		var after map[string]any
		_ = json.Unmarshal(r.After, &after)
		switch r.Action {
		case "payment.amount_mismatch":
			a.Alert("amount_mismatch:"+r.CenterID.String(), fmt.Sprintf(
				"центр «%s»: оплата от провайдера на %s сом, а ссылка была на %s сом (платёж %v). Оплата записана как пришла — проверьте.",
				r.CenterName, somOf(after["paid_amount"]), somOf(after["link_amount"]), after["external_id"]))
		default:
			a.Alert("discount_capped:"+r.CenterID.String(), fmt.Sprintf(
				"центр «%s»: фиксированная скидка больше цены тарифа — начисление на 0 сом (ученик %v). Проверьте скидку.",
				r.CenterName, after["student_id"]))
		}
	}
	return afterID, nil
}

func somOf(v any) string {
	if f, ok := v.(float64); ok {
		return notify.Som(int64(f))
	}
	return fmt.Sprint(v)
}
