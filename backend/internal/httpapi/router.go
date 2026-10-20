// Package httpapi — HTTP-роутер (chi), хендлеры и DTO.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/attendance"
	"okuu/internal/auth"
	"okuu/internal/billing"
	"okuu/internal/groups"
	"okuu/internal/httpx"
	"okuu/internal/importer"
	"okuu/internal/parents"
	"okuu/internal/payments"
	"okuu/internal/reminders"
	"okuu/internal/reports"
	"okuu/internal/settings"
	"okuu/internal/staff"
	"okuu/internal/students"
	"okuu/internal/tariffs"
)

// Pinger — то, что нужно /healthz от базы (реализует *pgxpool.Pool).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps — зависимости роутера.
type Deps struct {
	DB         Pinger
	Auth       *auth.Service
	Staff      *staff.Service
	Tariffs    *tariffs.Service
	Groups     *groups.Service
	Students   *students.Service
	Billing    *billing.Service
	Attendance *attendance.Service
	Importer   *importer.Service
	Settings   *settings.Service
	Reports    *reports.Service
	Parents    *parents.Service
	Reminders  *reminders.Service
	Payments   *payments.Service // страница оплаты; вебхук — если есть онлайн-провайдер
	PayLimiter *auth.Limiter     // лимит /pay по IP; nil — 30 запросов в минуту
	Pool       *pgxpool.Pool     // чтение журнала действий
	AppOrigin  string            // APP_ORIGIN: проверка Origin (CSRF) и адрес фронтенда в ссылках-приглашениях
	Logger     *slog.Logger

	// Telegram-вебхук: маршрут есть, только если заданы оба (режим webhook).
	Bot                   UpdateQueue
	TelegramWebhookSecret string
}

type handlers struct {
	auth       *auth.Service
	staff      *staff.Service
	tariffs    *tariffs.Service
	groups     *groups.Service
	students   *students.Service
	billing    *billing.Service
	attendance *attendance.Service
	importer   *importer.Service
	settings   *settings.Service
	reports    *reports.Service
	parents    *parents.Service
	reminders  *reminders.Service
	payments   *payments.Service
	payLimiter *auth.Limiter
	pool       *pgxpool.Pool
	appOrigin  string
	logger     *slog.Logger

	bot             UpdateQueue
	tgWebhookSecret string
}

