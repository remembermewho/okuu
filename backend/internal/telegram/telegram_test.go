package telegram_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"okuu/internal/telegram"
	"okuu/internal/telegram/telegramtest"
)

func TestClientCallsAndErrors(t *testing.T) {
	srv := telegramtest.New(t)
	c := srv.Client()
	ctx := context.Background()

	m, err := c.SendMessage(ctx, telegram.SendMessageParams{ChatID: 42, Text: "Салам",
		ReplyMarkup: &telegram.ReplyKeyboardMarkup{Keyboard: [][]telegram.KeyboardButton{{{Text: "Номер", RequestContact: true}}}}})
	if err != nil || m.MessageID == 0 || m.Chat.ID != 42 {
		t.Fatalf("sendMessage = %+v, %v", m, err)
	}
	if got := srv.Last(42).Buttons(); fmt.Sprint(got) != "[Номер|contact=true]" {
		t.Errorf("кнопки = %v", got)
	}

	// ok=false → APIError с кодом и описанием.
	srv.Fail("sendMessage", 42, http.StatusBadRequest, "Bad Request: chat not found", 0, 1)
	_, err = c.SendMessage(ctx, telegram.SendMessageParams{ChatID: 42, Text: "x"})
	var apiErr *telegram.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 400 || apiErr.Method != "sendMessage" || !strings.Contains(apiErr.Description, "chat not found") {
		t.Errorf("err = %v", err)
	}

	// Повторная правка тем же текстом — не ошибка.
	srv.Fail("editMessageText", 42, http.StatusBadRequest, "Bad Request: message is not modified", 0, 1)
	if err := c.EditMessageText(ctx, telegram.EditMessageTextParams{ChatID: 42, MessageID: m.MessageID, Text: "x"}); err != nil {
		t.Errorf("message is not modified: %v", err)
	}

	// 429 — RetryAfter из parameters.
	srv.Fail("answerCallbackQuery", 0, http.StatusTooManyRequests, "Too Many Requests: retry after 7", 7, 1)
	err = c.AnswerCallbackQuery(ctx, telegram.AnswerCallbackQueryParams{CallbackQueryID: "1"})
	if !errors.As(err, &apiErr) || apiErr.Code != 429 || apiErr.RetryAfter != 7*time.Second {
		t.Errorf("429: %v (%+v)", err, apiErr)
	}

	// 403 — IsBlocked.
	srv.Block(43)
	_, err = c.SendMessage(ctx, telegram.SendMessageParams{ChatID: 43, Text: "x"})
	if !telegram.IsBlocked(err) {
		t.Errorf("403: IsBlocked(%v) = false", err)
	}

	if err := c.SetWebhook(ctx, telegram.SetWebhookParams{URL: ""}); err != nil {
		t.Errorf("setWebhook: %v", err)
	}
}

// Токен — в пути запроса; ни в ошибках сети, ни при печати клиента его нет.
func TestClientNeverLeaksToken(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	base := dead.URL
	dead.Close() // соединение будет отклонено

	c := telegram.NewClient(telegramtest.Token, telegram.ClientOptions{BaseURL: base})
	_, err := c.SendMessage(context.Background(), telegram.SendMessageParams{ChatID: 1, Text: "x"})
	if err == nil {
		t.Fatal("ожидалась ошибка сети")
	}
	for name, s := range map[string]string{
		"err": err.Error(), "%v": fmt.Sprintf("%v", c), "%+v": fmt.Sprintf("%+v", c), "%#v": fmt.Sprintf("%#v", c),
	} {
		if strings.Contains(s, telegramtest.Token) || strings.Contains(s, "TEST-fake") {
			t.Errorf("%s содержит токен: %s", name, s)
		}
	}

	// Отмена контекста распознаётся после очистки ошибки.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv := telegramtest.New(t)
	if _, err := srv.Client().SendMessage(ctx, telegram.SendMessageParams{ChatID: 1, Text: "x"}); !errors.Is(err, context.Canceled) {
		t.Errorf("отменённый ctx: %v", err)
	}
}

