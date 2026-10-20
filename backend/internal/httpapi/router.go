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
	"okuu/internal/groups"
	"okuu/internal/httpx"
	"okuu/internal/staff"
	"okuu/internal/tariffs"
)

// Pinger — то, что нужно /healthz от базы (реализует *pgxpool.Pool).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps — зависимости роутера.
type Deps struct {
	DB        Pinger
	Auth      *auth.Service
	Staff     *staff.Service
	Tariffs   *tariffs.Service
	Groups    *groups.Service
	AppOrigin string // APP_ORIGIN: проверка Origin (CSRF) и адрес фронтенда в ссылках-приглашениях
	Logger    *slog.Logger
}

type handlers struct {
	auth      *auth.Service
	staff     *staff.Service
	tariffs   *tariffs.Service
	groups    *groups.Service
	appOrigin string
	logger    *slog.Logger
}

// NewRouter собирает все маршруты приложения.
func NewRouter(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	h := &handlers{
		auth: d.Auth, staff: d.Staff, tariffs: d.Tariffs, groups: d.Groups,
		appOrigin: d.AppOrigin, logger: d.Logger,
	}

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
		r.Use(auth.RequireOrigin(d.AppOrigin))

		r.Post("/auth/register", h.register)
		r.Post("/auth/login", h.login)
		r.Post("/auth/logout", h.logout)

		// Публичные: страница приглашения и его принятие (вход необязателен).
		r.Get("/invites/{token}", h.getInvite)
		r.Post("/invites/{token}/accept", h.acceptInvite)

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.RequireAuth)
			r.Get("/me", h.me)

			// Данные центра: членство и роль проверяет RequireCenter, center_id берётся из него.
			r.Route("/c/{centerId}", func(r chi.Router) {
				ownerOrAdmin := d.Auth.RequireCenter(auth.RoleOwner, auth.RoleAdmin)
				ownerOnly := d.Auth.RequireCenter(auth.RoleOwner)

				anyStaff := d.Auth.RequireCenter(auth.RoleOwner, auth.RoleAdmin, auth.RoleTeacher)

				r.With(ownerOrAdmin).Get("/staff", h.listStaff)
				r.Group(func(r chi.Router) {
					r.Use(ownerOnly)
					r.Post("/staff/invites", h.createInvite)
					r.Get("/staff/invites", h.listInvites)
					r.Delete("/staff/invites/{inviteId}", h.revokeInvite)
					r.Patch("/staff/{userId}", h.changeStaffRole)
					r.Delete("/staff/{userId}", h.removeStaff)
				})

				r.Group(func(r chi.Router) {
					r.Use(ownerOrAdmin)
					r.Get("/tariffs", h.listTariffs)
					r.Post("/tariffs", h.createTariff)
					r.Patch("/tariffs/{tariffId}", h.patchTariff)
					r.Delete("/tariffs/{tariffId}", h.archiveTariff)
				})

				// Группы читают все сотрудники (преподаватель — только свои), меняют owner и admin.
				r.With(anyStaff).Get("/groups", h.listGroups)
				r.With(anyStaff).Get("/groups/{groupId}", h.getGroup)
				r.Group(func(r chi.Router) {
					r.Use(ownerOrAdmin)
					r.Post("/groups", h.createGroup)
					r.Patch("/groups/{groupId}", h.patchGroup)
					r.Delete("/groups/{groupId}", h.archiveGroup)
				})
			})
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
