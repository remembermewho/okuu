package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Лимиты Telegram на отправку: не больше 30 сообщений в секунду на бота (берём 25 с запасом)
// и не больше одного в секунду в один чат.
const (
	GlobalPerSecond = 25
	PerChatInterval = time.Second
	// maxAttempts — попыток на сообщение при 429 (первая + повторы после retry_after).
	maxAttempts = 4
	// staleChats — после стольких чатов в памяти забываем тех, кому писали больше секунды назад.
	staleChats = 1000
)

// ErrBlocked — Telegram ответил 403: бот заблокирован, сообщение не доставлено.
var ErrBlocked = errors.New("telegram: бот заблокирован пользователем")

// Sender — очередь отправки: каждый вызов ждёт своего места в пределах лимитов и только потом
// идёт в Telegram. Безопасен для одновременного использования. 429 — ждёт retry_after (все
// отправки встают на паузу) и повторяет; 403 — вызывает OnBlocked и возвращает ErrBlocked.
type Sender struct {
	api       API
	now       func() time.Time
	sleep     func(ctx context.Context, d time.Duration) error
	onBlocked func(ctx context.Context, chatID int64)
	logger    *slog.Logger

	mu          sync.Mutex
	window      []time.Time // моменты отправок за последнюю секунду, по возрастанию
	lastByChat  map[int64]time.Time
	pausedUntil time.Time // после 429
}

// SenderOptions — необязательные настройки; Now и Sleep подменяются в тестах.
type SenderOptions struct {
	Now       func() time.Time
	Sleep     func(ctx context.Context, d time.Duration) error
	OnBlocked func(ctx context.Context, chatID int64) // 403 от Telegram (R19)
	Logger    *slog.Logger
}

// NewSender создаёт очередь поверх api.
func NewSender(api API, opt SenderOptions) *Sender {
	s := &Sender{api: api, now: opt.Now, sleep: opt.Sleep, onBlocked: opt.OnBlocked, logger: opt.Logger,
		lastByChat: map[int64]time.Time{}}
	if s.now == nil {
		s.now = time.Now
	}
	if s.sleep == nil {
		s.sleep = sleepCtx
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	return s
}

// Send отправляет сообщение с учётом лимитов.
func (s *Sender) Send(ctx context.Context, p SendMessageParams) (Message, error) {
	var m Message
	err := s.do(ctx, "sendMessage", p.ChatID, func(ctx context.Context) error {
		var err error
		m, err = s.api.SendMessage(ctx, p)
		return err
	})
	return m, err
}

// Edit меняет сообщение бота; правка — тоже сообщение в чат, лимиты те же.
func (s *Sender) Edit(ctx context.Context, p EditMessageTextParams) error {
	return s.do(ctx, "editMessageText", p.ChatID, func(ctx context.Context) error {
		return s.api.EditMessageText(ctx, p)
	})
}

// Answer отвечает на нажатие кнопки. Это не сообщение в чат — без очереди.
func (s *Sender) Answer(ctx context.Context, p AnswerCallbackQueryParams) error {
	return s.api.AnswerCallbackQuery(ctx, p)
}

func (s *Sender) do(ctx context.Context, method string, chatID int64, call func(context.Context) error) error {
	for attempt := 1; ; attempt++ {
		if err := s.wait(ctx, chatID); err != nil {
			return err
		}
		err := call(ctx)
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			return err
		}
		switch {
		case apiErr.Code == http.StatusTooManyRequests && attempt < maxAttempts:
			retry := apiErr.RetryAfter
			if retry <= 0 {
				retry = time.Second
			}
			s.logger.Warn("telegram: превышен лимит, повтор", "method", method, "retry_after", retry, "attempt", attempt)
			s.pause(retry)
		case apiErr.Code == http.StatusForbidden:
			if s.onBlocked != nil {
				s.onBlocked(ctx, chatID)
			}
			return fmt.Errorf("%w: %w", ErrBlocked, err)
		default:
			return err
		}
	}
}

// wait ждёт, пока отправка в chatID уложится в лимиты, и занимает место.
func (s *Sender) wait(ctx context.Context, chatID int64) error {
	for {
		s.mu.Lock()
		now := s.now()
		d := s.delay(now, chatID)
		if d <= 0 {
			s.record(now, chatID)
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		if err := s.sleep(ctx, d); err != nil {
			return err
		}
	}
}

func (s *Sender) delay(now time.Time, chatID int64) time.Duration {
	cut := 0
	for cut < len(s.window) && now.Sub(s.window[cut]) >= time.Second {
		cut++
	}
	s.window = s.window[cut:]

	d := s.pausedUntil.Sub(now)
	if len(s.window) >= GlobalPerSecond {
		d = max(d, s.window[0].Add(time.Second).Sub(now))
	}
	if last, ok := s.lastByChat[chatID]; ok {
		d = max(d, last.Add(PerChatInterval).Sub(now))
	}
	return d
}

func (s *Sender) record(now time.Time, chatID int64) {
	s.window = append(s.window, now)
	if len(s.lastByChat) >= staleChats {
		for id, t := range s.lastByChat {
			if now.Sub(t) >= PerChatInterval {
				delete(s.lastByChat, id)
			}
		}
	}
	s.lastByChat[chatID] = now
}

func (s *Sender) pause(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if until := s.now().Add(d); until.After(s.pausedUntil) {
		s.pausedUntil = until
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