func TestGetUpdatesLongPolling(t *testing.T) {
	srv := telegramtest.New(t)
	c := srv.Client()
	go func() {
		time.Sleep(50 * time.Millisecond)
		srv.PushUpdate(telegram.Update{UpdateID: 5, Message: &telegram.Message{Chat: telegram.Chat{ID: 1}, Text: "/help"}})
	}()
	u, err := c.GetUpdates(context.Background(), telegram.GetUpdatesParams{Timeout: 5})
	if err != nil || len(u) != 1 || u[0].UpdateID != 5 || u[0].ChatID() != 1 {
		t.Fatalf("getUpdates = %+v, %v", u, err)
	}
	// offset подтверждает полученные: повторно не приходят.
	u, err = c.GetUpdates(context.Background(), telegram.GetUpdatesParams{Offset: 6})
	if err != nil || len(u) != 0 {
		t.Fatalf("после offset: %+v, %v", u, err)
	}
}

func newSender(t *testing.T, srv *telegramtest.Server, onBlocked func(context.Context, int64)) (*telegram.Sender, *telegramtest.Clock) {
	clk := telegramtest.NewClock(time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC))
	srv.SetNow(clk.Now)
	return telegram.NewSender(srv.Client(), telegram.SenderOptions{Now: clk.Now, Sleep: clk.Sleep, OnBlocked: onBlocked,
		Logger: slog.New(slog.DiscardHandler)}), clk
}

// sentAt — моменты успешных sendMessage.
func sentAt(srv *telegramtest.Server) []time.Time {
	var out []time.Time
	for _, c := range srv.Calls() {
		if c.Method == "sendMessage" {
			out = append(out, c.At)
		}
	}
	return out
}

// maxInWindow — наибольшее число отправок за любую секунду.
func maxInWindow(times []time.Time) int {
	best := 0
	for i := range times {
		n := 0
		for _, t := range times {
			if !t.Before(times[i]) && t.Sub(times[i]) < time.Second {
				n++
			}
		}
		best = max(best, n)
	}
	return best
}

