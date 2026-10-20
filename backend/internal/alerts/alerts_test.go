package alerts

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"okuu/internal/telegram"
)

type fakeSender struct {
	mu   sync.Mutex
	sent []telegram.SendMessageParams
}

func (f *fakeSender) Send(_ context.Context, p telegram.SendMessageParams) (telegram.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, p)
	return telegram.Message{}, nil
}

func (f *fakeSender) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, p := range f.sent {
		out = append(out, p.Text)
	}
	return out
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestAlertDedupWindow(t *testing.T) {
	s := &fakeSender{}
	clk := &clock{t: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)}
	a := New(Options{Sender: s, ChatID: -42, Env: "prod", Logger: slog.New(slog.DiscardHandler), Now: clk.now})

	a.Alert("job:billing.monthly", "billing упал")
	a.Alert("job:billing.monthly", "billing упал снова") // через 0 мин — склеивается
	clk.t = clk.t.Add(9 * time.Minute)
	a.Alert("job:billing.monthly", "billing упал в третий раз") // через 9 мин — тоже
	a.Alert("job:reminders.auto", "другой ключ — отдельно")
	clk.t = clk.t.Add(time.Minute) // ровно 10 минут после первого — снова можно
	a.Alert("job:billing.monthly", "billing всё ещё падает")
	a.Wait()

	// Отправка асинхронная — порядок в чате не важен, важен набор.
	got := map[string]bool{}
	for _, txt := range s.texts() {
		got[txt] = true
	}
	want := []string{
		"⚠️ Окуу (prod): billing упал",
		"⚠️ Окуу (prod): другой ключ — отдельно",
		"⚠️ Окуу (prod): billing всё ещё падает\n\n(ещё таких же за 10 мин: 2 — не отправлялись)",
	}
	if len(got) != len(want) {
		t.Fatalf("отправлено %d: %q", len(got), s.texts())
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("нет алерта %q среди %q", w, s.texts())
		}
	}
	for _, p := range s.sent {
		if p.ChatID != -42 {
			t.Errorf("чат %d", p.ChatID)
		}
	}
}

func TestAlertWithoutChatOnlyLogs(t *testing.T) {
	var logs strings.Builder
	s := &fakeSender{}
	a := New(Options{Sender: s, Logger: slog.New(slog.NewTextHandler(&logs, nil))}) // ChatID пустой
	a.Alert("k", "только в лог")
	a.Wait()
	if len(s.texts()) != 0 || !strings.Contains(logs.String(), "только в лог") {
		t.Errorf("отправлено %v, лог %q", s.texts(), logs.String())
	}
	var nilAlerter *Alerter
	nilAlerter.Alert("k", "nil — без паники")
	nilAlerter.Wait()
}

func TestAlertTruncatesLongText(t *testing.T) {
	s := &fakeSender{}
	a := New(Options{Sender: s, ChatID: 1, Logger: slog.New(slog.DiscardHandler)})
	a.Alert("k", strings.Repeat("я", 5000))
	a.Wait()
	if got := s.texts(); len(got) != 1 || len(got[0]) > maxText+len("…") {
		t.Errorf("длина %d", len(got[0]))
	}
}
