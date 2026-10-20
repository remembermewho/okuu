package auth_test

import (
	"context"
	"crypto/sha256"
	"testing"

	"okuu/internal/auth"
	"okuu/internal/testdb"
)

func TestDeleteExpiredSessions(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	var userID string
	err := pool.QueryRow(ctx,
		`INSERT INTO users (phone, name, password_hash) VALUES ('+996700000001', 'Тест', 'x') RETURNING id`,
	).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}

	sessions := []struct {
		token   string
		expires string
	}{
		{"expired-1", "now() - interval '1 hour'"},
		{"expired-2", "now() - interval '31 days'"},
		{"live", "now() + interval '30 days'"},
	}
	for _, s := range sessions {
		h := sha256.Sum256([]byte(s.token))
		if _, err := pool.Exec(ctx,
			`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, `+s.expires+`)`,
			h[:], userID); err != nil {
			t.Fatal(err)
		}
	}

	n, err := auth.DeleteExpiredSessions(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("удалено %d, want 2", n)
	}

	live := sha256.Sum256([]byte("live"))
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE token_hash = $1`, live[:]).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Errorf("живая сессия удалена")
	}

	// Повторный вызов ничего не удаляет.
	if n, err := auth.DeleteExpiredSessions(ctx, pool); err != nil || n != 0 {
		t.Errorf("повторно: n=%d err=%v, want 0, nil", n, err)
	}

	// ON DELETE CASCADE: сессии уходят вместе с пользователем.
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("после удаления пользователя осталось %d сессий", left)
	}
}
