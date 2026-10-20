package jobs

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"okuu/internal/alerts"
	"okuu/internal/payments"
	"okuu/internal/telegram"
	"okuu/internal/testdb"
)

type fakeSender struct {
	mu    sync.Mutex
	texts []string
}

func (f *fakeSender) Send(_ context.Context, p telegram.SendMessageParams) (telegram.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, p.Text)
	return telegram.Message{}, nil
}

// Ошибка фоновой задачи — алерт команде.
func TestJobFailureAlerts(t *testing.T) {
	pool := testdb.New(t)
	svc := payments.NewService(payments.Options{Pool: pool, Logger: slog.New(slog.DiscardHandler)})
	pool.Close() // база недоступна — запуск упадёт

	s := &fakeSender{}
	a := alerts.New(alerts.Options{Sender: s, ChatID: -1, Env: "test", Logger: slog.New(slog.DiscardHandler)})
	stop, cancel := context.WithCancel(context.Background())
	cancel() // один запуск и выход
	RunReconcile(stop, context.Background(), pool, slog.New(slog.DiscardHandler), a, svc, time.Hour)
	a.Wait()
	if len(s.texts) != 1 || !strings.Contains(s.texts[0], "фоновая задача payments.reconcile не выполнилась") {
		t.Errorf("алерты: %q", s.texts)
	}
}

// Остановка: планировщик заканчивает текущий запуск и выходит; Started — для /readyz.
func TestSchedulerStartedAndStops(t *testing.T) {
	pool := testdb.New(t)
	sch := &Scheduler{Pool: pool, Logger: slog.New(slog.DiscardHandler), Interval: time.Hour}
	if sch.Started() {
		t.Fatal("до запуска Started = true")
	}
	stop, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sch.Run(stop, context.Background())
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !sch.Started() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !sch.Started() {
		t.Fatal("планировщик не запустился")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("планировщик не остановился")
	}
	if sch.Started() {
		t.Error("после остановки Started = true")
	}
}