// NewRouter собирает все маршруты приложения.
func NewRouter(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.PayLimiter == nil {
		d.PayLimiter = auth.NewLimiter(payRateLimit, payRateWindow, nil)
	}
	h := &handlers{
		auth: d.Auth, staff: d.Staff, tariffs: d.Tariffs, groups: d.Groups, students: d.Students, billing: d.Billing,
		attendance: d.Attendance, importer: d.Importer, settings: d.Settings, reports: d.Reports, parents: d.Parents, reminders: d.Reminders, payments: d.Payments, payLimiter: d.PayLimiter,
		pool: d.Pool, appOrigin: d.AppOrigin, logger: d.Logger, bot: d.Bot, tgWebhookSecret: d.TelegramWebhookSecret,
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

	// Вебхук Telegram — вне /api/v1: без cookie и проверки Origin, доступ по секрету.
	if d.Bot != nil && d.TelegramWebhookSecret != "" {
		r.Post("/webhooks/telegram/{secret}", h.telegramWebhook)
	}
	// Вебхук платёжного провайдера (Finik) — только если онлайн-провайдер включён; доступ — по подписи.
	if d.Payments != nil && d.Payments.Online() != nil {
		r.Post("/webhooks/"+d.Payments.Online().Name(), h.providerWebhook)
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.RequireOrigin(d.AppOrigin))

		r.Post("/auth/register", h.register)
		r.Post("/auth/login", h.login)
		r.Post("/auth/logout", h.logout)

		// Публичные: страница приглашения и его принятие (вход необязателен).
		r.Get("/invites/{token}", h.getInvite)
		r.Post("/invites/{token}/accept", h.acceptInvite)

		// Публичная страница оплаты родителя (B2): по токену из ссылки, без входа.
		if d.Payments != nil {
			r.Get("/pay/{token}", h.getPayLink)
			r.Post("/pay/{token}/start", h.startPayment)
		}

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.RequireAuth)
			r.Get("/me", h.me)

			// Данные центра: членство и роль проверяет RequireCenter, center_id берётся из него.
			r.Route("/c/{centerId}", func(r chi.Router) {
				ownerOrAdmin := d.Auth.RequireCenter(auth.RoleOwner, auth.RoleAdmin)
				ownerOnly := d.Auth.RequireCenter(auth.RoleOwner)

				anyStaff := d.Auth.RequireCenter(auth.RoleOwner, auth.RoleAdmin, auth.RoleTeacher)

				// Настройки центра (S8): читают owner и admin, меняет только owner.
				r.With(ownerOrAdmin).Get("/settings", h.getSettings)
				r.With(ownerOnly).Patch("/settings", h.patchSettings)

				// Отчёт (S7) и журнал действий (S9) — только owner.
				r.Group(func(r chi.Router) {
					r.Use(ownerOnly)
					r.Get("/reports/month", h.monthReport)
					r.Get("/reports/month/export.xlsx", h.monthReportXLSX)
					r.Get("/audit", h.listAudit)
				})

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

				// Ученики, родители, записи — только owner и admin (преподаватель денег не видит).
				r.Group(func(r chi.Router) {
					r.Use(ownerOrAdmin)
					r.Get("/students", h.listStudents)
					r.Post("/students", h.createStudent)
					r.Get("/students/{studentId}", h.getStudent)
					r.Patch("/students/{studentId}", h.patchStudent)
					r.Post("/students/{studentId}/enrollments", h.createEnrollment)
					r.Post("/students/{studentId}/enrollments/{enrollmentId}/end", h.endEnrollment)
					r.Post("/students/{studentId}/enrollments/{enrollmentId}/renew", h.renewEnrollment)

					// Приглашение родителя в Telegram-бот: ссылка + текст для WhatsApp.
					r.Post("/parents/{parentId}/invite", h.createParentInvite)

					// Деньги: оплаты, отмены, история, должники (F6).
					r.Post("/students/{studentId}/payments", h.createOperation)
					r.Get("/students/{studentId}/ledger", h.studentLedger)
					r.Post("/ledger/{ledgerId}/cancel", h.cancelOperation)
					r.Get("/debtors", h.listDebtors)

					// «Напомнить» в Telegram (R17–R18).
					r.Post("/reminders", h.sendReminders)

					// История посещений ученика — для карточки (S5).
					r.Get("/students/{studentId}/attendance", h.studentAttendance)

					// Импорт из Excel/CSV (F4).
					r.Get("/import/template.xlsx", h.importTemplate)
					r.Post("/import/preview", h.importPreview)
					r.Post("/import/commit", h.importCommit)
				})

				// Посещаемость (F5): занятия и отметки — все сотрудники, преподаватель только свои группы
				// и за последние 7 дней (R12); внеплановые занятия и отмена — owner и admin.
				r.With(anyStaff).Get("/lessons", h.listLessons)
				r.With(anyStaff).Get("/lessons/{lessonId}/attendance", h.getLessonAttendance)
				r.With(anyStaff).Put("/lessons/{lessonId}/attendance", h.putAttendance)
				r.Group(func(r chi.Router) {
					r.Use(ownerOrAdmin)
					r.Post("/lessons", h.createLesson)
					r.Patch("/lessons/{lessonId}", h.patchLesson)
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
	// Шаблон маршрута, а не путь: в путях /pay/{token} и /invites/{token} — секретные токены.
	route := r.Method
	if rc := chi.RouteContext(r.Context()); rc != nil {
		route = rc.RoutePattern()
	}
	h.logger.Error("ошибка обработки запроса", "method", r.Method, "route", route,
		"request_id", middleware.GetReqID(r.Context()), "err", err)
	httpx.Error(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
}
