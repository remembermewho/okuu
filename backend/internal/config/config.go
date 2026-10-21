// Package config читает настройки приложения из переменных окружения.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"

	"okuu/internal/notify"
)

const (
	minKeyLen = 32
	// devOrigin — адрес Vite dev-сервера по умолчанию.
	devOrigin = "http://localhost:5173"
)

// Config — настройки процесса. Секреты приходят только из env.
type Config struct {
	Env           string // dev | test | prod
	DatabaseURL   string
	HTTPAddr      string
	AppOrigin     string // разрешённый Origin для изменяющих запросов (CSRF)
	EncryptionKey []byte
	Telegram      Telegram
	// FinikEnabled — онлайн-оплата через Finik (FINIK_ENABLED). Пока провайдер — заглушка:
	// без него оплата идёт по реквизитам центра.
	FinikEnabled bool
	FinikAPIKey  Secret
	// TrustedProxies — прокси (Caddy), от которых верим X-Forwarded-For (TRUSTED_PROXIES, CIDR через запятую).
	TrustedProxies []netip.Prefix
	// AlertChatID — служебный Telegram-чат для алертов (ALERT_TELEGRAM_CHAT_ID); 0 — только лог.
	AlertChatID int64
	// BotLanguages — языки сообщений родителям (BOT_LANGUAGES: «ru» по умолчанию или «ru,ky»).
	BotLanguages notify.Langs
}

// Режимы Telegram-бота.
const (
	TelegramOff     = "off"     // бот не запущен (нет токена)
	TelegramPolling = "polling" // dev: long polling (getUpdates)
	TelegramWebhook = "webhook" // prod: POST /webhooks/telegram/{secret}
)

// Telegram — настройки бота. Token и WebhookSecret — Secret: в логах и при печати скрыты.
type Telegram struct {
	Mode          string
	Token         Secret
	Username      string // без @: для ссылок t.me/<username>?start=<токен>
	WebhookSecret Secret // секрет в пути вебхука и в X-Telegram-Bot-Api-Secret-Token
}

// Secret — значение-секрет. String, GoString и LogValue его скрывают; само значение — Reveal.
type Secret string

func (s Secret) String() string               { return s.mask() }
func (s Secret) GoString() string             { return s.mask() }
func (s Secret) LogValue() slog.Value         { return slog.StringValue(s.mask()) }
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.mask()), nil }

// Reveal возвращает сам секрет — только для передачи туда, где он нужен (запрос к API).
func (s Secret) Reveal() string { return string(s) }

func (s Secret) mask() string {
	if s == "" {
		return ""
	}
	return "[скрыто]"
}

// IsDev сообщает, что приложение запущено локально.
func (c Config) IsDev() bool { return c.Env == "dev" }