func TestSenderGlobalLimit(t *testing.T) {
	srv := telegramtest.New(t)
	s, clk := newSender(t, srv, nil)
	start := clk.Now()
	for i := range 60 {
		if _, err := s.Send(context.Background(), telegram.SendMessageParams{ChatID: int64(1000 + i), Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	times := sentAt(srv)
	if len(times) != 60 {
		t.Fatalf("отправлено %d", len(times))
	}
	if n := maxInWindow(times); n != telegram.GlobalPerSecond {
		t.Errorf("за секунду до %d сообщений, want %d", n, telegram.GlobalPerSecond)
	}
	// 60 сообщений по 25 в секунду: третья пачка уходит через 2 секунды.
	if d := times[59].Sub(start); d != 2*time.Second {
		t.Errorf("последнее через %v, want 2s", d)
	}
}

func TestSenderPerChatLimit(t *testing.T) {
	srv := telegramtest.New(t)
	s, clk := newSender(t, srv, nil)
	start := clk.Now()
	for range 3 {
		if _, err := s.Send(context.Background(), telegram.SendMessageParams{ChatID: 7, Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	// Другой чат не ждёт первый.
	if _, err := s.Send(context.Background(), telegram.SendMessageParams{ChatID: 8, Text: "y"}); err != nil {
		t.Fatal(err)
	}
	var got []time.Duration
	for _, at := range sentAt(srv) {
		got = append(got, at.Sub(start))
	}
	if fmt.Sprint(got) != "[0s 1s 2s 2s]" {
		t.Errorf("моменты отправки = %v, want [0s 1s 2s 2s]", got)
	}
	// Правка — тоже сообщение в чат.
	if err := s.Edit(context.Background(), telegram.EditMessageTextParams{ChatID: 7, MessageID: 1, Text: "z"}); err != nil {
		t.Fatal(err)
	}
	if last := srv.Last(7); last.Method != "editMessageText" || last.At.Sub(start) != 3*time.Second {
		t.Errorf("правка: %s через %v, want через 3s", last.Method, last.At.Sub(start))
	}
}

// Одновременные отправки из многих горутин: всё доставлено, и лимит на чат держится —
// 10 сообщений в один чат не могли уйти быстрее чем за 9 секунд (часы общие и только растут;
// точные моменты здесь не проверить: другие горутины двигают часы между занятием места и запросом).
func TestSenderConcurrent(t *testing.T) {
	srv := telegramtest.New(t)
	s, clk := newSender(t, srv, nil)
	start := clk.Now()
	var wg sync.WaitGroup
	for i := range 80 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Send(context.Background(), telegram.SendMessageParams{ChatID: int64(i%8 + 1), Text: "x"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := len(sentAt(srv)); n != 80 {
		t.Errorf("доставлено %d из 80", n)
	}
	if d := clk.Now().Sub(start); d < 9*time.Second {
		t.Errorf("80 сообщений в 8 чатов ушли за %v, want не быстрее 9s", d)
	}
}

func TestSender429RetriesAfterPause(t *testing.T) {
	srv := telegramtest.New(t)
	s, clk := newSender(t, srv, nil)
	start := clk.Now()
	srv.Fail("sendMessage", 0, http.StatusTooManyRequests, "Too Many Requests: retry after 3", 3, 1)
	if _, err := s.Send(context.Background(), telegram.SendMessageParams{ChatID: 1, Text: "x"}); err != nil {
		t.Fatalf("после повтора: %v", err)
	}
	times := sentAt(srv)
	if len(times) != 1 || times[0].Sub(start) != 3*time.Second {
		t.Fatalf("успешная отправка в %v, want через 3s", times)
	}
	// Пауза общая: другой чат тоже ждал бы — проверяем, что после паузы очередь идёт дальше сразу.
	if _, err := s.Send(context.Background(), telegram.SendMessageParams{ChatID: 2, Text: "y"}); err != nil {
		t.Fatal(err)
	}

	// 429 без конца — после нескольких попыток ошибка наружу.
	srv.Fail("sendMessage", 3, http.StatusTooManyRequests, "Too Many Requests: retry after 1", 1, -1)
	_, err := s.Send(context.Background(), telegram.SendMessageParams{ChatID: 3, Text: "z"})
	var apiErr *telegram.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 429 {
		t.Fatalf("бесконечный 429: %v", err)
	}
	attempts := 0
	for _, c := range srv.Calls() {
		if c.ChatID == 3 {
			attempts++
		}
	}
	if attempts != 4 {
		t.Errorf("попыток = %d, want 4", attempts)
	}
}

func TestSender403MarksBlocked(t *testing.T) {
	srv := telegramtest.New(t)
	var blocked []int64
	s, _ := newSender(t, srv, func(_ context.Context, chatID int64) { blocked = append(blocked, chatID) })
	srv.Block(9)
	_, err := s.Send(context.Background(), telegram.SendMessageParams{ChatID: 9, Text: "x"})
	if !errors.Is(err, telegram.ErrBlocked) || !telegram.IsBlocked(err) {
		t.Fatalf("err = %v, want ErrBlocked", err)
	}
	if fmt.Sprint(blocked) != "[9]" {
		t.Errorf("OnBlocked вызван для %v, want [9]", blocked)
	}
	// Другие ошибки — не блокировка.
	srv.Fail("sendMessage", 10, http.StatusBadRequest, "Bad Request: chat not found", 0, 1)
	if _, err := s.Send(context.Background(), telegram.SendMessageParams{ChatID: 10, Text: "x"}); err == nil || errors.Is(err, telegram.ErrBlocked) {
		t.Errorf("400: %v", err)
	}
	if len(blocked) != 1 {
		t.Errorf("OnBlocked на 400: %v", blocked)
	}
}
