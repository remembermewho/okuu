// Package jobs — фоновые задачи внутри сервера. Каждая идемпотентна и запускается под
// pg_try_advisory_lock: второй экземпляр сервера в это время просто пропускает запуск.
//
// Остановка в два контекста: stop — новых запусков не начинать (отменяется первым), work —
// контекст текущего запуска (отменяется, только если запуск не успел закончиться за срок
// остановки сервера). Ошибка запуска — в лог и алерт команде.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/alerts"
	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/payments"
	"okuu/internal/reminders"
)

// Ключи advisory lock задач ("okuu" + номер задачи).
const (
	BillingLockKey   int64 = 0x6f6b7575_00000001 // billing.monthly
	RemindersLockKey int64 = 0x6f6b7575_00000002 // reminders.auto
	DispatchLockKey  int64 = 0x6f6b7575_00000003 // отправка событийных уведомлений
	ReconcileLockKey int64 = 0x6f6b7575_00000004 // payments.reconcile
)

// ReconcileInterval — как часто payments.reconcile.
const ReconcileInterval = 15 * time.Minute

// failed — ошибка запуска задачи: лог и алерт (одинаковые — не чаще раза в 10 минут).
func failed(logger *slog.Logger, a *alerts.Alerter, job string, err error) {
	logger.Error(job+": запуск не удался", "err", err)
	a.Alert("job:"+job, fmt.Sprintf("фоновая задача %s не выполнилась: %v", job, err))
}

// RunReconcile — payments.reconcile раз в interval (и сразу при старте) до отмены stop.
func RunReconcile(stop, work context.Context, pool *pgxpool.Pool, logger *slog.Logger, a *alerts.Alerter, svc *payments.Service, interval time.Duration) {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = ReconcileInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := ReconcileOnce(work, pool, logger, svc); err != nil && work.Err() == nil {
			failed(logger, a, "payments.reconcile", err)
		}
		select {
		case <-stop.Done():
			return
		case <-t.C:
		}
	}
}

// ReconcileOnce — один запуск payments.reconcile под advisory lock.
func ReconcileOnce(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, svc *payments.Service) (bool, error) {
	unlock, ok, err := tryLock(ctx, pool, ReconcileLockKey)
	if err != nil || !ok {
		return false, err
	}
	defer unlock()
	res, err := svc.Reconcile(ctx)
	if res.Paid > 0 || res.Expired > 0 {
		logger.Info("payments.reconcile", "checked", res.Checked, "paid", res.Paid, "expired", res.Expired)
	}
	return true, err
}

// Scheduler — тикер раз в Interval; первый запуск — сразу при старте (рестарт в день начисления не пропустит его).
// После billing.monthly — reminders.auto (если задан Reminders): свежие начисления сразу видны напоминаниям.
type Scheduler struct {
	Pool      *pgxpool.Pool
	Logger    *slog.Logger
	Now       func() time.Time
	Interval  time.Duration
	Reminders *reminders.Service
	Alerts    *alerts.Alerter

	started atomic.Bool
}

// Started — планировщик запущен и ещё не остановлен (для /readyz).
func (s *Scheduler) Started() bool { return s.started.Load() }

// Run работает до отмены stop; текущий запуск выполняется с work.
func (s *Scheduler) Run(stop, work context.Context) {
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Interval == 0 {
		s.Interval = time.Hour
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	s.started.Store(true)
	defer s.started.Store(false)
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		if _, err := RunBillingOnce(work, s.Pool, s.Logger, s.Now()); err != nil && work.Err() == nil {
			failed(s.Logger, s.Alerts, "billing.monthly", err)
		}
		if s.Reminders != nil && stop.Err() == nil {
			if _, err := RunRemindersOnce(work, s.Pool, s.Logger, s.Reminders); err != nil && work.Err() == nil {
				failed(s.Logger, s.Alerts, "reminders.auto", err)
			}
		}
		select {
		case <-stop.Done():
			return
		case <-t.C:
		}
	}
}