// Load читает конфигурацию из окружения и проверяет её.
func Load() (Config, error) {
	cfg := Config{
		Env:         getenv("APP_ENV", "dev"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
	}

	var errs []error
	switch cfg.Env {
	case "dev", "test", "prod":
	default:
		errs = append(errs, fmt.Errorf("APP_ENV: неизвестное значение %q (dev|test|prod)", cfg.Env))
	}
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL не задан"))
	}

	cfg.AppOrigin = os.Getenv("APP_ORIGIN")
	if cfg.AppOrigin == "" && cfg.Env != "prod" {
		cfg.AppOrigin = devOrigin
	}
	if err := validateOrigin(cfg.AppOrigin); err != nil {
		errs = append(errs, err)
	}

	var err error
	if cfg.EncryptionKey, err = loadKey("ENCRYPTION_KEY", cfg.Env == "prod"); err != nil {
		errs = append(errs, err)
	}
	if cfg.Telegram, err = loadTelegram(cfg.Env); err != nil {
		errs = append(errs, err)
	}
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv("FINIK_ENABLED"))); v {
	case "", "0", "false", "no":
	case "1", "true", "yes":
		cfg.FinikEnabled = true
	default:
		errs = append(errs, fmt.Errorf("FINIK_ENABLED: ожидается true или false, получено %q", v))
	}
	cfg.FinikAPIKey = Secret(strings.TrimSpace(os.Getenv("FINIK_API_KEY")))

	if cfg.TrustedProxies, err = ParseCIDRs(os.Getenv("TRUSTED_PROXIES")); err != nil {
		errs = append(errs, fmt.Errorf("TRUSTED_PROXIES: %w", err))
	}
	if v := strings.TrimSpace(os.Getenv("ALERT_TELEGRAM_CHAT_ID")); v != "" {
		if cfg.AlertChatID, err = strconv.ParseInt(v, 10, 64); err != nil || cfg.AlertChatID == 0 {
			errs = append(errs, fmt.Errorf("ALERT_TELEGRAM_CHAT_ID: ожидается id чата (число, у групп — отрицательное), получено %q", v))
		}
	}
	if cfg.BotLanguages, err = notify.ParseLangs(os.Getenv("BOT_LANGUAGES")); err != nil {
		errs = append(errs, fmt.Errorf("BOT_LANGUAGES: %w", err))
	}
	if cfg.Env == "prod" {
		errs = append(errs, prodChecks(cfg)...)
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// prodChecks — то, без чего prod не запускается: HTTPS-адрес фронтенда (cookie Secure, вебхук
// Telegram), вебхук вместо polling. ENCRYPTION_KEY и секрет вебхука проверяются выше.
func prodChecks(cfg Config) []error {
	var errs []error
	if cfg.AppOrigin != "" && !strings.HasPrefix(cfg.AppOrigin, "https://") {
		errs = append(errs, fmt.Errorf("APP_ORIGIN: в prod нужен https://, получено %q", cfg.AppOrigin))
	}
	if cfg.Telegram.Mode != TelegramWebhook {
		errs = append(errs, fmt.Errorf("TELEGRAM_MODE: в prod только webhook, получено %q (задайте TELEGRAM_BOT_TOKEN, TELEGRAM_BOT_USERNAME, TELEGRAM_WEBHOOK_SECRET)", cfg.Telegram.Mode))
	}
	return errs
}

// ParseCIDRs разбирает список «10.0.0.0/8, 172.30.0.0/24, 127.0.0.1» (адрес без маски — один хост).
func ParseCIDRs(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			a, err := netip.ParseAddr(part)
			if err != nil {
				return nil, fmt.Errorf("%q — не адрес и не CIDR", part)
			}
			out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, fmt.Errorf("%q — не CIDR", part)
		}
		if p.Addr().Is4In6() {
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// loadKey декодирует ключ из base64. Пустой ключ допустим только вне prod.
func loadKey(name string, required bool) ([]byte, error) {
	v := os.Getenv(name)
	if v == "" {
		if required {
			return nil, fmt.Errorf("%s не задан", name)
		}
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("%s: ожидается base64: %w", name, err)
	}
	if len(key) < minKeyLen {
		return nil, fmt.Errorf("%s: нужно минимум %d байт, получено %d", name, minKeyLen, len(key))
	}
	return key, nil
}

// loadTelegram читает TELEGRAM_*. Без токена бот выключен; с токеном режим по умолчанию —
// polling вне prod и webhook в prod. Ошибки не содержат значений секретов.
func loadTelegram(env string) (Telegram, error) {
	tg := Telegram{
		Mode:          os.Getenv("TELEGRAM_MODE"),
		Token:         Secret(strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))),
		Username:      strings.TrimPrefix(strings.TrimSpace(os.Getenv("TELEGRAM_BOT_USERNAME")), "@"),
		WebhookSecret: Secret(strings.TrimSpace(os.Getenv("TELEGRAM_WEBHOOK_SECRET"))),
	}
	if tg.Mode == "" {
		switch {
		case tg.Token == "":
			tg.Mode = TelegramOff
		case env == "prod":
			tg.Mode = TelegramWebhook
		default:
			tg.Mode = TelegramPolling
		}
	}

	var errs []error
	switch tg.Mode {
	case TelegramOff:
	case TelegramPolling, TelegramWebhook:
		if tg.Token == "" {
			errs = append(errs, fmt.Errorf("TELEGRAM_BOT_TOKEN не задан (TELEGRAM_MODE=%s)", tg.Mode))
		}
		if tg.Username == "" {
			errs = append(errs, errors.New("TELEGRAM_BOT_USERNAME не задан: нужен для ссылок-приглашений"))
		}
	default:
		errs = append(errs, fmt.Errorf("TELEGRAM_MODE: неизвестное значение %q (off|polling|webhook)", tg.Mode))
	}
	if tg.Username != "" && !validUsername(tg.Username) {
		errs = append(errs, errors.New("TELEGRAM_BOT_USERNAME: ожидается имя бота (латиница, цифры, _), например okuu_bot"))
	}
	if tg.Mode == TelegramWebhook && tg.WebhookSecret == "" {
		errs = append(errs, errors.New("TELEGRAM_WEBHOOK_SECRET не задан (нужен для TELEGRAM_MODE=webhook)"))
	}
	if s := tg.WebhookSecret.Reveal(); s != "" && !validWebhookSecret(s) {
		errs = append(errs, errors.New("TELEGRAM_WEBHOOK_SECRET: 16–256 символов A-Z, a-z, 0-9, _ и -"))
	}
	return tg, errors.Join(errs...)
}

func validUsername(s string) bool {
	if len(s) < 5 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// validWebhookSecret — ограничения Telegram для secret_token плюс минимальная длина.
func validWebhookSecret(s string) bool {
	if len(s) < 16 || len(s) > 256 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// validateOrigin проверяет, что APP_ORIGIN — это scheme://host[:port] без пути.
func validateOrigin(origin string) error {
	if origin == "" {
		return errors.New("APP_ORIGIN не задан")
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("APP_ORIGIN: ожидается scheme://host[:port], получено %q", origin)
	}
	return nil
}

func getenv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
