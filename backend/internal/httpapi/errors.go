package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strconv"

	"okuu/internal/auth"
	"okuu/internal/groups"
	"okuu/internal/httpx"
	"okuu/internal/staff"
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
