// Package staff — сотрудники центра и приглашения (F1). Управляет ими только владелец;
// в MVP владелец один, по приглашению выдаются только роли admin и teacher.
package staff

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/audit"
	"okuu/internal/auth"
	"okuu/internal/db/sqlc"
	"okuu/internal/token"
	"okuu/internal/validate"
)

var (
	// ErrNotFound — сотрудника или приглашения нет в этом центре.
	ErrNotFound = errors.New("не найдено")
	// ErrInviteInvalid — приглашение неизвестно, просрочено, отозвано или уже использовано
	// (не различаем, чтобы не раскрывать, что токен когда-то существовал).
	ErrInviteInvalid = errors.New("приглашение недействительно")
	// ErrLoginRequired — телефон уже зарегистрирован: нужно войти и принять приглашение снова.
	ErrLoginRequired = errors.New("нужно войти")
	// ErrAlreadyMember — пользователь уже сотрудник этого центра.
	ErrAlreadyMember = errors.New("уже сотрудник центра")
	// ErrOwnerProtected — владельца нельзя понизить или удалить.
	ErrOwnerProtected = errors.New("владельца нельзя изменить")
	// ErrTeacherHasGroups — у преподавателя есть активные группы: сначала переназначить.
	ErrTeacherHasGroups = errors.New("у преподавателя есть группы")
)

// Service — сотрудники и приглашения.
type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
	auth *auth.Service
}

// NewService создаёт сервис; auth нужен, чтобы открыть сессию новому сотруднику.
func NewService(pool *pgxpool.Pool, authSvc *auth.Service) *Service {
	return &Service{pool: pool, q: sqlc.New(pool), auth: authSvc}
}

// Invite — приглашение без токена (список, ответы API).
type Invite struct {
	ID        uuid.UUID
	Role      string
	Name      *string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// CreatedInvite — только что созданное приглашение. Token есть только здесь: в БД его нет.
type CreatedInvite struct {
	Invite
	Token string
}

// CreateInvite создаёт одноразовое приглашение на 7 дней с ролью admin или teacher.
func (s *Service) CreateInvite(ctx context.Context, actor, centerID uuid.UUID, role, name string) (CreatedInvite, error) {
	if err := validateInviteRole(role); err != nil {
		return CreatedInvite{}, err
	}
	name, err := validate.OptionalName("name", "Имя", name)
	if err != nil {
		return CreatedInvite{}, err
	}
	tok, err := token.New()
	if err != nil {
		return CreatedInvite{}, err
	}

	var created CreatedInvite
	err = s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		row, err := q.CreateStaffInvite(ctx, sqlc.CreateStaffInviteParams{
			Token:     token.HashHex(tok),
			CenterID:  centerID,
			Role:      &role,
			Name:      nilIfEmpty(name),
			CreatedBy: &actor,
		})
		if err != nil {
			return err
		}
		created = CreatedInvite{
			Invite: Invite{ID: row.ID, Role: role, Name: row.Name, ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt},
			Token:  tok,
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.InviteCreate, Entity: "invite", EntityID: &row.ID,
			After: map[string]any{"role": role, "name": row.Name, "expires_at": row.ExpiresAt},
		})
	})
	return created, err
}

// ListInvites — действующие приглашения центра.
func (s *Service) ListInvites(ctx context.Context, centerID uuid.UUID) ([]Invite, error) {
	rows, err := s.q.ListActiveStaffInvites(ctx, centerID)
	if err != nil {
		return nil, err
	}
	out := make([]Invite, 0, len(rows))
	for _, r := range rows {
		out = append(out, Invite{ID: r.ID, Role: deref(r.Role), Name: r.Name, ExpiresAt: r.ExpiresAt, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// RevokeInvite отзывает неиспользованное приглашение. Чужое, уже отозванное или использованное — ErrNotFound.
func (s *Service) RevokeInvite(ctx context.Context, actor, centerID, inviteID uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		row, err := q.RevokeStaffInvite(ctx, sqlc.RevokeStaffInviteParams{ID: inviteID, CenterID: centerID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.InviteRevoke, Entity: "invite", EntityID: &row.ID,
			Before: map[string]any{"role": row.Role, "name": row.Name, "expires_at": row.ExpiresAt},
			After:  map[string]any{"revoked": true},
		})
	})
}

// PublicInvite — то, что видит человек по ссылке до входа.
type PublicInvite struct {
	CenterName string
	Role       string
	Name       *string
	ExpiresAt  time.Time
}

// GetInvite находит действующее приглашение по открытому токену из ссылки.
func (s *Service) GetInvite(ctx context.Context, tok string) (PublicInvite, error) {
	row, err := s.q.GetActiveStaffInvite(ctx, token.HashHex(tok))
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicInvite{}, ErrInviteInvalid
	}
	if err != nil {
		return PublicInvite{}, err
	}
	return PublicInvite{CenterName: row.CenterName, Role: deref(row.Role), Name: row.Name, ExpiresAt: row.ExpiresAt}, nil
}

// AcceptInput — принятие приглашения. UserID задан, если человек уже вошёл;
// иначе по Name/Phone/Password создаётся новый пользователь. Пустое Name — берётся из приглашения.
type AcceptInput struct {
	Token     string
	UserID    *uuid.UUID
	Name      string
	Phone     string
	Password  string
	UserAgent string
}

// AcceptResult — кто стал сотрудником. Session задана, если пользователь создан сейчас.
type AcceptResult struct {
	UserID   uuid.UUID
	CenterID uuid.UUID
	Session  *auth.Session
}

// Accept в одной транзакции: помечает приглашение использованным, при необходимости
// создаёт пользователя и сессию, добавляет членство и пишет аудит. Любая ошибка — откат,
// приглашение остаётся действующим.
func (s *Service) Accept(ctx context.Context, in AcceptInput) (AcceptResult, error) {
	var res AcceptResult
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		inv, err := q.ClaimStaffInvite(ctx, token.HashHex(in.Token))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInviteInvalid
		}
		if err != nil {
			return err
		}
		role := deref(inv.Role)
		res.CenterID = inv.CenterID

		newUser := in.UserID == nil
		if newUser {
			name := in.Name
			if name == "" && inv.Name != nil {
				name = *inv.Name
			}
			user, err := auth.CreateUser(ctx, tx, in.Phone, name, in.Password)
			if errors.Is(err, auth.ErrPhoneTaken) {
				return ErrLoginRequired
			}
			if err != nil {
				return err
			}
			res.UserID = user.ID
		} else {
			res.UserID = *in.UserID
		}

		if _, err := q.CreateMembership(ctx, sqlc.CreateMembershipParams{
			UserID: res.UserID, CenterID: inv.CenterID, Role: role,
		}); err != nil {
			if isUniqueViolation(err) {
				return ErrAlreadyMember
			}
			return err
		}

		if err := audit.Record(ctx, tx, audit.Entry{
			CenterID: inv.CenterID, UserID: &res.UserID, Action: audit.InviteAccept, Entity: "invite", EntityID: &inv.ID,
			After: map[string]any{"user_id": res.UserID, "role": role, "new_user": newUser},
		}); err != nil {
			return err
		}

		if newUser {
			sess, err := s.auth.NewSession(ctx, tx, res.UserID, in.UserAgent)
			if err != nil {
				return err
			}
			res.Session = &sess
		}
		return nil
	})
	return res, err
}

