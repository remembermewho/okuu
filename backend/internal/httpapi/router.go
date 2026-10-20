// Package httpapi — HTTP-роутер (chi), хендлеры и DTO.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Pinger — то, что нужно /healthz от базы (реализует *pgxpool.Pool).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps — зависимости роутера.
type Deps struct {
	DB     Pinger
	Logger *slog.Logger
}

// NewRouter собирает все маршруты приложения.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Не найдено")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Метод не поддерживается")
	})

	r.Get("/healthz", healthz(d))

	return r
}

func healthz(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.Ping(ctx); err != nil {
			if d.Logger != nil {
				d.Logger.Error("healthz: БД недоступна", "err", err)
			}
			writeError(w, http.StatusServiceUnavailable, "db_unavailable", "База данных недоступна")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
