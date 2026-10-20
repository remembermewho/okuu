// Команда server — HTTP API Окуу: config → db pool → migrations → router → scheduler.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"okuu/internal/config"
	"okuu/internal/db"
	"okuu/internal/httpapi"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("сервер остановлен с ошибкой", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
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

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(httpapi.Deps{DB: pool, Logger: logger}),
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
