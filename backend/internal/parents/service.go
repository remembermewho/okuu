// Package parents — приглашение родителя в Telegram-бот: ссылка t.me/<бот>?start=<токен>
// и готовый текст для WhatsApp. Саму привязку делает бот (telegram/bot).
package parents

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/audit"
	"okuu/internal/db/sqlc"
	"okuu/internal/notify"
	"okuu/internal/token"
)

var (
	// ErrNotFound — родителя нет в этом центре.
	ErrNotFound = errors.New("родитель не найден")
	// ErrBotNotConfigured — не задано имя бота (TELEGRAM_BOT_USERNAME): ссылку не построить.
	ErrBotNotConfigured = errors.New("telegram-бот не настроен")
)

// Service — приглашения родителей.
type Service struct {
	pool        *pgxpool.Pool
	botUsername string
}

// NewService создаёт сервис; botUsername — имя бота без @ (пустое — приглашения недоступны).
func NewService(pool *pgxpool.Pool, botUsername string) *Service {
	return &Service{pool: pool, botUsername: botUsername}
}

// Invite — созданное приглашение. Токен из BotURL в БД не хранится (там hex(sha256)).
type Invite struct {
	ID          uuid.UUID
	BotURL      string
	ExpiresAt   time.Time
	Texts       map[string]string // текст для WhatsApp на ru и ky
	Lang        string            // язык родителя: на нём WhatsAppURL
	WhatsAppURL string
	TgStatus    string // текущий статус связи (приглашение можно прислать и подключённому — например, новый телефон)
}

// CreateInvite создаёт одноразовое приглашение родителя на 30 дней. Прежние приглашения
// продолжают действовать до использования или срока.
func (s *Service) CreateInvite(ctx context.Context, actor, centerID, parentID uuid.UUID) (Invite, error) {
	if s.botUsername == "" {
		return Invite{}, ErrBotNotConfigured
	}
	raw, err := token.New()
	if err != nil {
		return Invite{}, err
	}
	var inv Invite
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		p, err := q.GetParent(ctx, sqlc.GetParentParams{CenterID: centerID, ID: parentID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		row, err := q.CreateParentInvite(ctx, sqlc.CreateParentInviteParams{
			Token: token.HashHex(raw), CenterID: centerID, ParentID: &p.ID, CreatedBy: &actor,
		})
		if err != nil {
			return fmt.Errorf("приглашение родителя: %w", err)
		}
		botURL := "https://t.me/" + s.botUsername + "?start=" + raw
		inv = Invite{
			ID: row.ID, BotURL: botURL, ExpiresAt: row.ExpiresAt, Lang: p.Lang, TgStatus: p.TgStatus,
			Texts: map[string]string{
				notify.LangRU: notify.For(notify.LangRU).Invite(p.Name, p.CenterName, botURL),
				notify.LangKY: notify.For(notify.LangKY).Invite(p.Name, p.CenterName, botURL),
			},
		}
		inv.WhatsAppURL = WhatsAppURL(p.Phone, notify.For(p.Lang).Invite(p.Name, p.CenterName, botURL))
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.InviteCreate, Entity: "invite", EntityID: &row.ID,
			After: map[string]any{"kind": "parent", "parent_id": p.ID, "expires_at": row.ExpiresAt},
		})
	})
	return inv, err
}

// WhatsAppURL — https://wa.me/<телефон без +>?text=<текст>. Пробелы — %20: «+» WhatsApp
// показал бы как есть.
func WhatsAppURL(phone, text string) string {
	return "https://wa.me/" + strings.TrimPrefix(phone, "+") + "?text=" + strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
}
