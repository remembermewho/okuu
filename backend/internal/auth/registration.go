package auth

import (
	"context"
	"errors"
	"fmt"

	"okuu/internal/token"
)

// Режимы регистрации центров (REGISTRATION_MODE).
const (
	// RegistrationInvite — только по одноразовой ссылке {APP_ORIGIN}/register/{токен} (по умолчанию в prod).
	RegistrationInvite = "invite"
	// RegistrationApproval — кто угодно, но центр ждёт одобрения суперадмина (status = pending).
	RegistrationApproval = "approval"
	// RegistrationOpen — свободная регистрация (по умолчанию локально).
	RegistrationOpen = "open"
)

// ValidRegistrationMode — значение REGISTRATION_MODE допустимо.
func ValidRegistrationMode(m string) bool {
	return m == RegistrationInvite || m == RegistrationApproval || m == RegistrationOpen
}

// Статусы центра.
const (
	CenterPending = "pending" // заявка ждёт одобрения
	CenterActive  = "active"
	CenterBlocked = "blocked" // доступ приостановлен суперадмином
)

var (
	// ErrRegistrationClosed — режим invite, а ссылки регистрации нет.
	ErrRegistrationClosed = errors.New("регистрация только по приглашению")
	// ErrRegistrationInvalid — ссылка регистрации неизвестна, использована, отозвана или просрочена.
	ErrRegistrationInvalid = errors.New("ссылка регистрации недействительна")
)

// RegistrationMode — текущий режим регистрации.
func (s *Service) RegistrationMode() string { return s.registrationMode }

// RegistrationLinkActive — ссылка регистрации действует (для страницы /register/{токен}).
func (s *Service) RegistrationLinkActive(ctx context.Context, raw string) (bool, error) {
	if raw == "" {
		return false, nil
	}
	ok, err := s.q.IsRegistrationLinkActive(ctx, token.HashHex(raw))
	if err != nil {
		return false, fmt.Errorf("ссылка регистрации: %w", err)
	}
	return ok, nil
}
