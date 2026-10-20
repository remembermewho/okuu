// Package httpapi — HTTP-роутер (chi), хендлеры и DTO.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"okuu/internal/auth"
	"okuu/internal/httpx"
)

// Pinger — то, что нужно /healthz от базы (реализует *pgxpool.Pool).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps — зависимости роутера.
type Deps struct {
	DB            Pinger
	Auth          *auth.Service
	AllowedOrigin string // APP_ORIGIN
	Logger        *slog.Logger
}

type handlers struct {
	auth   *auth.Service
	logger *slog.Logger
}

// NewRouter собирает все маршруты приложения.
func NewRouter(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	h := &handlers{auth: d.Auth, logger: d.Logger}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, http.StatusNotFound, "not_found", "Не найдено")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, http.StatusMethodNotAllowed, "method_not_allowed", "Метод не поддерживается")
	})

	r.Get("/healthz", healthz(d))

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.RequireOrigin(d.AllowedOrigin))

		r.Post("/auth/register", h.register)
		r.Post("/auth/login", h.login)
		r.Post("/auth/logout", h.logout)

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.RequireAuth)
			r.Get("/me", h.me)
			// /c/{centerId}/... — шаги 5–7, через d.Auth.RequireCenter(roles...).
		})
	})

	return r
}

func healthz(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.Ping(ctx); err != nil {
			d.Logger.Error("healthz: БД недоступна", "err", err)
			httpx.Error(w, http.StatusServiceUnavailable, "db_unavailable", "База данных недоступна")
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (h *handlers) internalError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("ошибка обработки запроса", "method", r.Method, "path", r.URL.Path,
		"request_id", middleware.GetReqID(r.Context()), "err", err)
	httpx.Error(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
}
