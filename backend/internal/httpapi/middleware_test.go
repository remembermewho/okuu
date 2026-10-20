package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"okuu/internal/alerts"
	"okuu/internal/telegram"
)

type recordSender struct {
	mu    sync.Mutex
	texts []string
}

func (r *recordSender) Send(_ context.Context, p telegram.SendMessageParams) (telegram.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texts = append(r.texts, p.Text)
	return telegram.Message{}, nil
}

// panic в обработчике: 500 в формате API, строка журнала со статусом 500, стек в логе, алерт.
func TestRecovererAlertsOnPanic(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	sender := &recordSender{}
	a := alerts.New(alerts.Options{Sender: sender, ChatID: -1, Env: "test", Logger: logger})

	r := chi.NewRouter()
	r.Use(requestID, accessLog(logger), recoverer(logger, a))
	r.Get("/api/v1/boom/{token}", func(http.ResponseWriter, *http.Request) { panic("всё сломалось") })

	for range 3 { // одинаковые panic — один алерт за 10 минут
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/boom/secret-token-123", nil))
		if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"internal"`) {
			t.Fatalf("ответ: %d %s", rec.Code, rec.Body)
		}
	}
	a.Wait()
	if len(sender.texts) != 1 || !strings.Contains(sender.texts[0], "panic в GET /api/v1/boom/{token}: всё сломалось") {
		t.Errorf("алерты: %q", sender.texts)
	}
	out := logs.String()
	if !strings.Contains(out, `"status":500`) || !strings.Contains(out, `"stack"`) || strings.Contains(out, "secret-token-123") {
		t.Errorf("лог:\n%s", out)
	}
}
