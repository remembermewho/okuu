package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDotEnv(t *testing.T) {
	in := `
# комментарий
DATABASE_URL=postgres://u:p@localhost:5433/db?sslmode=disable
export HTTP_ADDR=":9090"
KEY_B64=abc==
SINGLE='quoted value'
EMPTY=
`
	got, err := parseDotEnv(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"DATABASE_URL": "postgres://u:p@localhost:5433/db?sslmode=disable",
		"HTTP_ADDR":    ":9090",
		"KEY_B64":      "abc==",
		"SINGLE":       "quoted value",
		"EMPTY":        "",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d vars, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseDotEnvInvalidLine(t *testing.T) {
	if _, err := parseDotEnv(strings.NewReader("NOEQUALS\n")); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}

func TestLoadDotEnvDoesNotOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("OKUU_T_A=file\nOKUU_T_B=file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OKUU_T_A", "env")
	os.Unsetenv("OKUU_T_B")
	t.Cleanup(func() { os.Unsetenv("OKUU_T_B") })

	loaded, err := LoadDotEnv(filepath.Join(dir, "missing.env"), path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != path {
		t.Errorf("loaded = %q, want %q", loaded, path)
	}
	if v := os.Getenv("OKUU_T_A"); v != "env" {
		t.Errorf("OKUU_T_A = %q, переменная окружения не должна перезаписываться", v)
	}
	if v := os.Getenv("OKUU_T_B"); v != "file" {
		t.Errorf("OKUU_T_B = %q, want file", v)
	}
}

func TestLoad(t *testing.T) {
	key32 := base64.StdEncoding.EncodeToString(make([]byte, 32))
	key16 := base64.StdEncoding.EncodeToString(make([]byte, 16))

	// prod — полный набор для prod; with — он же с заменами ("" — переменную убрать).
	prod := map[string]string{
		"APP_ENV": "prod", "DATABASE_URL": "postgres://x", "ENCRYPTION_KEY": key32, "APP_ORIGIN": "https://app.okuu.kg",
		"TELEGRAM_BOT_TOKEN": "123456:FAKE-test-token", "TELEGRAM_BOT_USERNAME": "okuu_test_bot",
		"TELEGRAM_WEBHOOK_SECRET": "fake_webhook_secret_0123456789",
	}
	with := func(kv ...string) map[string]string {
		m := map[string]string{}
		for k, v := range prod {
			m[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}

	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"dev без ключей", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x"}, false},
		{"нет DATABASE_URL", map[string]string{"APP_ENV": "dev"}, true},
		{"prod полностью", prod, false},
		{"prod без ENCRYPTION_KEY", with("ENCRYPTION_KEY", ""), true},
		{"prod с коротким ключом", with("ENCRYPTION_KEY", key16), true},
		{"prod без APP_ORIGIN", with("APP_ORIGIN", ""), true},
		{"prod с http APP_ORIGIN", with("APP_ORIGIN", "http://app.okuu.kg"), true},
		{"prod без DATABASE_URL", with("DATABASE_URL", ""), true},
		{"prod с polling", with("TELEGRAM_MODE", "polling"), true},
		{"prod без бота", with("TELEGRAM_BOT_TOKEN", ""), true},
		{"prod без секрета вебхука", with("TELEGRAM_WEBHOOK_SECRET", ""), true},
		{"TRUSTED_PROXIES", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "TRUSTED_PROXIES": "172.30.0.0/24, 10.0.0.1"}, false},
		{"TRUSTED_PROXIES кривой", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "TRUSTED_PROXIES": "172.30.0.0/99"}, true},
		{"ALERT_TELEGRAM_CHAT_ID", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "ALERT_TELEGRAM_CHAT_ID": "-1001234567890"}, false},
		{"ALERT_TELEGRAM_CHAT_ID кривой", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "ALERT_TELEGRAM_CHAT_ID": "@chat"}, true},
		{"APP_ORIGIN с путём", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "APP_ORIGIN": "http://localhost:5173/app"}, true},
		{"APP_ORIGIN без схемы", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "APP_ORIGIN": "localhost:5173"}, true},
		{"короткий ключ", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "ENCRYPTION_KEY": key16}, true},
		{"ключ не base64", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "ENCRYPTION_KEY": "!!!"}, true},
		{"неизвестный APP_ENV", map[string]string{"APP_ENV": "staging", "DATABASE_URL": "postgres://x"}, true},
		{"FINIK_ENABLED=true", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "FINIK_ENABLED": "true"}, false},
		{"FINIK_ENABLED кривой", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "FINIK_ENABLED": "maybe"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range append([]string{"APP_ENV", "DATABASE_URL", "HTTP_ADDR", "APP_ORIGIN", "ENCRYPTION_KEY", "FINIK_ENABLED",
				"FINIK_API_KEY", "TRUSTED_PROXIES", "ALERT_TELEGRAM_CHAT_ID"}, telegramVars...) {
				t.Setenv(k, tt.env[k])
			}
			cfg, err := Load()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				if strings.Contains(err.Error(), "FAKE-test-token") || strings.Contains(err.Error(), "fake_webhook_secret") {
					t.Errorf("в ошибке секрет: %v", err)
				}
				return
			}
			if tt.env["APP_ENV"] == "prod" && cfg.Telegram.Mode != TelegramWebhook {
				t.Errorf("prod: Telegram.Mode = %q", cfg.Telegram.Mode)
			}
			if tt.env["TRUSTED_PROXIES"] != "" && fmt.Sprint(cfg.TrustedProxies) != "[172.30.0.0/24 10.0.0.1/32]" {
				t.Errorf("TrustedProxies = %v", cfg.TrustedProxies)
			}
			if tt.env["ALERT_TELEGRAM_CHAT_ID"] != "" && cfg.AlertChatID != -1001234567890 {
				t.Errorf("AlertChatID = %d", cfg.AlertChatID)
			}
			if cfg.HTTPAddr != ":8080" {
				t.Errorf("HTTPAddr = %q, want :8080 по умолчанию", cfg.HTTPAddr)
			}
			if cfg.FinikEnabled != (tt.env["FINIK_ENABLED"] == "true") {
				t.Errorf("FinikEnabled = %v", cfg.FinikEnabled)
			}
			if tt.env["APP_ENV"] != "prod" && tt.env["APP_ORIGIN"] == "" && cfg.AppOrigin != devOrigin {
				t.Errorf("AppOrigin = %q, want %q по умолчанию вне prod", cfg.AppOrigin, devOrigin)
			}
		})
	}
}

