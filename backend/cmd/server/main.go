// Команда server — HTTP API Окуу: config → db pool → migrations → router → scheduler.
//
//	server                  запустить HTTP-сервер (миграции применяются при старте)
//	server migrate [up]     применить миграции и выйти
//	server migrate status   показать состояние миграций
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/auth"
	"okuu/internal/config"
	"okuu/internal/db"
	"okuu/internal/httpapi"
	"okuu/internal/staff"
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
	}
	return fmt.Errorf("неизвестная команда %q (serve|migrate)", cmd)
}

func serve(ctx context.Context, logger *slog.Logger, cfg config.Config, pool *pgxpool.Pool) error {
	authSvc := auth.NewService(pool, auth.Options{CookieSecure: cfg.Env == "prod", Logger: logger})
	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewRouter(httpapi.Deps{
			DB:        pool,
			Auth:      authSvc,
			Staff:     staff.NewService(pool, authSvc),
			AppOrigin: cfg.AppOrigin,
			Logger:    logger,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

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
