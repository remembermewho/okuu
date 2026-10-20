// Package jobs — фоновые задачи внутри сервера. Каждая идемпотентна и запускается под
// pg_try_advisory_lock: второй экземпляр сервера в это время просто пропускает запуск.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
)

// BillingLockKey — ключ advisory lock задачи billing.monthly ("okuu" + номер задачи).
const BillingLockKey int64 = 0x6f6b7575_00000001

// Scheduler — тикер раз в Interval; первый запуск — сразу при старте (рестарт в день начисления не пропустит его).
type Scheduler struct {
	Pool     *pgxpool.Pool
	Logger   *slog.Logger
	Now      func() time.Time
	Interval time.Duration
}

// Run работает до отмены ctx.
func (s *Scheduler) Run(ctx context.Context) {
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Interval == 0 {
		s.Interval = time.Hour
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		if _, err := RunBillingOnce(ctx, s.Pool, s.Logger, s.Now()); err != nil && ctx.Err() == nil {
			s.Logger.Error("billing.monthly: запуск не удался", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunBillingOnce — один запуск billing.monthly по всем центрам, у которых наступил billing_day
// (в их часовом поясе). ran=false — задачу сейчас выполняет другой процесс.
func RunBillingOnce(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, now time.Time) (ran bool, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	unlock, ok, err := tryLock(ctx, pool, BillingLockKey)
	if err != nil || !ok {
		return false, err
	}
	defer unlock()

	centers, err := sqlc.New(pool).ListCentersForBilling(ctx)
	if err != nil {
		return true, err
	}
	for _, c := range centers {
		today, err := civil.Today(now, c.Timezone)
		if err != nil {
			logger.Error("billing.monthly: часовой пояс центра", "center_id", c.ID, "err", err)
			continue
		}
		if today.Day() < int(c.BillingDay) {
			continue
		}
		res, err := billing.RunMonthly(ctx, pool, logger, c.ID, today)
		if err != nil {
			logger.Error("billing.monthly: центр не обработан", "center_id", c.ID, "err", err)
			continue
		}
		if res.Charged > 0 || res.Paused > 0 || res.Failed > 0 {
			logger.Info("billing.monthly", "center_id", c.ID, "period", res.Period,
				"charged", res.Charged, "paused", res.Paused, "skipped", res.Skipped, "failed", res.Failed)
		}
	}
	return true, nil
}

// tryLock берёт сессионный advisory lock на отдельном соединении пула. Соединение держится
// до unlock; если снять блокировку не удалось, соединение закрывается — вместе с сессией уходит и lock.
func tryLock(ctx context.Context, pool *pgxpool.Pool, key int64) (unlock func(), ok bool, err error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&ok); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("advisory lock: %w", err)
	}
	if !ok {
		conn.Release()
		return nil, false, nil
	}
	return func() {
		uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(uctx, `SELECT pg_advisory_unlock($1)`, key); err != nil {
			_ = conn.Conn().Close(uctx)
		}
		conn.Release()
	}, true, nil
}