var telegramVars = []string{"TELEGRAM_MODE", "TELEGRAM_BOT_TOKEN", "TELEGRAM_BOT_USERNAME", "TELEGRAM_WEBHOOK_SECRET"}

func TestLoadTelegram(t *testing.T) {
	const (
		fakeToken  = "123456:FAKE-test-token"
		fakeSecret = "fake_webhook_secret_0123456789"
	)
	tests := []struct {
		name     string
		env      map[string]string
		wantMode string
		wantErr  bool
	}{
		{"без токена — выключен", map[string]string{"APP_ENV": "dev"}, TelegramOff, false},
		{"dev с токеном — polling", map[string]string{"APP_ENV": "dev", "TELEGRAM_BOT_TOKEN": fakeToken, "TELEGRAM_BOT_USERNAME": "@okuu_test_bot"}, TelegramPolling, false},
		{"prod с токеном — webhook", map[string]string{"APP_ENV": "prod", "TELEGRAM_BOT_TOKEN": fakeToken, "TELEGRAM_BOT_USERNAME": "okuu_test_bot", "TELEGRAM_WEBHOOK_SECRET": fakeSecret}, TelegramWebhook, false},
		{"webhook без секрета", map[string]string{"APP_ENV": "dev", "TELEGRAM_MODE": "webhook", "TELEGRAM_BOT_TOKEN": fakeToken, "TELEGRAM_BOT_USERNAME": "okuu_test_bot"}, "", true},
		{"короткий секрет", map[string]string{"APP_ENV": "dev", "TELEGRAM_MODE": "webhook", "TELEGRAM_BOT_TOKEN": fakeToken, "TELEGRAM_BOT_USERNAME": "okuu_test_bot", "TELEGRAM_WEBHOOK_SECRET": "short"}, "", true},
		{"секрет с недопустимым символом", map[string]string{"APP_ENV": "dev", "TELEGRAM_MODE": "webhook", "TELEGRAM_BOT_TOKEN": fakeToken, "TELEGRAM_BOT_USERNAME": "okuu_test_bot", "TELEGRAM_WEBHOOK_SECRET": "fake/webhook/secret/0123"}, "", true},
		{"polling без токена", map[string]string{"APP_ENV": "dev", "TELEGRAM_MODE": "polling", "TELEGRAM_BOT_USERNAME": "okuu_test_bot"}, "", true},
		{"токен без имени бота", map[string]string{"APP_ENV": "dev", "TELEGRAM_BOT_TOKEN": fakeToken}, "", true},
		{"кривое имя бота", map[string]string{"APP_ENV": "dev", "TELEGRAM_BOT_TOKEN": fakeToken, "TELEGRAM_BOT_USERNAME": "okuu bot"}, "", true},
		{"неизвестный режим", map[string]string{"APP_ENV": "dev", "TELEGRAM_MODE": "push"}, "", true},
		{"явно выключен с токеном", map[string]string{"APP_ENV": "dev", "TELEGRAM_MODE": "off", "TELEGRAM_BOT_TOKEN": fakeToken}, TelegramOff, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range telegramVars {
				t.Setenv(k, tt.env[k])
			}
			tg, err := loadTelegram(tt.env["APP_ENV"])
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				if strings.Contains(err.Error(), fakeToken) || strings.Contains(err.Error(), "fake/webhook") {
					t.Errorf("ошибка содержит секрет: %v", err)
				}
				return
			}
			if tg.Mode != tt.wantMode {
				t.Errorf("Mode = %q, want %q", tg.Mode, tt.wantMode)
			}
			if tt.env["TELEGRAM_BOT_USERNAME"] != "" && tg.Username != "okuu_test_bot" {
				t.Errorf("Username = %q, want без @", tg.Username)
			}
		})
	}
}

// Секрет не печатается ни через fmt, ни через slog, ни в JSON.
func TestSecretIsMasked(t *testing.T) {
	const raw = "123456:FAKE-test-token"
	tg := Telegram{Mode: TelegramPolling, Token: Secret(raw), Username: "okuu_test_bot"}
	var logged strings.Builder
	slog.New(slog.NewTextHandler(&logged, nil)).Info("cfg", "token", tg.Token, "tg", tg)
	js, err := json.Marshal(tg)
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]string{
		"%v": fmt.Sprintf("%v", tg), "%+v": fmt.Sprintf("%+v", tg), "%#v": fmt.Sprintf("%#v", tg),
		"%s": fmt.Sprintf("%s", tg.Token), "slog": logged.String(), "json": string(js),
	} {
		if strings.Contains(s, raw) || strings.Contains(s, "FAKE") {
			t.Errorf("%s: секрет виден: %s", name, s)
		}
	}
	if tg.Token.Reveal() != raw {
		t.Error("Reveal должен вернуть значение")
	}
	if Secret("").String() != "" {
		t.Error("пустой секрет печатается пустым")
	}
}
