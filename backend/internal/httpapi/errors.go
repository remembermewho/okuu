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
	"okuu/internal/platform"
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
	{auth.ErrNotMember, http.StatusNotFound, "not_found", "Не найдено"},
	{auth.ErrResetNotAllowed, http.StatusConflict, "reset_not_allowed", "Этому сотруднику пароль сбросить нельзя: он работает и в другом центре или он владелец. Обратитесь в поддержку"},
	{auth.ErrResetInvalid, http.StatusNotFound, "reset_invalid", "Ссылка для смены пароля недействительна или устарела"},
	{auth.ErrRegistrationClosed, http.StatusForbidden, "registration_closed", "Регистрация центров — по приглашению. Напишите нам, чтобы получить ссылку"},
	{auth.ErrRegistrationInvalid, http.StatusNotFound, "registration_invalid", "Ссылка регистрации недействительна: она уже использована, отозвана или устарела"},

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
	{parents.ErrDemoCenter, http.StatusConflict, "demo_center", "Это демо-центр: родители вымышленные, Telegram и напоминания не работают"},
	{reminders.ErrDemoCenter, http.StatusConflict, "demo_center", "Это демо-центр: родители вымышленные, Telegram и напоминания не работают"},

	{payments.ErrNotFound, http.StatusNotFound, "not_found", "Ссылка оплаты недействительна или устарела"},
	{payments.ErrAlreadyPaid, http.StatusConflict, "already_paid", "Эта ссылка уже оплачена"},
	{payments.ErrProviderUnavailable, http.StatusBadGateway, "provider_unavailable", "Платёжный сервис недоступен, попробуйте позже"},
	{payments.ErrBadSignature, http.StatusUnauthorized, "bad_signature", "Неверная подпись"},
	{payments.ErrBadEvent, http.StatusBadRequest, "bad_request", "Некорректное уведомление"},
	{payments.ErrNotImplemented, http.StatusServiceUnavailable, "provider_unavailable", "Платёжный сервис ещё не подключён"},

	{reminders.ErrNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{reminders.ErrTelegramOff, http.StatusConflict, "telegram_not_configured", "Telegram-бот не запущен (TELEGRAM_BOT_TOKEN)"},

	{platform.ErrNotFound, http.StatusNotFound, "not_found", "Не найдено"},
	{platform.ErrStatusConflict, http.StatusConflict, "center_status_conflict", "Статус центра уже другой — обновите страницу"},
	{platform.ErrLinkClosed, http.StatusConflict, "registration_link_closed", "Ссылка уже использована или отозвана"},
	{platform.ErrNoOwner, http.StatusConflict, "no_owner", "У центра нет владельца — сбросить пароль некому"},

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
		httpx.Error(w, http.StatusTooManyRequests, "too_many_attempts", "Слишком много неудачных попыток. Попробуйте позже")
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
