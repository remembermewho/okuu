package db_test

import (
	"context"
	"io/fs"
	"slices"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/db"
	"okuu/internal/testdb"
	"okuu/migrations"
)

var wantTables = []string{
	"attendance", "audit_log", "broadcast_recipients", "broadcasts", "centers", "enrollments", "groups", "import_sessions", "invites", "ledger",
	"lessons", "memberships", "notification_outbox", "notifications", "parents", "password_resets", "payment_links", "platform_audit_log", "registration_links", "sessions",
	"students", "tariffs", "telegram_link_states", "users",
}

// migrationCount — число файлов миграций; последняя версия равна ему (нумерация 0001, 0002, ...).
func migrationCount(t *testing.T) int {
	t.Helper()
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("миграции не найдены: %v", err)
	}
	return len(files)
}

func TestMigrateCleanDBAndRerun(t *testing.T) {
	want := migrationCount(t)
	ctx := context.Background()
	pool := testdb.Connect(t, testdb.Empty(t))

	m, err := db.NewMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	res, err := m.Up(ctx)
	if err != nil {
		t.Fatalf("первый запуск: %v", err)
	}
	if len(res) != want {
		t.Fatalf("применено %d миграций, want %d", len(res), want)
	}
	assertVersion(t, m, int64(want))
	assertTables(t, pool, wantTables)

	// Повторный запуск (как рестарт сервера) — ничего не применяет и не падает.
	res, err = db.Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("повторный запуск: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("повторный запуск применил %d миграций, want 0", len(res))
	}
	assertVersion(t, m, int64(want))
	assertTables(t, pool, wantTables)
}

func TestMigrateDownAndUpAgain(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Connect(t, testdb.Empty(t))

	m, err := db.NewMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DownTo(ctx, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	assertVersion(t, m, 0)
	assertTables(t, pool, nil)

	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up после down: %v", err)
	}
	assertTables(t, pool, wantTables)
}

// Два экземпляра сервера стартуют одновременно: advisory lock goose
// должен пропустить каждую миграцию ровно один раз и без ошибок.
func TestMigrateConcurrentStart(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Connect(t, testdb.Empty(t))

	const n = 3
	var wg sync.WaitGroup
	applied := make([]int, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := db.Migrate(ctx, pool)
			applied[i], errs[i] = len(res), err
		}()
	}
	wg.Wait()

	total := 0
	for i := range n {
		if errs[i] != nil {
			t.Errorf("экземпляр %d: %v", i, errs[i])
		}
		total += applied[i]
	}
	if want := migrationCount(t); total != want {
		t.Errorf("применено миграций всего %d, want %d: каждая ровно один раз", total, want)
	}
	assertTables(t, pool, wantTables)
}

func assertVersion(t *testing.T, m *db.Migrator, want int64) {
	t.Helper()
	v, err := m.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != want {
		t.Fatalf("версия схемы = %d, want %d", v, want)
	}
}

// assertTables проверяет набор таблиц приложения в public (без служебной таблицы goose).
func assertTables(t *testing.T, pool *pgxpool.Pool, want []string) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name <> 'goose_db_version'
		ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("таблицы:\n got  %v\n want %v", got, want)
	}
}
