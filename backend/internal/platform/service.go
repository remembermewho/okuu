// Package platform — панель суперадмина: центры (одобрение, блокировка, тариф и оплата), ссылки
// регистрации, сброс пароля владельца, журнал действий платформы.
//
// Суперадмин видит о центре только агрегаты: ученики, родители, операции и журнал центра ему недоступны.
// Права суперадмина выдаёт только консольная команда (server admin grant), через API — никак.
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/auth"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/token"
	"okuu/internal/validate"
)

// Тарифы сервиса (centers.plan).
var Plans = []string{"free", "start", "center", "network", "founder"}

// Действия в platform_audit_log.
const (
	ActionCenterApprove     = "center.approve"
	ActionCenterBlock       = "center.block"
	ActionCenterUnblock     = "center.unblock"
	ActionCenterPlan        = "center.plan"
	ActionOwnerPasswordLink = "center.owner_password_reset"
	ActionLinkCreate        = "registration_link.create"
	ActionLinkRevoke        = "registration_link.revoke"
	ActionAdminGrant        = "admin.grant"
	ActionAdminRevoke       = "admin.revoke"
)

const (
	maxNote   = 200
	maxReason = 500
	listLimit = 500 // центров и ссылок на старте — десятки; поиск сужает список
)

var (
	// ErrNotFound — центра или ссылки нет.
	ErrNotFound = errors.New("не найдено")
	// ErrStatusConflict — действие не подходит к текущему статусу центра (одобрить активный и т. п.).
	ErrStatusConflict = errors.New("статус центра не позволяет это действие")
	// ErrLinkClosed — ссылку регистрации уже использовали или отозвали.
	ErrLinkClosed = errors.New("ссылка регистрации уже использована или отозвана")
	// ErrNoOwner — у центра нет владельца (ручная правка БД): сбрасывать пароль некому.
	ErrNoOwner = errors.New("у центра нет владельца")
	// ErrUserNotFound — пользователя с таким телефоном нет (server admin grant).
	ErrUserNotFound = errors.New("пользователь с таким телефоном не найден")
)

// Service — действия суперадмина.
type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
	auth *auth.Service
	now  func() time.Time
}

// NewService создаёт сервис; authSvc — для ссылки сброса пароля владельца.
func NewService(pool *pgxpool.Pool, authSvc *auth.Service, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, q: sqlc.New(pool), auth: authSvc, now: now}
}

// platformTZ — пояс платформы: по нему «оплачено до» считается просроченным.
const platformTZ = "Asia/Bishkek"

// Now — часы сервиса.
func (s *Service) Now() time.Time { return s.now() }

// Today — сегодня в поясе платформы (Бишкек): «оплачено до» раньше него — просрочено.
func (s *Service) Today() civil.Date {
	d, err := civil.Today(s.now(), platformTZ)
	if err != nil {
		return civil.DateOf(s.now())
	}
	return d
}

// record пишет действие в журнал платформы; admin == nil — консольная команда.
func record(ctx context.Context, q *sqlc.Queries, admin *uuid.UUID, action string, center *uuid.UUID, details map[string]any) error {
	var b []byte
	if details != nil {
		var err error
		if b, err = json.Marshal(details); err != nil {
			return err
		}
	}
	if err := q.InsertPlatformAudit(ctx, sqlc.InsertPlatformAuditParams{AdminID: admin, Action: action, CenterID: center, Details: b}); err != nil {
		return fmt.Errorf("журнал платформы %s: %w", action, err)
	}
	return nil
}

func (s *Service) inTx(ctx context.Context, fn func(q *sqlc.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(s.q.WithTx(tx)) })
}

// ---------------------------------------------------------------- ссылки регистрации

// IssuedLink — новая ссылка регистрации. Token — только в ответе (в БД — hex(sha256)).
type IssuedLink struct {
	ID        uuid.UUID
	Token     string
	Note      string
	ExpiresAt time.Time
}

