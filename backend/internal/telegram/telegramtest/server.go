// Package telegramtest — фейковый Telegram Bot API на httptest для тестов. Реальный бот
// в тестах не используется; токен здесь — заведомо фейковый.
package telegramtest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"okuu/internal/telegram"
)

// Token — фейковый токен бота для тестов.
const Token = "123456:TEST-fake-token"

// Call — один запрос к фейковому API.
type Call struct {
	Method    string
	ChatID    int64
	MessageID int64 // для editMessageText — какое сообщение правят; для sendMessage — выданный id
	Text      string
	Markup    json.RawMessage
	Raw       json.RawMessage
	At        time.Time
}

// Buttons — тексты кнопок клавиатуры (inline или reply) построчно через «|».
func (c Call) Buttons() []string {
	var m struct {
		Inline [][]struct {
			Text string `json:"text"`
			Data string `json:"callback_data"`
		} `json:"inline_keyboard"`
		Keyboard [][]struct {
			Text           string `json:"text"`
			RequestContact bool   `json:"request_contact"`
		} `json:"keyboard"`
	}
	_ = json.Unmarshal(c.Markup, &m)
	var out []string
	for _, row := range m.Inline {
		for _, b := range row {
			out = append(out, b.Text+"|"+b.Data)
		}
	}
	for _, row := range m.Keyboard {
		for _, b := range row {
			out = append(out, fmt.Sprintf("%s|contact=%v", b.Text, b.RequestContact))
		}
	}
	return out
}

// failure — заготовленный ответ-ошибка.
type failure struct {
	method     string
	chatID     int64 // 0 — любой чат
	code       int
	desc       string
	retryAfter int
	times      int // -1 — всегда
}

// Server — фейковый Bot API.
type Server struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	now      func() time.Time
	calls    []Call
	failures []*failure
	nextMsg  int64
	updates  []telegram.Update
	notify   chan struct{}
}

