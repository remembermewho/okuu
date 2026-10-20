// Package db создаёт пул соединений PostgreSQL. Здесь же будет код, сгенерированный sqlc.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect открывает пул и проверяет соединение через Ping.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("разбор DATABASE_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("создание пула: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping БД: %w", err)
	}
	return pool, nil
}

// CenterTimezone — часовой пояс центра (для команд, которым нужно «сегодня» центра).
func CenterTimezone(ctx context.Context, pool *pgxpool.Pool, centerID uuid.UUID) (string, error) {
	var tz string
	if err := pool.QueryRow(ctx, `SELECT timezone FROM centers WHERE id = $1`, centerID).Scan(&tz); err != nil {
		return "", fmt.Errorf("центр %s: %w", centerID, err)
	}
	return tz, nil
}
