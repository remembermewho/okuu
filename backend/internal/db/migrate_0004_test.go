package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"okuu/internal/db"
	"okuu/internal/testdb"
)

// 0004 заполняет billed_through у месячных записей, созданных до неё: текущий и прошлые
// месяцы уже прошли выбор R5, будущие и абонементы — нет.
func TestMigration0004BackfillsBilledThrough(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Connect(t, testdb.Empty(t))
	m, err := db.NewMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.UpTo(ctx, 3); err != nil {
		t.Fatal(err)
	}

	// Центр в Бишкеке; «текущий месяц» считаем в БД так же, как миграция.
	_, err = pool.Exec(ctx, `
		WITH c AS (INSERT INTO centers (name) VALUES ('Ц') RETURNING id),
		     st AS (INSERT INTO students (center_id, full_name) SELECT id, 'У' FROM c RETURNING id, center_id),
		     tm AS (INSERT INTO tariffs (center_id, name, kind, price) SELECT id, 'М', 'monthly', 100 FROM c RETURNING id),
		     tp AS (INSERT INTO tariffs (center_id, name, kind, price, lessons_count) SELECT id, 'А', 'package', 100, 8 FROM c RETURNING id),
		     g1 AS (INSERT INTO groups (center_id, name) SELECT id, 'текущий' FROM c RETURNING id),
		     g2 AS (INSERT INTO groups (center_id, name) SELECT id, 'давно' FROM c RETURNING id),
		     g3 AS (INSERT INTO groups (center_id, name) SELECT id, 'будущий' FROM c RETURNING id),
		     g4 AS (INSERT INTO groups (center_id, name) SELECT id, 'абонемент' FROM c RETURNING id),
		     m AS (SELECT date_trunc('month', now() AT TIME ZONE 'Asia/Bishkek')::date AS p)
		INSERT INTO enrollments (center_id, student_id, group_id, tariff_id, start_date)
		SELECT st.center_id, st.id, g1.id, tm.id, m.p + 3 FROM st, g1, tm, m
		UNION ALL SELECT st.center_id, st.id, g2.id, tm.id, m.p - 400 FROM st, g2, tm, m
		UNION ALL SELECT st.center_id, st.id, g3.id, tm.id, (m.p + interval '1 month')::date FROM st, g3, tm, m
		UNION ALL SELECT st.center_id, st.id, g4.id, tp.id, m.p FROM st, g4, tp, m`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up 0004: %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT g.name, e.billed_through IS NOT DISTINCT FROM date_trunc('month', now() AT TIME ZONE 'Asia/Bishkek')::date,
		       e.billed_through IS NULL
		FROM enrollments e JOIN groups g ON g.id = e.group_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string]string{"текущий": "period", "давно": "period", "будущий": "null", "абонемент": "null"}
	seen := 0
	for rows.Next() {
		var name string
		var isPeriod, isNull bool
		if err := rows.Scan(&name, &isPeriod, &isNull); err != nil {
			t.Fatal(err)
		}
		got := map[bool]string{true: "period", false: "null"}[isPeriod]
		if !isPeriod && !isNull {
			got = "другое"
		}
		if got != want[name] {
			t.Errorf("%s: billed_through = %s, want %s", name, got, want[name])
		}
		seen++
	}
	if seen != 4 {
		t.Fatalf("записей %d, want 4", seen)
	}

	// CHECK: только 1-е число месяца.
	_, err = pool.Exec(ctx, `UPDATE enrollments SET billed_through = '2026-10-15'`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("billed_through не 1-го числа: err = %v, want check violation", err)
	}
}
