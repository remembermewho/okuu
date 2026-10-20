package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"okuu/migrations"
)

// Migrator применяет встроенные миграции goose к базе.
type Migrator struct {
	p *goose.Provider
}

// NewMigrator создаёт мигратор поверх пула. Advisory lock не даёт двум
// экземплярам сервера мигрировать одновременно. Close пул не закрывает.
func NewMigrator(pool *pgxpool.Pool) (*Migrator, error) {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, stdlib.OpenDBFromPool(pool), migrations.FS,
		goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("goose: %w", err)
	}
	return &Migrator{p: p}, nil
}

// Up применяет все неприменённые миграции и возвращает их результаты.
func (m *Migrator) Up(ctx context.Context) ([]*goose.MigrationResult, error) {
	res, err := m.p.Up(ctx)
	if err != nil {
		return res, fmt.Errorf("миграции: %w", err)
	}
	return res, nil
}

// UpTo применяет миграции до версии version включительно (для тестов миграций с данными).
func (m *Migrator) UpTo(ctx context.Context, version int64) ([]*goose.MigrationResult, error) {
	return m.p.UpTo(ctx, version)
}

// DownTo откатывает миграции до версии version (0 — всё).
func (m *Migrator) DownTo(ctx context.Context, version int64) ([]*goose.MigrationResult, error) {
	return m.p.DownTo(ctx, version)
}

// Status возвращает состояние каждой миграции.
func (m *Migrator) Status(ctx context.Context) ([]*goose.MigrationStatus, error) {
	return m.p.Status(ctx)
}

// Version возвращает текущую версию схемы.
func (m *Migrator) Version(ctx context.Context) (int64, error) {
	return m.p.GetDBVersion(ctx)
}

// Close освобождает *sql.DB-обёртку; сам пул остаётся открытым.
func (m *Migrator) Close() error { return m.p.Close() }

// Migrate — короткая форма для старта сервера: применить всё и закрыть мигратор.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]*goose.MigrationResult, error) {
	m, err := NewMigrator(pool)
	if err != nil {
		return nil, err
	}
	defer m.Close()
	return m.Up(ctx)
}
