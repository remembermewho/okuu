package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"okuu/internal/alerts"
	"okuu/internal/httpx"
	"okuu/internal/importer"
)

type ctxKey int

const requestIDKey ctxKey = iota

// RequestIDHeader — id запроса: принимаем от прокси (если он похож на id) или создаём; отдаём в ответе.
const RequestIDHeader = "X-Request-ID"

// requestIDFrom — id запроса из контекста ("" — вне HTTP).
func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID(id) {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// validRequestID — чужой id берём, только если он короткий и из безопасных символов (он идёт в логи).
func validRequestID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// statusWriter запоминает код ответа для журнала запросов.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status, w.wrote = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status, w.wrote = http.StatusOK, true
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// routeOf — шаблон маршрута (/api/v1/pay/{token}), а не путь: в путях бывают токены и секреты.
func routeOf(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return "-" // маршрут не найден
}

// accessLog — одна строка на запрос: request_id, метод, шаблон маршрута, статус, длительность.
// Ни пути, ни query (там телефоны в ?q=), ни заголовков, ни тела.
func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			level := slog.LevelInfo
			if sw.status >= 500 {
				level = slog.LevelError
			}
			logger.LogAttrs(r.Context(), level, "http",
				slog.String("request_id", requestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("route", routeOf(r)),
				slog.Int("status", sw.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}

// recoverer — panic в обработчике: 500 в формате API, стек в лог, алерт команде.
func recoverer(logger *slog.Logger, a *alerts.Alerter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v) // штатный обрыв соединения — как в net/http
				}
				route := routeOf(r)
				logger.Error("panic в обработчике", "request_id", requestIDFrom(r.Context()), "method", r.Method,
					"route", route, "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				a.Alert("panic:"+r.Method+" "+route, fmt.Sprintf("panic в %s %s: %v (request_id %s)", r.Method, route, v, requestIDFrom(r.Context())))
				httpx.Error(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// securityHeaders — для всех ответов nosniff и no-referrer; ответы API не кэшируются
// (там персональные данные и деньги).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// Лимиты тела запроса: 1 МБ, для импорта — файл 5 МБ плюс запас на multipart.
const (
	maxBody       = httpx.MaxBodyBytes
	maxImportBody = importer.MaxFileBytes + 1<<20
)

// bodyLimit ограничивает тело любого запроса; обработчик получит ошибку чтения
// *http.MaxBytesError и ответит 400/413.
func bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxBody)
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/import/preview") {
			limit = maxImportBody
		}
		if r.ContentLength > limit {
			httpx.Error(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("Тело запроса больше %d МБ", limit>>20))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}
