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
	"okuu/internal/auth"
	"okuu/internal/db/sqlc"
	"okuu/internal/notify"
	"okuu/internal/token"
	"okuu/internal/validate"
)

var (
	// ErrNotFound — родителя нет в этом центре.
	ErrNotFound = errors.New("родитель не найден")
	// ErrBotNotConfigured — не задано имя бота (TELEGRAM_BOT_USERNAME): ссылку не построить.
	ErrBotNotConfigured = errors.New("telegram-бот не настроен")
	// ErrDemoCenter — демо-центр: родители вымышленные, приглашения в бот не создаются.
	ErrDemoCenter = errors.New("демо-центр: Telegram не подключается")
)

// Service — приглашения родителей.
type Service struct {
	pool        *pgxpool.Pool
	botUsername string
	langs       notify.Langs // языки бота (BOT_LANGUAGES): текст приглашения — только на них
}

// NewService создаёт сервис; botUsername — имя бота без @ (пустое — приглашения недоступны).
func NewService(pool *pgxpool.Pool, botUsername string) *Service {
	return &Service{pool: pool, botUsername: botUsername}
}

// WithLangs — языки бота (BOT_LANGUAGES). Без вызова — только русский.
func (s *Service) WithLangs(l notify.Langs) *Service {
	s.langs = l
	return s
}

// Invite — созданное приглашение. Токен из BotURL в БД не хранится (там hex(sha256)).
type Invite struct {
	ID          uuid.UUID
	BotURL      string
	ExpiresAt   time.Time
	Texts       map[string]string // текст для WhatsApp на языках бота: ru всегда, ky — если включён
	Lang        string            // язык родителя (если включён, иначе ru): на нём WhatsAppURL
	WhatsAppURL string
	TgStatus    string // текущий статус связи (приглашение можно прислать и подключённому — например, новый телефон)
}

// CreateInvite создаёт одноразовое приглашение родителя на 30 дней. Прежние приглашения
// продолжают действовать до использования или срока.
func (s *Service) CreateInvite(ctx context.Context, actor, centerID, parentID uuid.UUID) (Invite, error) {
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
		// Демо-центр: родители вымышленные — к боту не подключаются.
		if c, err := q.GetCenter(ctx, centerID); err != nil {
			return err
		} else if c.IsDemo {
			return ErrDemoCenter
		}
		if s.botUsername == "" {
			return ErrBotNotConfigured
		}
		row, err := q.CreateParentInvite(ctx, sqlc.CreateParentInviteParams{
			Token: token.HashHex(raw), CenterID: centerID, ParentID: &p.ID, CreatedBy: &actor,
		})
		if err != nil {
			return fmt.Errorf("приглашение родителя: %w", err)
		}
		botURL := "https://t.me/" + s.botUsername + "?start=" + raw
		lang := s.langs.Resolve(p.Lang)
		inv = Invite{
			ID: row.ID, BotURL: botURL, ExpiresAt: row.ExpiresAt, Lang: lang, TgStatus: p.TgStatus,
			Texts: map[string]string{notify.LangRU: notify.For(notify.LangRU).Invite(p.Name, p.CenterName, botURL)},
		}
		if s.langs.Multi() {
			inv.Texts[notify.LangKY] = notify.For(notify.LangKY).Invite(p.Name, p.CenterName, botURL)
		}
		inv.WhatsAppURL = WhatsAppURL(p.Phone, inv.Texts[lang])
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.InviteCreate, Entity: "invite", EntityID: &row.ID,
			After: map[string]any{"kind": "parent", "parent_id": p.ID, "expires_at": row.ExpiresAt},
		})
	})
	return inv, err
}

// Found — существующий родитель центра по телефону: в форме ученика «Родитель найден: …».
type Found struct {
	ID       uuid.UUID
	Name     string
	Children []string // ученики родителя, кроме ушедших
}

// Lookup — родитель центра с этим телефоном (телефон в любом принятом виде) или nil, если такого нет.
func (s *Service) Lookup(ctx context.Context, centerID uuid.UUID, phone string) (*Found, error) {
	norm, err := auth.NormalizePhone(phone)
	var verr *validate.Error
	if errors.As(err, &verr) {
		return nil, validate.Errorf("phone", "%s", verr.Message)
	}
	if err != nil {
		return nil, err
	}
	q := sqlc.New(s.pool)
	p, err := q.GetParentByPhone(ctx, sqlc.GetParentByPhoneParams{CenterID: centerID, Phone: norm})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	children, err := q.ListParentChildNames(ctx, sqlc.ListParentChildNamesParams{CenterID: centerID, ParentID: &p.ID})
	if err != nil {
		return nil, err
	}
	return &Found{ID: p.ID, Name: p.Name, Children: children}, nil
}

// WhatsAppURL — https://wa.me/<телефон без +>?text=<текст>. Пробелы — %20: «+» WhatsApp
// показал бы как есть.
func WhatsAppURL(phone, text string) string {
	return "https://wa.me/" + strings.TrimPrefix(phone, "+") + "?text=" + strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
}
