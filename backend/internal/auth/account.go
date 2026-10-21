package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"okuu/internal/audit"
	"okuu/internal/db/sqlc"
	"okuu/internal/token"
	"okuu/internal/validate"
)

// Личный кабинет: имя, смена пароля, выход на всех устройствах, сброс пароля по одноразовой ссылке.

var (
	// ErrResetNotAllowed — владелец центра не может сбросить пароль этому сотруднику: тот состоит ещё
	// в других центрах или сам владелец. Сброс — через поддержку (server user reset-link).
	ErrResetNotAllowed = errors.New("сброс пароля недоступен")
	// ErrResetInvalid — ссылка сброса неизвестна, просрочена, отозвана или использована (не различаем).
	ErrResetInvalid = errors.New("ссылка сброса недействительна")
	// ErrNotMember — пользователь не сотрудник этого центра.
	ErrNotMember = errors.New("не сотрудник центра")
	// ErrUserNotFound — нет пользователя с таким телефоном (только для команды поддержки).
	ErrUserNotFound = errors.New("пользователь не найден")
)

// PasswordResetTTL — срок ссылки сброса (сама дата считается в БД: now() + 24 часа).
const PasswordResetTTL = 24 * time.Hour

// UpdateName меняет имя пользователя (видно в сотрудниках всех его центров).
func (s *Service) UpdateName(ctx context.Context, userID uuid.UUID, name string) error {
	name, err := validate.Name("name", "Имя", name)
	if err != nil {
		return err
	}
	return s.q.UpdateUserName(ctx, sqlc.UpdateUserNameParams{ID: userID, Name: name})
}

// ChangePasswordInput — смена пароля из личного кабинета.
type ChangePasswordInput struct {
	UserID       uuid.UUID
	SessionToken string // текущая сессия остаётся, остальные удаляются
	Current, New string
	IP           string
}

// ChangePassword проверяет текущий пароль (лимит попыток — как у входа, на пользователя и IP), задаёт новый
// по правилам регистрации и удаляет все другие сессии пользователя. Неверный текущий — ошибка поля
// current_password.
func (s *Service) ChangePassword(ctx context.Context, in ChangePasswordInput) error {
	userKey, ipKey := "pwd:"+in.UserID.String(), "ip:"+in.IP
	if blocked, retry := s.limiter.Blocked(userKey, ipKey); blocked {
		return &TooManyAttemptsError{RetryAfter: retry}
	}
	user, err := s.q.GetUserByID(ctx, in.UserID)
	if err != nil {
		return err
	}
	ok, err := VerifyPassword(in.Current, user.PasswordHash)
	if err != nil {
		return fmt.Errorf("проверка пароля пользователя %s: %w", user.ID, err)
	}
	if !ok {
		s.limiter.Fail(userKey, ipKey)
		return validate.Errorf("current_password", "Текущий пароль неверный")
	}
	s.limiter.Reset(userKey)
	if err := ValidatePassword(in.New); err != nil {
		return asField(err, "new_password")
	}
	hash, err := HashPassword(in.New)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		if err := q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{ID: user.ID, PasswordHash: hash}); err != nil {
			return err
		}
		n, err := q.DeleteUserSessionsExcept(ctx, sqlc.DeleteUserSessionsExceptParams{UserID: user.ID, KeepTokenHash: token.Hash(in.SessionToken)})
		if err != nil {
			return err
		}
		return recordForUserCenters(ctx, tx, user.ID, &user.ID, audit.UserPasswordChange, map[string]any{"other_sessions_closed": n})
	})
}

// LogoutAll удаляет все сессии пользователя, включая текущую.
func (s *Service) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		n, err := q.DeleteUserSessions(ctx, userID)
		if err != nil {
			return err
		}
		return recordForUserCenters(ctx, tx, userID, &userID, audit.UserLogoutAll, map[string]any{"sessions_closed": n})
	})
}

// IssuedReset — выданная ссылка сброса. Token есть только здесь: в БД — его хэш.
type IssuedReset struct {
	Token     string
	UserName  string
	ExpiresAt time.Time
}

// IssueStaffPasswordReset — владелец центра выдаёт ссылку сброса пароля сотруднику. Разрешено, только если
// сотрудник состоит ТОЛЬКО в этом центре и он не владелец: иначе владелец одного центра мог бы перехватить
// аккаунт человека, который работает и в другом. Не сотрудник этого центра — ErrNotMember (404).
func (s *Service) IssueStaffPasswordReset(ctx context.Context, actor, centerID, userID uuid.UUID) (IssuedReset, error) {
	var out IssuedReset
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		centers, err := q.ListMembershipsByUser(ctx, userID)
		if err != nil {
			return err
		}
		var here *sqlc.ListMembershipsByUserRow
		for i := range centers {
			if centers[i].CenterID == centerID {
				here = &centers[i]
			}
		}
		if here == nil {
			return ErrNotMember
		}
		if len(centers) != 1 || here.Role == RoleOwner {
			return ErrResetNotAllowed
		}
		user, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return err
		}
		if out, err = issueReset(ctx, q, user, &centerID, &actor); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.UserPasswordResetIssue, Entity: "user", EntityID: &userID,
			After: map[string]any{"name": user.Name, "expires_at": out.ExpiresAt},
		})
	})
	return out, err
}

