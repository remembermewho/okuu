package auth

import (
	"context"

	"okuu/internal/db/sqlc"
)

// DeleteExpiredSessions удаляет просроченные сессии и возвращает их число.
// Вызывается при логине; позже — периодической задачей в jobs.
// db — *pgxpool.Pool или pgx.Tx.
func DeleteExpiredSessions(ctx context.Context, db sqlc.DBTX) (int64, error) {
	return sqlc.New(db).DeleteExpiredSessions(ctx)
}
