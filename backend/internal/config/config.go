// Package config читает настройки приложения из переменных окружения.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

const minKeyLen = 32

// Config — настройки процесса. Секреты приходят только из env.
type Config struct {
	Env           string // dev | test | prod
	DatabaseURL   string
	HTTPAddr      string
	SessionKey    []byte
	EncryptionKey []byte
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

	var err error
	if cfg.SessionKey, err = loadKey("SESSION_KEY", cfg.Env == "prod"); err != nil {
		errs = append(errs, err)
	}
	if cfg.EncryptionKey, err = loadKey("ENCRYPTION_KEY", cfg.Env == "prod"); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
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

func getenv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
