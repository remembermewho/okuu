// Package testdb создаёт временные базы PostgreSQL для интеграционных тестов.
//
// Подключение берётся из TEST_DATABASE_URL, иначе из DATABASE_URL (в том числе из .env
// в корне репозитория) — обычно это БД из `docker compose up -d db`. Каждый тест получает
// свою базу okuu_test_<random>, которая удаляется после теста. С `go test -short` тесты
// с БД пропускаются.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/config"
	"okuu/internal/db"
)

var (
	adminOnce sync.Once
	adminURL  string
)

// Empty создаёт пустую базу и возвращает её URL. База удаляется в t.Cleanup.
func Empty(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("тест с PostgreSQL пропущен в -short")
	}
	base := baseURL(t)

	name := "okuu_test_" + randomHex(t, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("testdb: нет подключения к PostgreSQL (%v). Запустите `docker compose up -d db`", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("testdb: создание базы: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, base)
		if err != nil {
			t.Errorf("testdb: удаление базы %s: %v", name, err)
			return
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("testdb: удаление базы %s: %v", name, err)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("testdb: разбор URL: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

// Connect открывает пул к базе url и закрывает его в t.Cleanup.
func Connect(t testing.TB, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := db.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// New создаёт отдельную базу, применяет миграции и возвращает пул.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool := Connect(t, Empty(t))
	if _, err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("testdb: %v", err)
	}
	return pool
}

func baseURL(t testing.TB) string {
	adminOnce.Do(func() {
		if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
			adminURL = v
			return
		}
		if path := findUp(".env"); path != "" {
			if _, err := config.LoadDotEnv(path); err != nil {
				t.Logf("testdb: %v", err)
			}
		}
		adminURL = os.Getenv("DATABASE_URL")
	})
	if adminURL == "" {
		t.Fatal("testdb: задайте TEST_DATABASE_URL или DATABASE_URL")
	}
	return adminURL
}

// findUp ищет файл name в текущей папке и выше.
func findUp(name string) string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func randomHex(t testing.TB, n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
