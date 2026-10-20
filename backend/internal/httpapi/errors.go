package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strconv"

	"okuu/internal/attendance"
	"okuu/internal/auth"
	"okuu/internal/billing"
	"okuu/internal/groups"
	"okuu/internal/httpx"
	"okuu/internal/importer"
	"okuu/internal/parents"
	"okuu/internal/payments"
	"okuu/internal/reminders"
	"okuu/internal/staff"
	"okuu/internal/students"
	"okuu/internal/tariffs"
	"okuu/internal/validate"
)

// knownErrors — ошибки сервисов и их ответы API. Всё, чего нет здесь, — 500 internal.
var knownErrors = []struct {
	err     error
	status  int
	code    string
	message string
}{
	{auth.ErrPhoneTaken, http.StatusConflict, "phone_taken", "Этот телефон уже зарегистрирован"},
	{auth.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials", "Неверный телефон или пароль"},

	{staff.ErrNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{staff.ErrInviteInvalid, http.StatusNotFound, "invite_invalid", "Приглашение недействительно или уже использовано"},
	{staff.ErrLoginRequired, http.StatusConflict, "login_required", "Этот телефон уже зарегистрирован. Войдите и примите приглашение снова"},
	{staff.ErrAlreadyMember, http.StatusConflict, "already_member", "Вы уже сотрудник этого центра"},
	{staff.ErrOwnerProtected, http.StatusConflict, "owner_protected", "Владельца центра нельзя изменить или удалить"},
	{staff.ErrTeacherHasGroups, http.StatusConflict, "teacher_has_groups", "У преподавателя есть группы — сначала назначьте им другого преподавателя"},

	{tariffs.ErrNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{tariffs.ErrInUse, http.StatusConflict, "tariff_in_use", "На тариф уже записаны ученики — тип тарифа менять нельзя"},

	{groups.ErrNotFound, http.StatusNotFound, "not_found", "Не найдено"},

	{students.ErrNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{students.ErrParentPhoneTaken, http.StatusConflict, "parent_phone_taken", "Этот телефон уже у другого родителя в центре"},
	{students.ErrAlreadyEnrolled, http.StatusConflict, "already_enrolled", "Ученик уже записан в эту группу"},
	{students.ErrEnrollmentEnded, http.StatusConflict, "enrollment_ended", "Запись уже завершена"},
	{students.ErrNotPackage, http.StatusConflict, "not_package", "Продлить можно только абонемент"},
	{students.ErrStudentLeft, http.StatusConflict, "student_left", "Ученик ушёл — сначала верните ему статус"},

	{billing.ErrStudentNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{billing.ErrOperationNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{billing.ErrAlreadyCancelled, http.StatusConflict, "already_cancelled", "Операция уже отменена"},
	{billing.ErrIdempotencyMismatch, http.StatusUnprocessableEntity, "idempotency_mismatch", "Этот Idempotency-Key уже использован для другой операции"},
	{billing.ErrFinikOwnerOnly, http.StatusForbidden, "forbidden", "Оплату через Finik может отменить только владелец"},

	{attendance.ErrNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{attendance.ErrStudentNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{attendance.ErrLessonCancelled, http.StatusConflict, "lesson_cancelled", "Занятие отменено — отметки не ставятся"},
	{attendance.ErrLessonExists, http.StatusConflict, "lesson_exists", "У группы уже есть занятие в это время"},
	{attendance.ErrMarkWindow, http.StatusForbidden, "forbidden", "Преподаватель меняет отметки только за последние 7 дней"},

	{parents.ErrNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{parents.ErrBotNotConfigured, http.StatusConflict, "telegram_not_configured", "Telegram-бот не настроен (TELEGRAM_BOT_USERNAME)"},

	{payments.ErrNotFound, http.StatusNotFound, "not_found", "Ссылка оплаты недействительна или устарела"},
	{payments.ErrAlreadyPaid, http.StatusConflict, "already_paid", "Эта ссылка уже оплачена"},
	{payments.ErrProviderUnavailable, http.StatusBadGateway, "provider_unavailable", "Платёжный сервис недоступен, попробуйте позже"},
	{payments.ErrBadSignature, http.StatusUnauthorized, "bad_signature", "Неверная подпись"},
	{payments.ErrBadEvent, http.StatusBadRequest, "bad_request", "Некорректное уведомление"},
	{payments.ErrNotImplemented, http.StatusServiceUnavailable, "provider_unavailable", "Платёжный сервис ещё не подключён"},

	{reminders.ErrNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{reminders.ErrTelegramOff, http.StatusConflict, "telegram_not_configured", "Telegram-бот не запущен (TELEGRAM_BOT_TOKEN)"},

	{importer.ErrNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{importer.ErrAlreadyCommitted, http.StatusConflict, "import_already_committed", "Этот импорт уже подтверждён"},
	{importer.ErrExpired, http.StatusGone, "import_expired", "Превью устарело (хранится час) — загрузите файл заново"},
}

// fail переводит ошибку сервиса в ответ API.
func (h *handlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	var verr *validate.Error
	if errors.As(err, &verr) {
		status := http.StatusBadRequest
		if verr.Unprocessable {
			status = http.StatusUnprocessableEntity
		}
		httpx.JSON(w, status, httpx.ErrorBody{Error: httpx.ErrorDetail{Code: "validation", Message: verr.Message, Field: verr.Field}})
		return
	}
	var tooMany *auth.TooManyAttemptsError
	if errors.As(err, &tooMany) {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(tooMany.RetryAfter.Seconds()))))
		httpx.Error(w, http.StatusTooManyRequests, "too_many_attempts", "Слишком много попыток входа. Попробуйте позже")
		return
	}
	for _, k := range knownErrors {
		if errors.Is(err, k.err) {
			httpx.Error(w, k.status, k.code, k.message)
			return
		}
	}
	h.internalError(w, r, err)
}

func badJSON(w http.ResponseWriter) {
	httpx.Error(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса")
}
