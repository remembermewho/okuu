package demo_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/auth"
	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/demo"
	"okuu/internal/testdb"
)

const (
	ownerPhone = "+996555099001"
	password   = "demo-pass-123"
)

func bishkek(t *testing.T, y int, m time.Month, d, h int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Bishkek")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(y, m, d, h, 0, 0, 0, loc)
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// Картина демо не зависит от дня месяца: 8 с просрочкой, 5 ожидают, 2 ушли; сегодня не отмечено.
func TestSeedPicture(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	for _, now := range []time.Time{
		bishkek(t, 2026, time.October, 22, 11), // середина месяца
		bishkek(t, 2026, time.November, 3, 9),  // срок за текущий месяц ещё не наступил
		bishkek(t, 2026, time.March, 1, 8),     // день начислений
	} {
		t.Run(now.Format("2006-01-02"), func(t *testing.T) {
			res, err := demo.Seed(ctx, pool, now, ownerPhone, password)
			if err != nil {
				t.Fatal(err)
			}
			if res.Students != 40 || res.Overdue != 8 || res.Pending != 5 || res.Left != 2 {
				t.Errorf("итог: %+v", res)
			}
			bill := billing.NewService(pool, func() time.Time { return now }, nil)
			overdue, sum, err := bill.Debtors(ctx, res.CenterID, nil, billing.DebtorsOverdue)
			if err != nil {
				t.Fatal(err)
			}
			pending, _, err := bill.Debtors(ctx, res.CenterID, nil, billing.DebtorsPending)
			if err != nil {
				t.Fatal(err)
			}
			if len(overdue) != 8 || len(pending) != 5 {
				t.Errorf("должники: просрочка %d, ожидают %d (сводка %+v)", len(overdue), len(pending), sum)
			}
			days := map[int]bool{}
			for _, d := range overdue {
				days[d.DaysOverdue] = true
			}
			if len(days) < 3 {
				t.Errorf("просрочка у всех почти одинаковая: %v", days)
			}
			// Остальные расплатились ровно: ни переплат, ни лишних долгов.
			if n := count(t, pool, `SELECT count(*) FROM (SELECT student_id FROM ledger WHERE center_id = $1 AND cancelled_at IS NULL
				GROUP BY student_id HAVING sum(amount) <> 0) x`, res.CenterID); n != 13 {
				t.Errorf("учеников с ненулевым балансом %d, want 13", n)
			}
			if n := count(t, pool, `SELECT count(*) FROM ledger WHERE center_id = $1 AND cancelled_at IS NOT NULL AND cancel_reason <> ''`, res.CenterID); n != 3 {
				t.Errorf("отмен %d, want 3", n)
			}
			if n := count(t, pool, `SELECT count(DISTINCT method) FROM ledger WHERE center_id = $1 AND kind = 'payment'`, res.CenterID); n != 3 {
				t.Errorf("способов оплаты %d, want 3", n)
			}
			// История — три прошлых месяца и текущий; ничего не в будущем.
			if n := count(t, pool, `SELECT count(DISTINCT period) FROM ledger WHERE center_id = $1 AND period IS NOT NULL`, res.CenterID); n != 4 {
				t.Errorf("месяцев начислений %d, want 4", n)
			}
			for _, table := range []string{"ledger WHERE created_at", "audit_log WHERE at", "attendance a JOIN lessons l ON l.id = a.lesson_id WHERE marked_at"} {
				if n := count(t, pool, `SELECT count(*) FROM `+table+` > $2 AND center_id = $1`, res.CenterID, now); n != 0 {
					t.Errorf("%s: %d записей в будущем", table, n)
				}
			}
			today := civil.DateOf(now.In(now.Location()))
			marked := count(t, pool, `SELECT count(*) FROM attendance a JOIN lessons l ON l.id = a.lesson_id
				WHERE l.center_id = $1 AND l.date >= $2`, res.CenterID, today.AddDays(-14))
			if marked < 100 || count(t, pool, `SELECT count(*) FROM lessons WHERE center_id = $1 AND date >= $2`, res.CenterID, today) != 0 {
				t.Errorf("посещаемость: отметок %d, сегодня должно быть пусто", marked)
			}
			if n := count(t, pool, `SELECT count(*) FROM audit_log WHERE center_id = $1`, res.CenterID); n < 300 {
				t.Errorf("журнал: %d записей", n)
			}
		})
	}
}

// Макет чата на лендинге — настоящее напоминание: оба ребёнка, итог, «Как оплатить» с реквизитами демо;
// кнопок оплаты нет — только «Баланс и посещения».
func TestSampleReminder(t *testing.T) {
	m := demo.SampleReminder(civil.New(2026, time.October, 22))
	for _, want := range []string{"Центр «Пример»: 25.10 — срок оплаты", "Попов Матвей — 3 150 сом", "Итого: 6 300 сом",
		"Как оплатить:\n" + demo.PayDetails, "отправьте чек"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("нет %q в:\n%s", want, m.Text)
		}
	}
	if len(m.Buttons) != 1 || m.Buttons[0] != "Баланс и посещения" {
		t.Errorf("кнопки: %q", m.Buttons)
	}
}

