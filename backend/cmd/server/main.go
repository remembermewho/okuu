// Команда server — HTTP API Окуу: config → db pool → migrations → router → scheduler.
//
//	server                  запустить HTTP-сервер (миграции применяются при старте)
//	server migrate [up]     применить миграции и выйти
//	server migrate status   показать состояние миграций
//	server billing run --center=<id> [--date=YYYY-MM-DD]
//	                        начисления billing.monthly по центру на дату (по умолчанию — сегодня в его поясе)
//	server user reset-link --phone=<телефон>
//	                        одноразовая ссылка сброса пароля на 24 часа (для поддержки)
//	server demo seed --phone=<телефон> --password=<пароль>
//	                        создать демо-центр для показа (прежний демо-центр удаляется)
//	server demo reset       пересоздать демо-центр с нуля с тем же владельцем и паролем
//	server demo message     текст настоящего напоминания (JSON) — для картинки бота на лендинге
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/alerts"
	"okuu/internal/attendance"
	"okuu/internal/auth"
	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/config"
	"okuu/internal/db"
	"okuu/internal/demo"
	"okuu/internal/groups"
	"okuu/internal/httpapi"
	"okuu/internal/importer"
	"okuu/internal/jobs"
	"okuu/internal/parents"
	"okuu/internal/payments"
	"okuu/internal/payments/finik"
	"okuu/internal/reminders"
	"okuu/internal/reports"
	"okuu/internal/settings"
	"okuu/internal/staff"
	"okuu/internal/students"
	"okuu/internal/tariffs"
	"okuu/internal/telegram"
	"okuu/internal/telegram/bot"
)

// shutdownTimeout — сколько ждём текущие запросы и фоновые задачи при остановке.
const shutdownTimeout = 20 * time.Second

func main() {
	// healthcheck — для HEALTHCHECK в Docker (в distroless нет curl): без БД и конфигурации.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	// demo message — для скриншотов лендинга: настоящее напоминание в JSON (текст и кнопки), без БД и .env.
	if len(os.Args) > 2 && os.Args[1] == "demo" && os.Args[2] == "message" {
		today, _ := civil.Today(time.Now(), "Asia/Bishkek")
		if err := json.NewEncoder(os.Stdout).Encode(demo.SampleReminder(today)); err != nil {
			os.Exit(1)
		}
		return
	}
	// .env читаем только локально: в prod переменные задаёт окружение.
	var dotenv string
	if os.Getenv("APP_ENV") != "prod" {
		path, err := config.LoadDotEnv(".env", "../.env")
		if err != nil {
			fmt.Fprintln(os.Stderr, "ошибка .env:", err)
			os.Exit(2)
		}
		dotenv = path
	}
	logger := newLogger(os.Getenv("APP_ENV"))
	if dotenv != "" {
		logger.Info("загружен .env", "path", dotenv)
	}

	cfg, err := config.Load()
	if err != nil {
		// Понятный список того, что не так, и выход без запуска.
		fmt.Fprintln(os.Stderr, "Ошибка конфигурации — сервер не запущен:")
		for _, line := range strings.Split(err.Error(), "\n") {
			fmt.Fprintln(os.Stderr, "  -", line)
		}
		os.Exit(2)
	}
	if err := run(logger, cfg, os.Args[1:]); err != nil {
		logger.Error("сервер остановлен с ошибкой", "err", err)
		os.Exit(1)
	}
}

// newLogger — JSON в prod (для сбора логов), текст локально.
func newLogger(env string) *slog.Logger {
	if env == "prod" {
		return slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

// healthcheck — GET /readyz своего же сервера (HTTP_ADDR); 0 — готов.
func healthcheck() int {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "HTTP_ADDR:", err)
		return 1
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/readyz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "readyz:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "readyz:", resp.Status)
		return 1
	}
	return 0
}

func run(logger *slog.Logger, cfg config.Config, args []string) error {

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
	case "user":
		if len(args) < 2 || args[1] != "reset-link" {
			return errors.New("использование: server user reset-link --phone=<телефон>")
		}
		return userResetLink(ctx, logger, cfg, pool, args[2:])
	case "demo":
		if len(args) < 2 || (args[1] != "seed" && args[1] != "reset") {
			return errors.New("использование: server demo seed --phone=<телефон> --password=<пароль> | server demo reset")
		}
		return demoCmd(ctx, logger, pool, args[1], args[2:])
	}
	return fmt.Errorf("неизвестная команда %q (serve|migrate|billing|user|demo)", cmd)
}

