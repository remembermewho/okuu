package auth

import (
	"errors"
	"time"
)

var (
	// ErrPhoneTaken — телефон уже зарегистрирован.
	ErrPhoneTaken = errors.New("телефон уже зарегистрирован")
	// ErrInvalidCredentials — неверный телефон или пароль (не различаем, чтобы не раскрывать номер).
	ErrInvalidCredentials = errors.New("неверный телефон или пароль")
	// ErrNoSession — нет cookie, сессия не найдена или просрочена.
	ErrNoSession = errors.New("сессия не найдена")
)

// ValidationError — ошибка входных данных в конкретном поле.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// TooManyAttemptsError — превышен лимит неудачных попыток входа.
type TooManyAttemptsError struct {
	RetryAfter time.Duration
}

func (e *TooManyAttemptsError) Error() string {
	return "слишком много попыток входа"
}