// New запускает фейковый сервер; закрывается в t.Cleanup.
func New(t *testing.T) *Server {
	s := &Server{t: t, now: time.Now, nextMsg: 100, notify: make(chan struct{}, 1)}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

// URL — базовый адрес для telegram.ClientOptions.BaseURL.
func (s *Server) URL() string { return s.srv.URL }

// Client — настоящий telegram.Client, направленный на фейковый сервер.
func (s *Server) Client() *telegram.Client {
	return telegram.NewClient(Token, telegram.ClientOptions{BaseURL: s.URL()})
}

// SetNow — часы, по которым записывается время вызовов (по умолчанию time.Now).
func (s *Server) SetNow(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// Fail — следующие times вызовов method в chatID (0 — в любой чат) вернут ошибку code.
// times = -1 — всегда.
func (s *Server) Fail(method string, chatID int64, code int, desc string, retryAfter, times int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = append(s.failures, &failure{method: method, chatID: chatID, code: code, desc: desc, retryAfter: retryAfter, times: times})
}

// Block — пользователь чата заблокировал бота: sendMessage и editMessageText отвечают 403.
func (s *Server) Block(chatID int64) {
	s.Fail("sendMessage", chatID, http.StatusForbidden, "Forbidden: bot was blocked by the user", 0, -1)
	s.Fail("editMessageText", chatID, http.StatusForbidden, "Forbidden: bot was blocked by the user", 0, -1)
}

// Unblock — пользователь разблокировал бота: заготовленные ошибки для чата снимаются.
func (s *Server) Unblock(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.failures[:0]
	for _, f := range s.failures {
		if f.chatID != chatID {
			kept = append(kept, f)
		}
	}
	s.failures = kept
}

// PushUpdate кладёт апдейт в очередь getUpdates.
func (s *Server) PushUpdate(u telegram.Update) {
	s.mu.Lock()
	s.updates = append(s.updates, u)
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// Calls — все вызовы (копия).
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Messages — успешные sendMessage и editMessageText в чат по порядку.
func (s *Server) Messages(chatID int64) []Call {
	var out []Call
	for _, c := range s.Calls() {
		if c.ChatID == chatID && (c.Method == "sendMessage" || c.Method == "editMessageText") {
			out = append(out, c)
		}
	}
	return out
}

// Last — последнее сообщение в чат (тест падает, если сообщений нет).
func (s *Server) Last(chatID int64) Call {
	s.t.Helper()
	m := s.Messages(chatID)
	if len(m) == 0 {
		s.t.Fatalf("telegramtest: в чат %d ничего не отправлено", chatID)
	}
	return m[len(m)-1]
}

// Reset забывает записанные вызовы (заготовленные ошибки остаются).
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
}

type request struct {
	ChatID    int64           `json:"chat_id"`
	MessageID int64           `json:"message_id"`
	Text      string          `json:"text"`
	Markup    json.RawMessage `json:"reply_markup"`
	Offset    int64           `json:"offset"`
	Timeout   int             `json:"timeout"`
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	method, ok := strings.CutPrefix(r.URL.Path, "/bot"+Token+"/")
	if !ok || r.Method != http.MethodPost {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error_code": 404, "description": "Not Found"})
		return
	}
	var raw json.RawMessage
	var req request
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil || json.Unmarshal(raw, &req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error_code": 400, "description": "Bad Request: invalid JSON"})
		return
	}

	if method == "getUpdates" {
		s.getUpdates(w, r, req)
		return
	}

	s.mu.Lock()
	call := Call{Method: method, ChatID: req.ChatID, MessageID: req.MessageID, Text: req.Text, Markup: req.Markup, Raw: raw, At: s.now()}
	if f := s.takeFailure(method, req.ChatID); f != nil {
		call.Method = method + ":error" // Messages() такие вызовы не считает
		s.calls = append(s.calls, call)
		s.mu.Unlock()
		resp := map[string]any{"ok": false, "error_code": f.code, "description": f.desc}
		if f.retryAfter > 0 {
			resp["parameters"] = map[string]any{"retry_after": f.retryAfter}
		}
		writeJSON(w, f.code, resp)
		return
	}
	var result any = true
	switch method {
	case "sendMessage":
		if req.ChatID == 0 || req.Text == "" {
			s.mu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error_code": 400, "description": "Bad Request: chat_id and text are required"})
			return
		}
		s.nextMsg++
		call.MessageID = s.nextMsg
		result = map[string]any{"message_id": s.nextMsg, "chat": map[string]any{"id": req.ChatID, "type": "private"}, "date": call.At.Unix(), "text": req.Text}
	case "editMessageText":
		if req.ChatID == 0 || req.MessageID == 0 || req.Text == "" {
			s.mu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error_code": 400, "description": "Bad Request: message to edit not found"})
			return
		}
	case "answerCallbackQuery", "setWebhook":
	default:
		s.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error_code": 404, "description": "Not Found: method " + method})
		return
	}
	s.calls = append(s.calls, call)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (s *Server) takeFailure(method string, chatID int64) *failure {
	for i, f := range s.failures {
		if f.method != method || (f.chatID != 0 && f.chatID != chatID) {
			continue
		}
		if f.times > 0 {
			f.times--
			if f.times == 0 {
				s.failures = append(s.failures[:i], s.failures[i+1:]...)
			}
		}
		return f
	}
	return nil
}

// getUpdates отдаёт апдейты с update_id >= offset; если их нет — ждёт до timeout секунд.
func (s *Server) getUpdates(w http.ResponseWriter, r *http.Request, req request) {
	deadline := time.After(time.Duration(req.Timeout) * time.Second)
	for {
		s.mu.Lock()
		var out []telegram.Update
		kept := s.updates[:0]
		for _, u := range s.updates {
			if u.UpdateID >= req.Offset {
				out = append(out, u)
				kept = append(kept, u)
			}
		}
		s.updates = kept
		s.calls = append(s.calls, Call{Method: "getUpdates", At: s.now()})
		s.mu.Unlock()
		if len(out) > 0 || req.Timeout == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": out})
			return
		}
		select {
		case <-s.notify:
		case <-deadline:
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": []telegram.Update{}})
			return
		case <-r.Context().Done():
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Clock — управляемые часы для очереди отправки: Sleep не ждёт, а сдвигает время.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

// NewClock — часы, стоящие на t.
func NewClock(t time.Time) *Clock { return &Clock{t: t} }

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// Sleep для telegram.SenderOptions: сдвигает часы на d.
func (c *Clock) Sleep(ctx context.Context, d time.Duration) error {
	c.Advance(d)
	return ctx.Err()
}
