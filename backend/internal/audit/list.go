package audit

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/validate"
)

const (
	DefaultLimit = 50
	MaxLimit     = 200
	// SystemName — кто сделал запись, если user_id пустой (billing.monthly и другие задачи).
	SystemName = "Система"
)

// Filter — фильтры журнала. Action с точкой на конце («ledger.») — все действия с этим префиксом.
// From/To — даты включительно в часовом поясе центра.
type Filter struct {
	UserID *uuid.UUID
	Action string
	From   *civil.Date
	To     *civil.Date
	Limit  int
	Cursor string
}

// List — журнал центра, новые сверху; nextCursor == "" — конец. UserName пустой — запись системы (SystemName).
func List(ctx context.Context, db sqlc.DBTX, centerID uuid.UUID, f Filter) ([]sqlc.ListAuditRow, string, error) {
	q := sqlc.New(db)
	limit := f.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return nil, "", validate.Errorf("limit", "limit — от 1 до %d", MaxLimit)
	}
	p := sqlc.ListAuditParams{CenterID: centerID, UserID: f.UserID, Lim: int32(limit + 1)}
	if a := strings.TrimSpace(f.Action); a != "" {
		if strings.HasSuffix(a, ".") {
			p.ActionPrefix = &a
		} else {
			p.Action = &a
		}
	}
	if f.From != nil || f.To != nil {
		c, err := q.GetCenter(ctx, centerID)
		if err != nil {
			return nil, "", err
		}
		if f.From != nil {
			t, err := civil.StartOfDay(*f.From, c.Timezone)
			if err != nil {
				return nil, "", err
			}
			p.FromAt = &t
		}
		if f.To != nil {
			t, err := civil.StartOfDay(f.To.AddDays(1), c.Timezone)
			if err != nil {
				return nil, "", err
			}
			p.ToAt = &t
		}
		if f.From != nil && f.To != nil && f.To.Before(*f.From) {
			return nil, "", validate.Errorf("to", "Дата «по» раньше даты «с»")
		}
	}
	if f.Cursor != "" {
		id, err := strconv.ParseInt(f.Cursor, 10, 64)
		if err != nil || id <= 0 {
			return nil, "", validate.Errorf("cursor", "Некорректный cursor — возьмите next_cursor из прошлого ответа")
		}
		p.BeforeID = &id
	}
	rows, err := q.ListAudit(ctx, p)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = strconv.FormatInt(rows[limit-1].ID, 10)
	}
	return rows, next, nil
}
