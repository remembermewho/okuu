package config

import (
	"encoding/base64"
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

	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"dev без ключей", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x"}, false},
		{"нет DATABASE_URL", map[string]string{"APP_ENV": "dev"}, true},
		{"prod без ENCRYPTION_KEY", map[string]string{"APP_ENV": "prod", "DATABASE_URL": "postgres://x", "APP_ORIGIN": "https://app.okuu.kg"}, true},
		{"prod с ключами", map[string]string{"APP_ENV": "prod", "DATABASE_URL": "postgres://x", "ENCRYPTION_KEY": key32, "APP_ORIGIN": "https://app.okuu.kg"}, false},
		{"prod без APP_ORIGIN", map[string]string{"APP_ENV": "prod", "DATABASE_URL": "postgres://x", "ENCRYPTION_KEY": key32}, true},
		{"APP_ORIGIN с путём", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "APP_ORIGIN": "http://localhost:5173/app"}, true},
		{"APP_ORIGIN без схемы", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "APP_ORIGIN": "localhost:5173"}, true},
		{"короткий ключ", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "ENCRYPTION_KEY": key16}, true},
		{"ключ не base64", map[string]string{"APP_ENV": "dev", "DATABASE_URL": "postgres://x", "ENCRYPTION_KEY": "!!!"}, true},
		{"неизвестный APP_ENV", map[string]string{"APP_ENV": "staging", "DATABASE_URL": "postgres://x"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range []string{"APP_ENV", "DATABASE_URL", "HTTP_ADDR", "APP_ORIGIN", "ENCRYPTION_KEY"} {
				t.Setenv(k, tt.env[k])
			}
			cfg, err := Load()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if cfg.HTTPAddr != ":8080" {
				t.Errorf("HTTPAddr = %q, want :8080 по умолчанию", cfg.HTTPAddr)
			}
			if tt.env["APP_ORIGIN"] == "" && cfg.AppOrigin != devOrigin {
				t.Errorf("AppOrigin = %q, want %q по умолчанию вне prod", cfg.AppOrigin, devOrigin)
			}
		})
	}
}