// CreateRegistrationLink — одноразовая ссылка регистрации центра на 14 дней. note — для кого (обязательно).
// admin == nil — консольная команда (первый центр, пока суперадмина нет).
func (s *Service) CreateRegistrationLink(ctx context.Context, admin *uuid.UUID, note string) (IssuedLink, error) {
	note, err := cleanText("note", "Для кого ссылка", note, maxNote)
	if err != nil {
		return IssuedLink{}, err
	}
	raw, err := token.New()
	if err != nil {
		return IssuedLink{}, err
	}
	var out IssuedLink
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.CreateRegistrationLink(ctx, sqlc.CreateRegistrationLinkParams{Token: token.HashHex(raw), Note: note, CreatedBy: admin})
		if err != nil {
			return err
		}
		out = IssuedLink{ID: row.ID, Token: raw, Note: note, ExpiresAt: row.ExpiresAt}
		return record(ctx, q, admin, ActionLinkCreate, nil, map[string]any{"link_id": row.ID, "note": note, "expires_at": row.ExpiresAt})
	})
	return out, err
}

// ListRegistrationLinks — ссылки, новые сверху.
func (s *Service) ListRegistrationLinks(ctx context.Context) ([]sqlc.ListRegistrationLinksRow, error) {
	return s.q.ListRegistrationLinks(ctx, listLimit)
}

// RevokeRegistrationLink отзывает неиспользованную ссылку.
func (s *Service) RevokeRegistrationLink(ctx context.Context, admin uuid.UUID, id uuid.UUID) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.RevokeRegistrationLink(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := q.GetRegistrationLink(ctx, id); errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
			return ErrLinkClosed
		}
		if err != nil {
			return err
		}
		return record(ctx, q, &admin, ActionLinkRevoke, nil, map[string]any{"link_id": id, "note": row.Note})
	})
}

// ---------------------------------------------------------------- центры

// ListCenters — центры с поиском (название, имя или телефон владельца) и фильтром по статусу.
// Заявки (pending) — сверху.
func (s *Service) ListCenters(ctx context.Context, query, status string) ([]sqlc.ListAdminCentersRow, error) {
	p := sqlc.ListAdminCentersParams{Lim: listLimit}
	if status != "" {
		if status != auth.CenterPending && status != auth.CenterActive && status != auth.CenterBlocked {
			return nil, validate.Errorf("status", "status — pending, active или blocked")
		}
		p.Status = &status
	}
	if q := strings.TrimSpace(query); q != "" {
		p.Q = &q
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, q)
		// «0707 123 456» — в БД «+996707123456»: ведущий 0 местного формата не ищем.
		digits = strings.TrimPrefix(digits, "0")
		if len(digits) >= 3 {
			p.QDigits = &digits
		}
	}
	return s.q.ListAdminCenters(ctx, p)
}

// CenterCard — карточка центра: агрегаты и журнал платформы по нему.
type CenterCard struct {
	sqlc.GetAdminCenterRow
	Audit []sqlc.ListPlatformAuditRow
}

// GetCenter — карточка центра.
func (s *Service) GetCenter(ctx context.Context, id uuid.UUID) (CenterCard, error) {
	row, err := s.q.GetAdminCenter(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return CenterCard{}, ErrNotFound
	}
	if err != nil {
		return CenterCard{}, err
	}
	a, err := s.q.ListPlatformAudit(ctx, sqlc.ListPlatformAuditParams{CenterID: &id, Lim: 50})
	if err != nil {
		return CenterCard{}, err
	}
	return CenterCard{GetAdminCenterRow: row, Audit: a}, nil
}

// changeStatus — общее для approve, block и unblock: UPDATE с условием на текущий статус.
func (s *Service) changeStatus(ctx context.Context, admin, id uuid.UUID, action string, details map[string]any,
	update func(q *sqlc.Queries) (uuid.UUID, error)) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		if _, err := update(q); errors.Is(err, pgx.ErrNoRows) {
			ok, err := q.CenterExists(ctx, id)
			if err != nil {
				return err
			}
			if !ok {
				return ErrNotFound
			}
			return ErrStatusConflict
		} else if err != nil {
			return err
		}
		return record(ctx, q, &admin, action, &id, details)
	})
}

// Approve одобряет заявку (pending → active).
func (s *Service) Approve(ctx context.Context, admin, id uuid.UUID) error {
	return s.changeStatus(ctx, admin, id, ActionCenterApprove, nil, func(q *sqlc.Queries) (uuid.UUID, error) {
		return q.ApproveCenter(ctx, id)
	})
}

