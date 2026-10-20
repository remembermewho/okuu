// Package audit — журнал действий (F8). Запись идёт в той же транзакции, что и само изменение:
// откатилось изменение — откатилась и запись.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"okuu/internal/db/sqlc"
)

// Действия журнала.
const (
	InviteCreate    = "invite.create"
	InviteRevoke    = "invite.revoke"
	InviteAccept    = "invite.accept"
	StaffRoleChange = "staff.role_change"
	StaffRemove     = "staff.remove"
)

// Entry — одна запись журнала. Before/After сериализуются в jsonb (nil → NULL).
type Entry struct {
	CenterID uuid.UUID
	UserID   *uuid.UUID // кто сделал; nil — система
	Action   string
	Entity   string
	EntityID *uuid.UUID
	Before   any
	After    any
}

// Record пишет запись в audit_log через tx (pgx.Tx или пул).
func Record(ctx context.Context, tx sqlc.DBTX, e Entry) error {
	before, err := marshal(e.Before)
	if err != nil {
		return fmt.Errorf("audit %s: before: %w", e.Action, err)
	}
	after, err := marshal(e.After)
	if err != nil {
		return fmt.Errorf("audit %s: after: %w", e.Action, err)
	}
	if err := sqlc.New(tx).InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		CenterID: e.CenterID,
		UserID:   e.UserID,
		Action:   e.Action,
		Entity:   e.Entity,
		EntityID: e.EntityID,
		Before:   before,
		After:    after,
	}); err != nil {
		return fmt.Errorf("audit %s: %w", e.Action, err)
	}
	return nil
}

func marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