// RunBillingOnce — один запуск billing.monthly по всем центрам, у которых наступил billing_day
// (в их часовом поясе). ran=false — задачу сейчас выполняет другой процесс. Ошибка одного
// центра не останавливает остальные; все они возвращаются вместе.
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
	var errs []error
	for _, c := range centers {
		today, err := civil.Today(now, c.Timezone)
		if err != nil {
			logger.Error("billing.monthly: часовой пояс центра", "center_id", c.ID, "err", err)
			errs = append(errs, fmt.Errorf("центр %s: %w", c.ID, err))
			continue
		}
		if today.Day() < billing.EffectiveBillingDay(c.BillingDay, c.BillingDayNext, c.BillingDayNextFrom, today.FirstOfMonth()) {
			continue
		}
		res, err := billing.RunMonthly(ctx, pool, logger, c.ID, today)
		if err != nil {
			logger.Error("billing.monthly: центр не обработан", "center_id", c.ID, "err", err)
			errs = append(errs, fmt.Errorf("центр %s: %w", c.ID, err))
			continue
		}
		if res.Failed > 0 {
			errs = append(errs, fmt.Errorf("центр %s: не начислено по %d записям (подробности в логе)", c.ID, res.Failed))
		}
		if res.Charged > 0 || res.Paused > 0 || res.Failed > 0 {
			logger.Info("billing.monthly", "center_id", c.ID, "period", res.Period,
				"charged", res.Charged, "paused", res.Paused, "skipped", res.Skipped, "failed", res.Failed)
		}
	}
	return true, errors.Join(errs...)
}

// RunRemindersOnce — один запуск reminders.auto под advisory lock. ran=false — задачу сейчас
// выполняет другой процесс.
func RunRemindersOnce(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, svc *reminders.Service) (ran bool, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	unlock, ok, err := tryLock(ctx, pool, RemindersLockKey)
	if err != nil || !ok {
		return false, err
	}
	defer unlock()
	res, err := svc.RunAuto(ctx)
	if res.Sent > 0 || res.Blocked > 0 || res.Failed > 0 {
		logger.Info("reminders.auto", "sent", res.Sent, "blocked", res.Blocked, "failed", res.Failed,
			"limited", res.Limited, "duplicate", res.Duplicate)
	}
	return true, err
}

// DispatchOnce — отправка событийных уведомлений под advisory lock.
func DispatchOnce(ctx context.Context, pool *pgxpool.Pool, svc *reminders.Service) (ran bool, sent int, err error) {
	unlock, ok, err := tryLock(ctx, pool, DispatchLockKey)
	if err != nil || !ok {
		return false, 0, err
	}
	defer unlock()
	sent, err = svc.Dispatch(ctx)
	return true, sent, err
}

// notifyChannel — канал pg_notify, в который пишет notify.Enqueue.
const notifyChannel = "okuu_notify"

// RunDispatcher отправляет событийные уведомления сразу после события: слушает LISTEN okuu_notify
// и, кроме того, проверяет очередь раз в interval (ночные события уходят утром, неудачные — повторяются).
// Работает до отмены stop; потерянное соединение переподключает.
func RunDispatcher(stop, work context.Context, pool *pgxpool.Pool, logger *slog.Logger, a *alerts.Alerter, svc *reminders.Service, interval time.Duration) {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = time.Minute
	}
	for stop.Err() == nil {
		err := listen(stop, work, pool, logger, a, svc, interval)
		if stop.Err() != nil {
			return
		}
		logger.Warn("уведомления: LISTEN прервался, переподключение", "err", err)
		select {
		case <-stop.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func listen(stop, work context.Context, pool *pgxpool.Pool, logger *slog.Logger, a *alerts.Alerter, svc *reminders.Service, interval time.Duration) error {
	conn, err := pool.Acquire(stop)
	if err != nil {
		return err
	}
	broken := false
	defer func() {
		if broken {
			conn.Hijack().Close(context.Background()) // соединение в неизвестном состоянии — в пул не возвращаем
			return
		}
		uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(uctx, "UNLISTEN *") // в пул — без подписки
		conn.Release()
	}()
	if _, err := conn.Exec(stop, "LISTEN "+notifyChannel); err != nil {
		broken = true
		return err
	}
	for {
		if _, sent, err := DispatchOnce(work, pool, svc); err != nil && work.Err() == nil {
			failed(logger, a, "notify.dispatch", err)
		} else if sent > 0 {
			logger.Info("уведомления отправлены", "sent", sent)
		}
		wctx, cancel := context.WithTimeout(stop, interval)
		_, err := conn.Conn().WaitForNotification(wctx)
		cancel()
		switch {
		case stop.Err() != nil:
			return nil
		case err != nil && !errors.Is(err, context.DeadlineExceeded):
			broken = true
			return err
		}
	}
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
