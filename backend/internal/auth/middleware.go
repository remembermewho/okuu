package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"okuu/internal/db/sqlc"
	"okuu/internal/httpx"
)

// SessionCookie — имя cookie с токеном сессии.
const SessionCookie = "okuu_session"

type ctxKey int

const (
	userIDKey ctxKey = iota
	membershipKey
)

// UserID возвращает текущего пользователя (после RequireAuth).
func UserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey).(uuid.UUID)
	return id, ok
}

// MembershipFrom возвращает членство в центре из пути (после RequireCenter).
// Все запросы к данным центра фильтруются по его CenterID.
func MembershipFrom(ctx context.Context) (sqlc.Membership, bool) {
	m, ok := ctx.Value(membershipKey).(sqlc.Membership)
	return m, ok
}

// SetSessionCookie выставляет cookie сессии: HttpOnly, SameSite=Lax, Path=/, Secure в prod.
func (s *Service) SetSessionCookie(w http.ResponseWriter, sess Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    sess.Token,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		MaxAge:   int(sess.ExpiresAt.Sub(s.now()) / time.Second),
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie удаляет cookie сессии в браузере.
func (s *Service) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// SessionToken читает токен из cookie ("" если cookie нет).
func SessionToken(r *http.Request) string {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// RequireOrigin — защита от CSRF: для POST/PUT/PATCH/DELETE заголовок Origin
// должен совпадать с allowed (APP_ORIGIN), иначе 403.
func RequireOrigin(allowed string) func(http.Handler) http.Handler {
	allowed = strings.TrimSuffix(allowed, "/")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				if !strings.EqualFold(r.Header.Get("Origin"), allowed) {
					httpx.Error(w, http.StatusForbidden, "bad_origin", "Запрос с недопустимого источника")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth пускает только с действующей сессией и кладёт пользователя в context.
// При продлении сессии обновляет cookie.
func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, extended, err := s.Authenticate(r.Context(), SessionToken(r))
		if errors.Is(err, ErrNoSession) {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if extended {
			s.SetSessionCookie(w, sess)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey, sess.UserID)))
	})
}

// RequirePlatformAdmin для /admin/*: только суперадмин (users.is_platform_admin). Всем остальным,
// в том числе без входа, — 404 not_found, как будто маршрута нет: существование панели не раскрываем.
func (s *Service) RequirePlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, extended, err := s.Authenticate(r.Context(), SessionToken(r))
		if errors.Is(err, ErrNoSession) {
			notFound(w)
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		u, err := s.q.GetUserByID(r.Context(), sess.UserID)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if !u.IsPlatformAdmin {
			notFound(w)
			return
		}
		if extended {
			s.SetSessionCookie(w, sess)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey, sess.UserID)))
	})
}

// RequireCenter для /c/{centerId}/...: проверяет членство, статус центра и роль (после RequireAuth).
// Не член центра (или центра нет) → 404 not_found, чтобы не раскрывать существование центра;
// заявка ещё не одобрена → 403 center_pending; центр заблокирован → 403 center_blocked
// (/me и выход работают — они вне /c/); член с неподходящей ролью → 403 forbidden.
func (s *Service) RequireCenter(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserID(r.Context())
			if !ok {
				httpx.Error(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
				return
			}
			centerID, err := uuid.Parse(chi.URLParam(r, "centerId"))
			if err != nil {
				notFound(w)
				return
			}
			a, err := s.q.GetMembershipAccess(r.Context(), sqlc.GetMembershipAccessParams{UserID: userID, CenterID: centerID})
			if errors.Is(err, pgx.ErrNoRows) {
				notFound(w)
				return
			}
			if err != nil {
				s.internalError(w, r, err)
				return
			}
			switch a.CenterStatus {
			case CenterPending:
				httpx.Error(w, http.StatusForbidden, "center_pending", "Заявка на подключение центра на рассмотрении")
				return
			case CenterBlocked:
				httpx.Error(w, http.StatusForbidden, "center_blocked", "Доступ к центру приостановлен. Свяжитесь с поддержкой")
				return
			}
			m := sqlc.Membership{UserID: a.UserID, CenterID: a.CenterID, Role: a.Role}
			if !slices.Contains(roles, m.Role) {
				httpx.Error(w, http.StatusForbidden, "forbidden", "Недостаточно прав")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), membershipKey, m)))
		})
	}
}

func notFound(w http.ResponseWriter) {
	httpx.Error(w, http.StatusNotFound, "not_found", "Не найдено")
}

func (s *Service) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("auth middleware", "method", r.Method, "err", err) // без пути: в путях бывают токены
	httpx.Error(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
}