func serve(ctx context.Context, logger *slog.Logger, cfg config.Config, pool *pgxpool.Pool) error {
	authSvc := auth.NewService(pool, auth.Options{CookieSecure: cfg.Env == "prod", Logger: logger})
	studentsSvc := students.NewService(pool, time.Now)
	groupsSvc := groups.NewService(pool)
	deps := httpapi.Deps{
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
		Parents:    parents.NewService(pool, cfg.Telegram.Username),
		Pool:       pool,
		AppOrigin:  cfg.AppOrigin,
		Logger:     logger,

		TrustedProxies: cfg.TrustedProxies,
	}

	// Telegram-бот: токен уходит только в клиент; в логах — никогда.
	// Одна очередь отправки на бота и напоминания: лимиты Telegram — общие на бота.
	var (
		tgAPI  *telegram.Client
		tgBot  *bot.Bot
		sender *telegram.Sender // nil — Telegram выключен: напоминания не отправляются
	)
	// Оплата родителем: по умолчанию — реквизиты центра; Finik — за флагом (пока заглушка).
	var online payments.Provider
	if cfg.FinikEnabled {
		online = finik.New(cfg.FinikAPIKey.Reveal())
		logger.Warn("FINIK_ENABLED: провайдер Finik — заглушка, онлайн-оплата пока не работает")
	}
	paymentsSvc := payments.NewService(payments.Options{Pool: pool, Online: online, AppOrigin: cfg.AppOrigin, Now: time.Now, Logger: logger})
	deps.Payments = paymentsSvc

	if cfg.Telegram.Mode != config.TelegramOff {
		tgAPI = telegram.NewClient(cfg.Telegram.Token.Reveal(), telegram.ClientOptions{})
		sender = telegram.NewSender(tgAPI, telegram.SenderOptions{OnBlocked: bot.MarkBlocked(pool, logger), Logger: logger})
	}
	// Алерты команде: в служебный чат через того же бота; без бота или чата — только лог.
	var alertSender alerts.Sender
	if sender != nil {
		alertSender = sender
	}
	alerter := alerts.New(alerts.Options{Sender: alertSender, ChatID: cfg.AlertChatID, Env: cfg.Env, Logger: logger})
	deps.Alerts = alerter
	if cfg.Telegram.Mode != config.TelegramOff {
		tgBot = bot.New(bot.Options{Pool: pool, Sender: sender, Now: time.Now, Logger: logger, Payments: paymentsSvc, Alerts: alerter, Langs: cfg.BotLanguages})
		if cfg.Telegram.Mode == config.TelegramWebhook {
			deps.Bot = tgBot
			deps.TelegramWebhookSecret = cfg.Telegram.WebhookSecret.Reveal()
		}
	}
	remindersSvc := reminders.NewService(pool, sender, time.Now, logger).WithLangs(cfg.BotLanguages)
	deps.Reminders = remindersSvc

	scheduler := &jobs.Scheduler{Pool: pool, Logger: logger, Reminders: remindersSvc, Alerts: alerter}
	migrator, err := db.NewMigrator(pool)
	if err != nil {
		return err
	}
	defer migrator.Close()
	deps.Ready = func(ctx context.Context) map[string]error {
		checks := map[string]error{"scheduler": nil, "migrations": nil}
		if !scheduler.Started() {
			checks["scheduler"] = errors.New("планировщик не запущен")
		}
		if pending, err := migrator.HasPending(ctx); err != nil {
			checks["migrations"] = err
		} else if pending {
			checks["migrations"] = errors.New("есть неприменённые миграции")
		}
		return checks
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(deps),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second, // импорт до 5 МБ с мобильного интернета
		WriteTimeout:      60 * time.Second, // выгрузка отчёта в xlsx
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	// Фоновые задачи. stopBg — новых запусков не начинать; killBg — прервать текущие, если
	// они не закончились за срок остановки.
	stopCtx, stopBg := context.WithCancel(context.Background())
	workCtx, killBg := context.WithCancel(context.Background())
	defer killBg()
	var bg sync.WaitGroup
	start := func(fn func()) {
		bg.Add(1)
		go func() {
			defer bg.Done()
			fn()
		}()
	}

	// billing.monthly и reminders.auto раз в час (и сразу при старте).
	start(func() { scheduler.Run(stopCtx, workCtx) })
	// payments.reconcile раз в 15 минут: потерянные вебхуки, истёкшие ссылки.
	start(func() {
		jobs.RunReconcile(stopCtx, workCtx, pool, logger, alerter, paymentsSvc, jobs.ReconcileInterval)
	})
	// Алерты по журналу: discount_capped, payment.amount_mismatch.
	start(func() { alerter.WatchAudit(stopCtx, pool, time.Minute) })

	if tgBot != nil {
		// Меню команд бота (/start, /balance, /pay, /attendance, /help, /stop) — с русскими описаниями.
		if err := bot.SetupCommands(ctx, tgAPI, cfg.BotLanguages); err != nil {
			logger.Warn("telegram: не удалось задать меню команд", "err", err)
		}
		logger.Info("telegram: языки бота", "langs", cfg.BotLanguages.List())
		start(func() { tgBot.Run(stopCtx, workCtx) })
		// Событийные уведомления (paid, package_low, absent) — сразу после события.
		start(func() { jobs.RunDispatcher(stopCtx, workCtx, pool, logger, alerter, remindersSvc, time.Minute) })
		switch cfg.Telegram.Mode {
		case config.TelegramPolling:
			start(func() { bot.Poll(stopCtx, tgAPI, tgBot, logger) })
		case config.TelegramWebhook:
			// В адресе вебхука секрет — сам адрес не логируем.
			secret := cfg.Telegram.WebhookSecret.Reveal()
			hook := strings.TrimSuffix(cfg.AppOrigin, "/") + "/webhooks/telegram/" + secret
			if err := bot.SetupWebhook(ctx, tgAPI, hook, secret); err != nil {
				logger.Error("telegram: не удалось установить вебхук", "err", err)
				alerter.Alert("telegram:webhook", fmt.Sprintf("не удалось установить вебхук Telegram — бот не получает сообщения: %v", err))
			} else {
				logger.Info("telegram: бот запущен (webhook)", "origin", cfg.AppOrigin)
			}
		}
	} else {
		logger.Info("telegram: бот выключен (нет TELEGRAM_BOT_TOKEN или TELEGRAM_MODE=off)")
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP сервер запущен", "addr", cfg.HTTPAddr, "env", cfg.Env)
		errCh <- srv.ListenAndServe()
	}()

	var serveErr error
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = err
		}
	case <-ctx.Done():
	}

	// Остановка: HTTP дожидается текущих запросов, фоновые задачи — текущих запусков
	// (новые не начинаются). Всё вместе — не дольше shutdownTimeout.
	logger.Info("остановка сервера")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	stopBg()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("HTTP: не все запросы завершились", "err", err)
	}
	done := make(chan struct{})
	go func() {
		bg.Wait()
		close(done)
	}()
	select {
	case <-done:
		logger.Info("фоновые задачи остановлены")
	case <-shutdownCtx.Done():
		logger.Warn("фоновые задачи не завершились за срок — прерываем", "timeout", shutdownTimeout)
		killBg()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			logger.Error("фоновые задачи не остановились после прерывания")
		}
	}
	alerter.Wait()
	return serveErr
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

