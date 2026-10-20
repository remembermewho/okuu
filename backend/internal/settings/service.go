// Package settings — настройки центра (S8): название, телефон, часовой пояс, день начисления,
// срок оплаты, правило «уважительной», напоминания, реквизиты. Ключи Finik здесь не читаются и не пишутся.
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/audit"
	"okuu/internal/auth"
	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/optional"
	"okuu/internal/validate"
)

const (
	MaxDueDays       = 60
	MaxPayDetailsLen = 1000
)

// Settings — настройки центра для экрана S8.
type Settings struct {
	Name               string
	Phone              *string
	Timezone           string
	BillingDay         int         // действует сейчас
	BillingDayNext     *int        // новый день начисления, ещё не вступивший в силу
	BillingDayNextFrom *civil.Date // с какого периода он действует
	DueDays            int
	ExcusedDeducts     bool
	RemindersOn        bool
	PayDetails         *string
	Plan               string
	FinikConnected     bool
}

// Patch — частичное изменение настроек.
type Patch struct {
	Name           optional.Field[string]
	Phone          optional.Field[string]
	Timezone       optional.Field[string]
	BillingDay     optional.Field[int]
	DueDays        optional.Field[int]
	ExcusedDeducts optional.Field[bool]
	RemindersOn    optional.Field[bool]
	PayDetails     optional.Field[string]
}

// Service — настройки.
type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
	now  func() time.Time
}

// NewService создаёт сервис; now — часы (nil — time.Now).
func NewService(pool *pgxpool.Pool, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, q: sqlc.New(pool), now: now}
}

// Get — текущие настройки. Отложенный день начисления, чей период уже наступил, показывается действующим.
func (s *Service) Get(ctx context.Context, centerID uuid.UUID) (Settings, error) {
	c, err := s.q.GetCenter(ctx, centerID)
	if err != nil {
		return Settings{}, err
	}
	return s.view(c)
}