// Member — сотрудник центра.
type Member struct {
	UserID uuid.UUID
	Name   string
	Phone  string
	Role   string
}

// List — сотрудники центра.
func (s *Service) List(ctx context.Context, centerID uuid.UUID) ([]Member, error) {
	rows, err := s.q.ListStaff(ctx, centerID)
	if err != nil {
		return nil, err
	}
	out := make([]Member, 0, len(rows))
	for _, r := range rows {
		out = append(out, Member{UserID: r.UserID, Name: r.Name, Phone: r.Phone, Role: r.Role})
	}
	return out, nil
}

// ChangeRole меняет роль admin ↔ teacher. Владельца менять нельзя.
func (s *Service) ChangeRole(ctx context.Context, actor, centerID, userID uuid.UUID, role string) (Member, error) {
	if err := validateInviteRole(role); err != nil {
		return Member{}, err
	}
	var m Member
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		cur, err := s.lockMember(ctx, q, centerID, userID)
		if err != nil {
			return err
		}
		m = Member{UserID: cur.UserID, Name: cur.Name, Phone: cur.Phone, Role: role}
		if cur.Role == auth.RoleOwner {
			return ErrOwnerProtected
		}
		if cur.Role == role {
			return nil
		}
		if err := q.UpdateMembershipRole(ctx, sqlc.UpdateMembershipRoleParams{Role: role, CenterID: centerID, UserID: userID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.StaffRoleChange, Entity: "membership", EntityID: &userID,
			Before: map[string]any{"role": cur.Role}, After: map[string]any{"role": role},
		})
	})
	return m, err
}

// Remove убирает сотрудника из центра. Владельца удалить нельзя; преподавателя с
// активными группами — только после переназначения групп. Доступ пропадает сразу:
// RequireCenter проверяет членство на каждом запросе.
func (s *Service) Remove(ctx context.Context, actor, centerID, userID uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		cur, err := s.lockMember(ctx, q, centerID, userID)
		if err != nil {
			return err
		}
		if cur.Role == auth.RoleOwner {
			return ErrOwnerProtected
		}
		groups, err := q.CountActiveGroupsOfTeacher(ctx, sqlc.CountActiveGroupsOfTeacherParams{CenterID: centerID, TeacherID: &userID})
		if err != nil {
			return err
		}
		if groups > 0 {
			return ErrTeacherHasGroups
		}
		if err := q.DeleteMembership(ctx, sqlc.DeleteMembershipParams{CenterID: centerID, UserID: userID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.StaffRemove, Entity: "membership", EntityID: &userID,
			Before: map[string]any{"role": cur.Role, "name": cur.Name, "phone": cur.Phone},
		})
	})
}

func (s *Service) lockMember(ctx context.Context, q *sqlc.Queries, centerID, userID uuid.UUID) (sqlc.GetStaffMemberForUpdateRow, error) {
	cur, err := q.GetStaffMemberForUpdate(ctx, sqlc.GetStaffMemberForUpdateParams{CenterID: centerID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return cur, ErrNotFound
	}
	return cur, err
}

// inTx выполняет fn в транзакции: ошибка fn — откат, иначе commit.
func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx, q *sqlc.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx, s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func validateInviteRole(role string) error {
	if role != auth.RoleAdmin && role != auth.RoleTeacher {
		return validate.Errorf("role", "Роль сотрудника — admin или teacher")
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