// IssueSupportPasswordReset — ссылка сброса для поддержки (команда server user reset-link): без ограничений
// по центрам, в том числе владельцу. В журналы его центров пишется user.password_reset_issue без автора.
func (s *Service) IssueSupportPasswordReset(ctx context.Context, phone string) (IssuedReset, error) {
	phone, err := NormalizePhone(phone)
	if err != nil {
		return IssuedReset{}, err
	}
	var out IssuedReset
	err = s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		user, err := q.GetUserByPhone(ctx, phone)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		if err != nil {
			return err
		}
		if out, err = issueReset(ctx, q, user, nil, nil); err != nil {
			return err
		}
		return recordForUserCenters(ctx, tx, user.ID, nil, audit.UserPasswordResetIssue,
			map[string]any{"name": user.Name, "expires_at": out.ExpiresAt, "by": "support"})
	})
	return out, err
}

func issueReset(ctx context.Context, q *sqlc.Queries, user sqlc.User, centerID, actor *uuid.UUID) (IssuedReset, error) {
	tok, err := token.New()
	if err != nil {
		return IssuedReset{}, err
	}
	if err := q.RevokeUserPasswordResets(ctx, user.ID); err != nil {
		return IssuedReset{}, err
	}
	row, err := q.CreatePasswordReset(ctx, sqlc.CreatePasswordResetParams{
		Token: token.HashHex(tok), UserID: user.ID, CenterID: centerID, CreatedBy: actor,
	})
	if err != nil {
		return IssuedReset{}, err
	}
	return IssuedReset{Token: tok, UserName: user.Name, ExpiresAt: row.ExpiresAt}, nil
}

// PublicReset — что видно по ссылке сброса до входа.
type PublicReset struct {
	UserName   string
	CenterName *string // nil — ссылку выдала поддержка
}

// GetPasswordReset — действующая ссылка сброса. Неизвестная, просроченная, отозванная, использованная —
// одинаково ErrResetInvalid.
func (s *Service) GetPasswordReset(ctx context.Context, tok string) (PublicReset, error) {
	if tok == "" {
		return PublicReset{}, ErrResetInvalid
	}
	row, err := s.q.GetActivePasswordReset(ctx, token.HashHex(tok))
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicReset{}, ErrResetInvalid
	}
	if err != nil {
		return PublicReset{}, err
	}
	return PublicReset{UserName: row.UserName, CenterName: row.CenterName}, nil
}

// CompletePasswordReset задаёт новый пароль по ссылке: ссылка становится использованной, все сессии
// пользователя удаляются (войти заново — с новым паролем). Пароль проверяется до того, как ссылка
// израсходуется.
func (s *Service) CompletePasswordReset(ctx context.Context, tok, newPassword string) error {
	if tok == "" {
		return ErrResetInvalid
	}
	if err := ValidatePassword(newPassword); err != nil {
		return asField(err, "new_password")
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		row, err := q.ClaimPasswordReset(ctx, token.HashHex(tok))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrResetInvalid
		}
		if err != nil {
			return err
		}
		if err := q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{ID: row.UserID, PasswordHash: hash}); err != nil {
			return err
		}
		n, err := q.DeleteUserSessions(ctx, row.UserID)
		if err != nil {
			return err
		}
		after := map[string]any{"sessions_closed": n}
		if row.CenterID != nil {
			return audit.Record(ctx, tx, audit.Entry{
				CenterID: *row.CenterID, UserID: &row.UserID, Action: audit.UserPasswordResetDone, Entity: "user", EntityID: &row.UserID, After: after,
			})
		}
		return recordForUserCenters(ctx, tx, row.UserID, &row.UserID, audit.UserPasswordResetDone, after)
	})
}

// recordForUserCenters — событие аккаунта (не центра) — в журнал каждого центра пользователя: журнал
// привязан к центру, а владельцу полезно видеть смену пароля и выход сотрудника.
func recordForUserCenters(ctx context.Context, tx pgx.Tx, userID uuid.UUID, actor *uuid.UUID, action string, after map[string]any) error {
	centers, err := sqlc.New(tx).ListMembershipsByUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, c := range centers {
		if err := audit.Record(ctx, tx, audit.Entry{
			CenterID: c.CenterID, UserID: actor, Action: action, Entity: "user", EntityID: &userID, After: after,
		}); err != nil {
			return err
		}
	}
	return nil
}

// asField — ошибка проверки пароля (поле password) под другим полем запроса.
func asField(err error, field string) error {
	var ve *validate.Error
	if errors.As(err, &ve) {
		return &validate.Error{Field: field, Message: ve.Message, Unprocessable: ve.Unprocessable}
	}
	return err
}

func (s *Service) inTx(ctx context.Context, fn func(pgx.Tx, *sqlc.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx, s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
