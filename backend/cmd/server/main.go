// Команда server — HTTP API Окуу: config → db pool → migrations → router → scheduler.
//
//	server                  запустить HTTP-сервер (миграции применяются при старте)
//	server migrate [up]     применить миграции и выйти
//	server migrate status   показать состояние миграций
//	server billing run --center=<id> [--date=YYYY-MM-DD]
//	                        начисления billing.monthly по центру на дату (по умолчанию — сегодня в его поясе)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/attendance"
	"okuu/internal/auth"
	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/config"
	"okuu/internal/db"
	"okuu/internal/groups"
	"okuu/internal/httpapi"
	"okuu/internal/importer"
	"okuu/internal/jobs"
	"okuu/internal/reports"
	"okuu/internal/settings"
	"okuu/internal/staff"
	"okuu/internal/students"
	"okuu/internal/tariffs"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger, os.Args[1:]); err != nil {
		logger.Error("сервер остановлен с ошибкой", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, args []string) error {
	// .env читаем только локально: в prod переменные задаёт окружение.
	if os.Getenv("APP_ENV") != "prod" {
		path, err := config.LoadDotEnv(".env", "../.env")
		if err != nil {
			return err
		}
		if path != "" {
			logger.Info("загружен .env", "path", path)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "serve":
		if err := migrateUp(ctx, logger, pool); err != nil {
			return err
		}
		return serve(ctx, logger, cfg, pool)
	case "migrate":
		sub := "up"
		if len(args) > 1 {
			sub = args[1]
		}
		switch sub {
		case "up":
			return migrateUp(ctx, logger, pool)
		case "status":
			return migrateStatus(ctx, pool)
		}
		return fmt.Errorf("неизвестная команда migrate %q (up|status)", sub)
	case "billing":
		if len(args) < 2 || args[1] != "run" {
			return errors.New("использование: server billing run --center=<id> [--date=YYYY-MM-DD]")
		}
		return billingRun(ctx, logger, pool, args[2:])
	}
	return fmt.Errorf("неизвестная команда %q (serve|migrate|billing)", cmd)
}

func serve(ctx context.Context, logger *slog.Logger, cfg config.Config, pool *pgxpool.Pool) error {
	authSvc := auth.NewService(pool, auth.Options{CookieSecure: cfg.Env == "prod", Logger: logger})
	studentsSvc := students.NewService(pool, time.Now)
	groupsSvc := groups.NewService(pool)
	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewRouter(httpapi.Deps{
			DB:         pool,
			Auth:       authSvc,
			Staff:      staff.NewService(pool, authSvc),
			Tariffs:    tariffs.NewService(pool),
			Groups:     groupsSvc,
			Students:   studentsSvc,
			Billing:    billing.NewService(pool, time.Now, logger),
			Attendance: attendance.NewService(pool, time.Now),
			Importer:   importer.NewService(pool, studentsSvc, groupsSvc, time.Now),
			Settings:   settings.NewService(pool, time.Now),
			Reports:    reports.NewService(pool, time.Now),
			Pool:       pool,
			AppOrigin:  cfg.AppOrigin,
			Logger:     logger,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Фоновые задачи: billing.monthly раз в час (и сразу при старте).
	go (&jobs.Scheduler{Pool: pool, Logger: logger}).Run(ctx)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP сервер запущен", "addr", cfg.HTTPAddr, "env", cfg.Env)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	logger.Info("остановка сервера")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func migrateUp(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool) error {
	m, err := db.NewMigrator(pool)
	if err != nil {
		return err
	}
	defer m.Close()

	res, err := m.Up(ctx)
	for _, r := range res {
		logger.Info("миграция применена", "version", r.Source.Version,
			"file", filepath.Base(r.Source.Path), "duration", r.Duration)
	}
	if err != nil {
		return err
	}
	v, err := m.Version(ctx)
	if err != nil {
		return err
	}
	logger.Info("схема БД актуальна", "version", v)
	return nil
}

func migrateStatus(ctx context.Context, pool *pgxpool.Pool) error {
	m, err := db.NewMigrator(pool)
	if err != nil {
		return err
	}
	defer m.Close()

	st, err := m.Status(ctx)
	if err != nil {
		return err
	}
	for _, s := range st {
		applied := "-"
		if !s.AppliedAt.IsZero() {
			applied = s.AppliedAt.Local().Format(time.DateTime)
		}
		fmt.Printf("%-4d %-8s %-20s %s\n", s.Source.Version, s.State, applied, filepath.Base(s.Source.Path))
	}
	return nil
}

// billingRun — ручной запуск billing.monthly по одному центру (для проверки и разбора).
// Начисляет период даты --date; повторный запуск ничего не дублирует.
func billingRun(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, args []string) error {
	fs := flag.NewFlagSet("billing run", flag.ContinueOnError)
	centerFlag := fs.String("center", "", "id центра (обязательно)")
	dateFlag := fs.String("date", "", "дата YYYY-MM-DD (по умолчанию — сегодня в часовом поясе центра)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	centerID, err := uuid.Parse(*centerFlag)
	if err != nil {
		return fmt.Errorf("--center: нужен id центра: %w", err)
	}
	if err := migrateUp(ctx, logger, pool); err != nil {
		return err
	}
	var today civil.Date
	if *dateFlag != "" {
		if today, err = civil.Parse(*dateFlag); err != nil {
			return fmt.Errorf("--date: %w", err)
		}
	} else {
		center, err := db.CenterTimezone(ctx, pool, centerID)
		if err != nil {
			return err
		}
		if today, err = civil.Today(time.Now(), center); err != nil {
			return err
		}
	}
	res, err := billing.RunMonthly(ctx, pool, logger, centerID, today)
	if err != nil {
		return err
	}
	fmt.Printf("период %s (дата %s): начислено %d, пауза %d, пропущено %d, ошибок %d\n",
		res.Period, today, res.Charged, res.Paused, res.Skipped, res.Failed)
	return nil
}