// userResetLink — для поддержки: одноразовая ссылка сброса пароля (24 часа) по телефону пользователя,
// в том числе владельцу центра. Печатает ссылку в stdout — передайте её человеку лично.
// Прежние неиспользованные ссылки этого пользователя перестают работать.
func userResetLink(ctx context.Context, logger *slog.Logger, cfg config.Config, pool *pgxpool.Pool, args []string) error {
	fs := flag.NewFlagSet("user reset-link", flag.ContinueOnError)
	phoneFlag := fs.String("phone", "", "телефон пользователя (+996XXXXXXXXX или 0XXXXXXXXX)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*phoneFlag) == "" {
		return errors.New("--phone: укажите телефон пользователя")
	}
	if err := migrateUp(ctx, logger, pool); err != nil {
		return err
	}
	res, err := auth.NewService(pool, auth.Options{Logger: logger}).IssueSupportPasswordReset(ctx, *phoneFlag)
	if err != nil {
		return err
	}
	fmt.Printf("Ссылка для смены пароля (%s), действует до %s:\n%s\n",
		res.UserName, res.ExpiresAt.Local().Format("02.01.2006 15:04"), httpapi.ResetURL(cfg.AppOrigin, res.Token))
	return nil
}

// demoCmd — демо-центр для показа: seed создаёт (с владельцем --phone), reset пересоздаёт.
// Удаляются только центры с is_demo; Telegram и напоминания для демо не работают.
func demoCmd(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, sub string, args []string) error {
	fs := flag.NewFlagSet("demo "+sub, flag.ContinueOnError)
	phoneFlag := fs.String("phone", "", "телефон владельца демо (+996XXXXXXXXX или 0XXXXXXXXX)")
	passwordFlag := fs.String("password", "", "пароль владельца демо (от 8 символов)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if sub == "seed" && (*phoneFlag == "" || *passwordFlag == "") {
		return errors.New("demo seed: укажите --phone и --password владельца демо")
	}
	if err := migrateUp(ctx, logger, pool); err != nil {
		return err
	}
	var (
		res demo.Result
		err error
	)
	if sub == "seed" {
		res, err = demo.Seed(ctx, pool, time.Now(), *phoneFlag, *passwordFlag)
	} else {
		res, err = demo.Reset(ctx, pool, time.Now())
	}
	if err != nil {
		return err
	}
	fmt.Printf("Готово: %s (id %s).\nВход: %s и пароль владельца. Учеников %d: с просрочкой %d, ожидают оплаты %d, ушли %d.\n",
		demo.CenterName, res.CenterID, res.OwnerPhone, res.Students, res.Overdue, res.Pending, res.Left)
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