// Block приостанавливает доступ к центру (pending или active → blocked); причина обязательна.
func (s *Service) Block(ctx context.Context, admin, id uuid.UUID, reason string) error {
	reason, err := cleanText("reason", "Причина", reason, maxReason)
	if err != nil {
		return err
	}
	return s.changeStatus(ctx, admin, id, ActionCenterBlock, map[string]any{"reason": reason}, func(q *sqlc.Queries) (uuid.UUID, error) {
		return q.BlockCenter(ctx, sqlc.BlockCenterParams{ID: id, Reason: &reason})
	})
}

// Unblock возвращает доступ (blocked → active).
func (s *Service) Unblock(ctx context.Context, admin, id uuid.UUID) error {
	return s.changeStatus(ctx, admin, id, ActionCenterUnblock, nil, func(q *sqlc.Queries) (uuid.UUID, error) {
		return q.UnblockCenter(ctx, id)
	})
}

// SetPlan задаёт тариф и «оплачено до» (nil — не задано). Просрочка оплаты центр не блокирует.
func (s *Service) SetPlan(ctx context.Context, admin, id uuid.UUID, plan string, paidUntil *civil.Date) error {
	if !validPlan(plan) {
		return validate.Errorf("plan", "Тариф — один из: %s", strings.Join(Plans, ", "))
	}
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		before, err := q.GetAdminCenter(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := q.SetCenterPlan(ctx, sqlc.SetCenterPlanParams{ID: id, Plan: plan, PaidUntil: paidUntil}); err != nil {
			return err
		}
		return record(ctx, q, &admin, ActionCenterPlan, &id, map[string]any{
			"before": map[string]any{"plan": before.Plan, "paid_until": before.PaidUntil},
			"after":  map[string]any{"plan": plan, "paid_until": paidUntil},
		})
	})
}

// OwnerPasswordReset — одноразовая ссылка сброса пароля владельца центра (24 часа). Заменяет
// консольную server user reset-link; та остаётся для аварий (например, суперадмин сам без доступа).
func (s *Service) OwnerPasswordReset(ctx context.Context, admin, id uuid.UUID) (auth.IssuedReset, error) {
	c, err := s.q.GetAdminCenter(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.IssuedReset{}, ErrNotFound
	}
	if err != nil {
		return auth.IssuedReset{}, err
	}
	if c.OwnerPhone == "" {
		return auth.IssuedReset{}, ErrNoOwner
	}
	res, err := s.auth.IssueSupportPasswordReset(ctx, c.OwnerPhone)
	if err != nil {
		return auth.IssuedReset{}, err
	}
	if err := record(ctx, s.q, &admin, ActionOwnerPasswordLink, &id, map[string]any{"owner": c.OwnerName, "expires_at": res.ExpiresAt}); err != nil {
		return auth.IssuedReset{}, err
	}
	return res, nil
}

// ListAudit — журнал платформы, новые сверху.
func (s *Service) ListAudit(ctx context.Context, limit int) ([]sqlc.ListPlatformAuditRow, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	return s.q.ListPlatformAudit(ctx, sqlc.ListPlatformAuditParams{Lim: int32(limit)})
}

// ---------------------------------------------------------------- права суперадмина (только консоль)

// SetPlatformAdmin выдаёт (on) или снимает права суперадмина по телефону. Только для server admin grant/revoke.
func (s *Service) SetPlatformAdmin(ctx context.Context, phone string, on bool) (string, error) {
	phone, err := auth.NormalizePhone(phone)
	if err != nil {
		return "", err
	}
	var name string
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.SetPlatformAdmin(ctx, sqlc.SetPlatformAdminParams{Phone: phone, IsAdmin: on})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		if err != nil {
			return err
		}
		name = row.Name
		action := ActionAdminGrant
		if !on {
			action = ActionAdminRevoke
		}
		return record(ctx, q, nil, action, nil, map[string]any{"user_id": row.ID, "name": row.Name})
	})
	return name, err
}

func validPlan(p string) bool {
	for _, v := range Plans {
		if v == p {
			return true
		}
	}
	return false
}

func cleanText(field, label, v string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", validate.Errorf(field, "%s — обязательное поле", label)
	}
	if utf8.RuneCountInString(v) > max {
		return "", validate.Errorf(field, "%s — не длиннее %d символов", label, max)
	}
	return v, nil
}
