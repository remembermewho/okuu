// Package httpx — общие JSON-ответы и разбор тела запроса для HTTP-слоя.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// MaxBodyBytes — предел размера JSON-тела запроса.
const MaxBodyBytes = 1 << 20

// ErrorBody — формат ошибки API: {"error":{"code":"...","message":"..."}}.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail — код и сообщение ошибки; Field — поле запроса для ошибок validation.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// JSON пишет v как JSON с заданным статусом.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error пишет ошибку в формате API.
func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}

// DecodeJSON читает тело запроса в dst: один JSON-объект, без неизвестных полей, не больше MaxBodyBytes.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("ожидается один JSON-объект")
	}
	return nil
}