// Update меняет настройки (только владелец). Новый billing_day действует со следующего периода:
// текущий месяц начисляется по старому дню (R3–R4), с 1-го числа следующего — по новому.
func (s *Service) Update(ctx context.Context, actor, centerID uuid.UUID, p Patch) (Settings, error) {
	var out Settings
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		c, err := q.GetCenterForUpdate(ctx, centerID)
		if err != nil {
			return err
		}
		before, err := s.view(c)
		if err != nil {
			return err
		}
		today, err := civil.Today(s.now(), c.Timezone)
		if err != nil {
			return err
		}
		next := before // уже с учётом наступившего отложенного дня

		if p.Name.Set {
			if p.Name.Value == nil {
				return validate.Errorf("name", "Название — обязательное поле")
			}
			if next.Name, err = validate.Name("name", "Название", *p.Name.Value); err != nil {
				return err
			}
		}
		if p.Phone.Set {
			next.Phone = nil
			if p.Phone.Value != nil && strings.TrimSpace(*p.Phone.Value) != "" {
				phone, err := auth.NormalizePhone(*p.Phone.Value)
				if err != nil {
					return err
				}
				next.Phone = &phone
			}
		}
		if p.Timezone.Set {
			if p.Timezone.Value == nil || !civil.ValidTimezone(*p.Timezone.Value) {
				return validate.Errorf("timezone", "Часовой пояс — из базы IANA, например Asia/Bishkek")
			}
			next.Timezone = *p.Timezone.Value
		}
		if p.BillingDay.Set {
			if p.BillingDay.Value == nil || *p.BillingDay.Value < 1 || *p.BillingDay.Value > 28 {
				return validate.Errorf("billing_day", "День начисления — от 1 до 28")
			}
			if d := *p.BillingDay.Value; d == next.BillingDay {
				next.BillingDayNext, next.BillingDayNextFrom = nil, nil // вернули текущий — отложенное отменяется
			} else {
				from := today.FirstOfMonth().AddMonths(1)
				next.BillingDayNext, next.BillingDayNextFrom = &d, &from
			}
		}
		if p.DueDays.Set {
			if p.DueDays.Value == nil || *p.DueDays.Value < 0 || *p.DueDays.Value > MaxDueDays {
				return validate.Errorf("due_days", "Срок оплаты — от 0 до %d дней", MaxDueDays)
			}
			next.DueDays = *p.DueDays.Value
		}
		if p.ExcusedDeducts.Set {
			if p.ExcusedDeducts.Value == nil {
				return validate.Errorf("excused_deducts", "excused_deducts — true или false")
			}
			next.ExcusedDeducts = *p.ExcusedDeducts.Value
		}
		if p.RemindersOn.Set {
			if p.RemindersOn.Value == nil {
				return validate.Errorf("reminders_on", "reminders_on — true или false")
			}
			next.RemindersOn = *p.RemindersOn.Value
		}
		if p.PayDetails.Set {
			next.PayDetails = nil
			if p.PayDetails.Value != nil {
				v := strings.TrimSpace(*p.PayDetails.Value)
				if utf8.RuneCountInString(v) > MaxPayDetailsLen || strings.ContainsRune(v, utf8.RuneError) {
					return validate.Errorf("pay_details", "Реквизиты — до %d символов в UTF-8", MaxPayDetailsLen)
				}
				if v != "" {
					next.PayDetails = &v
				}
			}
		}

		if err := q.UpdateCenterSettings(ctx, sqlc.UpdateCenterSettingsParams{
			ID: centerID, Name: next.Name, Phone: next.Phone, Timezone: next.Timezone,
			BillingDay: int16(next.BillingDay), BillingDayNext: int16Ptr(next.BillingDayNext), BillingDayNextFrom: next.BillingDayNextFrom,
			DueDays: int16(next.DueDays), ExcusedDeducts: next.ExcusedDeducts, RemindersOn: next.RemindersOn, PayDetails: next.PayDetails,
		}); err != nil {
			return err
		}
		out = next
		if b, a := snapshot(before), snapshot(next); !sameJSON(b, a) {
			return audit.Record(ctx, tx, audit.Entry{
				CenterID: centerID, UserID: &actor, Action: audit.CenterUpdate, Entity: "center", EntityID: &centerID,
				Before: b, After: a,
			})
		}
		return nil
	})
	return out, err
}

// view — настройки из строки centers; отложенный день, чей период наступил, становится действующим.
func (s *Service) view(c sqlc.Center) (Settings, error) {
	today, err := civil.Today(s.now(), c.Timezone)
	if err != nil {
		return Settings{}, err
	}
	st := Settings{
		Name: c.Name, Phone: c.Phone, Timezone: c.Timezone,
		BillingDay: billing.EffectiveBillingDay(c.BillingDay, c.BillingDayNext, c.BillingDayNextFrom, today.FirstOfMonth()),
		DueDays:    int(c.DueDays), ExcusedDeducts: c.ExcusedDeducts, RemindersOn: c.RemindersOn,
		PayDetails: c.PayDetails, Plan: c.Plan, FinikConnected: len(c.FinikCreds) > 0,
	}
	if c.BillingDayNext != nil && c.BillingDayNextFrom != nil && today.FirstOfMonth().Before(*c.BillingDayNextFrom) {
		d := int(*c.BillingDayNext)
		st.BillingDayNext, st.BillingDayNextFrom = &d, c.BillingDayNextFrom
	}
	return st, nil
}

func snapshot(s Settings) map[string]any {
	return map[string]any{
		"name": s.Name, "phone": s.Phone, "timezone": s.Timezone, "billing_day": s.BillingDay,
		"billing_day_next": s.BillingDayNext, "billing_day_next_from": s.BillingDayNextFrom, "due_days": s.DueDays,
		"excused_deducts": s.ExcusedDeducts, "reminders_on": s.RemindersOn, "pay_details": s.PayDetails,
	}
}

func int16Ptr(v *int) *int16 {
	if v == nil {
		return nil
	}
	x := int16(*v)
	return &x
}

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

func sameJSON(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}
