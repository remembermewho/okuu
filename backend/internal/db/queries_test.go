package db_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"okuu/internal/db/sqlc"
	"okuu/internal/testdb"
)

func ptr[T any](v T) *T { return &v }

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}

func TestUserQueries(t *testing.T) {
	ctx := context.Background()
	q := sqlc.New(testdb.New(t))

	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{Phone: "+996700000001", Name: "Айгуль", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	if u.TelegramID != nil || u.CreatedAt.IsZero() {
		t.Errorf("неожиданные значения по умолчанию: %+v", u)
	}

	byPhone, err := q.GetUserByPhone(ctx, "+996700000001")
	if err != nil || byPhone.ID != u.ID {
		t.Fatalf("GetUserByPhone: %+v, %v", byPhone, err)
	}
	byID, err := q.GetUserByID(ctx, u.ID)
	if err != nil || byID.Phone != u.Phone || byID.Name != "Айгуль" {
		t.Fatalf("GetUserByID: %+v, %v", byID, err)
	}

	if _, err := q.GetUserByPhone(ctx, "+996700000999"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("неизвестный телефон: err = %v, want pgx.ErrNoRows", err)
	}
	_, err = q.CreateUser(ctx, sqlc.CreateUserParams{Phone: "+996700000001", Name: "Другой", PasswordHash: "x"})
	if !isUniqueViolation(err) {
		t.Errorf("повтор телефона: err = %v, want unique violation", err)
	}
}

func TestCenterQueries(t *testing.T) {
	ctx := context.Background()
	q := sqlc.New(testdb.New(t))

	c, err := q.CreateCenter(ctx, sqlc.CreateCenterParams{Name: "Центр А", Phone: ptr("+996555000000")})
	if err != nil {
		t.Fatal(err)
	}
	// Значения по умолчанию из ТЗ: F1, R2, R4.
	if c.BillingDay != 1 || c.DueDays != 5 || c.Timezone != "Asia/Bishkek" || c.Plan != "pilot" ||
		!c.RemindersOn || c.ExcusedDeducts {
		t.Errorf("значения по умолчанию: %+v", c)
	}

	got, err := q.GetCenter(ctx, c.ID)
	if err != nil || got.Name != "Центр А" || got.Phone == nil || *got.Phone != "+996555000000" {
		t.Fatalf("GetCenter: %+v, %v", got, err)
	}

	noPhone, err := q.CreateCenter(ctx, sqlc.CreateCenterParams{Name: "Без телефона"})
	if err != nil || noPhone.Phone != nil {
		t.Fatalf("центр без телефона: %+v, %v", noPhone, err)
	}
}

func TestMembershipQueries(t *testing.T) {
	ctx := context.Background()
	q := sqlc.New(testdb.New(t))

	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{Phone: "+996700000002", Name: "Бекзат", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	cB, _ := q.CreateCenter(ctx, sqlc.CreateCenterParams{Name: "Б-центр"})
	cA, _ := q.CreateCenter(ctx, sqlc.CreateCenterParams{Name: "А-центр"})
	cOther, _ := q.CreateCenter(ctx, sqlc.CreateCenterParams{Name: "Чужой"})

	// Один аккаунт в нескольких центрах с разными ролями.
	for _, m := range []sqlc.CreateMembershipParams{
		{UserID: u.ID, CenterID: cB.ID, Role: "teacher"},
		{UserID: u.ID, CenterID: cA.ID, Role: "owner"},
	} {
		if _, err := q.CreateMembership(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	m, err := q.GetMembership(ctx, sqlc.GetMembershipParams{UserID: u.ID, CenterID: cB.ID})
	if err != nil || m.Role != "teacher" {
		t.Fatalf("GetMembership: %+v, %v", m, err)
	}
	if _, err := q.GetMembership(ctx, sqlc.GetMembershipParams{UserID: u.ID, CenterID: cOther.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("членство в чужом центре: err = %v, want pgx.ErrNoRows", err)
	}

	list, err := q.ListMembershipsByUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].CenterName != "А-центр" || list[0].Role != "owner" ||
		list[1].CenterName != "Б-центр" || list[1].Role != "teacher" {
		t.Errorf("ListMembershipsByUser = %+v", list)
	}

	empty, err := q.ListMembershipsByUser(ctx, cOther.ID)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("пустой список: %#v, %v (emit_empty_slices: ждём [] а не nil)", empty, err)
	}

	_, err = q.CreateMembership(ctx, sqlc.CreateMembershipParams{UserID: u.ID, CenterID: cOther.ID, Role: "parent"})
	if !isCheckViolation(err) {
		t.Errorf("роль parent: err = %v, want check violation", err)
	}
	_, err = q.CreateMembership(ctx, sqlc.CreateMembershipParams{UserID: u.ID, CenterID: cA.ID, Role: "admin"})
	if !isUniqueViolation(err) {
		t.Errorf("повторное членство: err = %v, want unique violation", err)
	}
}

func TestSessionQueries(t *testing.T) {
	ctx := context.Background()
	q := sqlc.New(testdb.New(t))

	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{Phone: "+996700000003", Name: "Нурлан", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	hash := func(token string) []byte { h := sha256.Sum256([]byte(token)); return h[:] }

	s, err := q.CreateSession(ctx, sqlc.CreateSessionParams{
		TokenHash: hash("live"),
		UserID:    u.ID,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		UserAgent: ptr("Mozilla/5.0"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.LastSeenAt != nil || s.UserAgent == nil || *s.UserAgent != "Mozilla/5.0" {
		t.Errorf("CreateSession: %+v", s)
	}

	got, err := q.GetSessionByTokenHash(ctx, hash("live"))
	if err != nil || got.UserID != u.ID {
		t.Fatalf("GetSessionByTokenHash: %+v, %v", got, err)
	}

	newExpiry := time.Now().Add(31 * 24 * time.Hour).Truncate(time.Microsecond)
	seen := time.Now().Truncate(time.Microsecond)
	if err := q.TouchSession(ctx, sqlc.TouchSessionParams{TokenHash: hash("live"), LastSeenAt: &seen, ExpiresAt: newExpiry}); err != nil {
		t.Fatal(err)
	}
	got, err = q.GetSessionByTokenHash(ctx, hash("live"))
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSeenAt == nil || !got.LastSeenAt.Equal(seen) || !got.ExpiresAt.Equal(newExpiry) {
		t.Errorf("после TouchSession: last_seen_at=%v expires_at=%v, want %v", got.LastSeenAt, got.ExpiresAt, newExpiry)
	}

	// Просроченная сессия не возвращается, даже если ещё лежит в таблице.
	if _, err := q.CreateSession(ctx, sqlc.CreateSessionParams{
		TokenHash: hash("old"), UserID: u.ID, ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetSessionByTokenHash(ctx, hash("old")); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("просроченная сессия: err = %v, want pgx.ErrNoRows", err)
	}

	n, err := q.DeleteExpiredSessions(ctx)
	if err != nil || n != 1 {
		t.Errorf("DeleteExpiredSessions: n=%d err=%v, want 1", n, err)
	}

	if err := q.DeleteSession(ctx, hash("live")); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetSessionByTokenHash(ctx, hash("live")); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("после DeleteSession: err = %v, want pgx.ErrNoRows", err)
	}
}