// Телефоны вымышленные (+996 555 0XX XXX), чатов Telegram нет; демо помечен is_demo.
func TestSeedFictionalContacts(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	res, err := demo.Seed(ctx, pool, time.Now(), "0555 099 001", password)
	if err != nil {
		t.Fatal(err)
	}
	if res.OwnerPhone != ownerPhone {
		t.Errorf("телефон владельца %q", res.OwnerPhone)
	}
	if n := count(t, pool, `SELECT count(*) FROM parents WHERE center_id = $1 AND (phone NOT LIKE '+9965550%' OR telegram_chat_id IS NOT NULL)`, res.CenterID); n != 0 {
		t.Errorf("родителей с настоящим видом телефона или чатом: %d", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.center_id = $1 AND u.phone NOT LIKE '+9965550%'`, res.CenterID); n != 0 {
		t.Errorf("сотрудников с настоящим видом телефона: %d", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM centers WHERE id = $1 AND is_demo AND name = $2`, res.CenterID, demo.CenterName); n != 1 {
		t.Error("центр не помечен is_demo")
	}
	// Пароль владельца и сотрудников — заданный.
	svc := auth.NewService(pool, auth.Options{})
	for _, phone := range []string{ownerPhone, demo.Staff[0].Phone, demo.Staff[2].Phone} {
		if _, err := svc.Login(ctx, auth.LoginInput{Phone: phone, Password: password, IP: "127.0.0.1"}); err != nil {
			t.Errorf("вход %s: %v", phone, err)
		}
	}
}

// seed дважды подряд — один демо-центр, новый пароль; reset — тот же владелец, чужие центры целы.
func TestSeedTwiceAndReset(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	now := time.Now()

	if _, err := demo.Reset(ctx, pool, now); !errors.Is(err, demo.ErrNoDemo) {
		t.Errorf("reset без демо: %v", err)
	}

	// Настоящий центр с данными — его ничто из демо трогать не должно.
	other := uuid.New()
	var realOwner uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (phone, name, password_hash) VALUES ('+996700000001', 'Настоящий', 'x') RETURNING id`).Scan(&realOwner); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO centers (id, name) SELECT $1, 'Настоящий центр' WHERE $2::uuid IS NOT NULL`,
		`INSERT INTO memberships (user_id, center_id, role) VALUES ($2, $1, 'owner')`,
		`INSERT INTO parents (center_id, name, phone) SELECT $1, 'Родитель', '+996700000002' WHERE $2::uuid IS NOT NULL`,
		`INSERT INTO students (center_id, full_name) SELECT $1, 'Ученик' WHERE $2::uuid IS NOT NULL`,
		`INSERT INTO ledger (center_id, student_id, kind, amount, created_by) SELECT $1, id, 'correction', -100, $2 FROM students WHERE center_id = $1`,
		`INSERT INTO audit_log (center_id, user_id, action, entity) VALUES ($1, $2, 'student.create', 'student')`,
	} {
		if _, err := pool.Exec(ctx, sql, other, realOwner); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	snapshot := func() string {
		var b strings.Builder
		for _, table := range []string{"memberships", "parents", "students", "ledger", "audit_log"} {
			b.WriteString(table)
			b.WriteByte('=')
			b.WriteString(string(rune('0' + count(t, pool, `SELECT count(*) FROM `+table+` WHERE center_id = $1`, other))))
			b.WriteByte(' ')
		}
		return b.String()
	}
	before := snapshot()

	first, err := demo.Seed(ctx, pool, now, ownerPhone, password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := demo.Seed(ctx, pool, now, ownerPhone, "another-pass-456")
	if err != nil {
		t.Fatalf("seed второй раз: %v", err)
	}
	if first.CenterID == second.CenterID || count(t, pool, `SELECT count(*) FROM centers WHERE is_demo`) != 1 ||
		count(t, pool, `SELECT count(*) FROM students WHERE center_id = $1`, first.CenterID) != 0 {
		t.Error("второй seed не заменил демо-центр")
	}
	svc := auth.NewService(pool, auth.Options{})
	if _, err := svc.Login(ctx, auth.LoginInput{Phone: ownerPhone, Password: "another-pass-456", IP: "127.0.0.1"}); err != nil {
		t.Errorf("новый пароль: %v", err)
	}

	reset, err := demo.Reset(ctx, pool, now)
	if err != nil {
		t.Fatal(err)
	}
	if reset.CenterID == second.CenterID || reset.OwnerPhone != ownerPhone || reset.Students != 40 {
		t.Errorf("reset: %+v", reset)
	}
	if _, err := svc.Login(ctx, auth.LoginInput{Phone: ownerPhone, Password: "another-pass-456", IP: "127.0.0.1"}); err != nil {
		t.Errorf("reset сменил пароль: %v", err)
	}
	// Пользователи демо не плодятся: владелец + 3 сотрудника + настоящий владелец.
	if n := count(t, pool, `SELECT count(*) FROM users`); n != 5 {
		t.Errorf("пользователей %d, want 5", n)
	}
	if got := snapshot(); got != before {
		t.Errorf("настоящий центр изменился: %s → %s", before, got)
	}

	// Телефон сотрудника настоящего центра для демо не годится: его пароль не трогаем.
	if _, err := demo.Seed(ctx, pool, now, "+996700000001", password); !errors.Is(err, demo.ErrPhoneInUse) {
		t.Errorf("seed на телефон настоящего владельца: %v", err)
	}
	if count(t, pool, `SELECT count(*) FROM users WHERE phone = '+996700000001' AND password_hash = 'x'`) != 1 {
		t.Error("пароль настоящего пользователя изменён")
	}
	if count(t, pool, `SELECT count(*) FROM centers WHERE is_demo`) != 1 {
		t.Error("неудачный seed удалил демо-центр (транзакция не откатилась)")
	}
	if _, err := demo.Seed(ctx, pool, now, demo.Staff[0].Phone, password); err == nil {
		t.Error("seed на телефон демо-сотрудника прошёл")
	}
}
