// Package demo — демо-центр для показа: вымышленные ученики, история оплат за три прошлых месяца,
// должники, посещаемость и журнал. Центр помечен is_demo: напоминания и бот его пропускают.
//
// Данные вставляются прямо SQL в одной транзакции: сервисы не умеют ставить операциям прошлые даты.
// Случайность детерминированная — у двух запусков в один день одинаковая картина.
package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/auth"
	"okuu/internal/civil"
)

const (
	// CenterName — название демо-центра.
	CenterName = "Демо: учебный центр «Пример»"
	// OwnerName — имя нового владельца демо (существующему имя не меняем).
	OwnerName = "Айжан Осмонова"
	// PayDetails — реквизиты демо-центра (вымышленные).
	PayDetails = "Оплата на MBank: 0555 000 100 (Учебный центр «Пример»).\n" +
		"После оплаты отправьте чек с ФИО ученика и суммой администратору в WhatsApp: +996 555 000 101."
	timezone = "Asia/Bishkek"
	dueDays  = 5
)

var (
	// ErrNoDemo — reset без демо-центра: сначала seed.
	ErrNoDemo = errors.New("демо-центра нет — создайте его: server demo seed --phone=<телефон> --password=<пароль>")
	// ErrPhoneInUse — телефон у пользователя настоящего центра: его пароль демо не трогает.
	ErrPhoneInUse = errors.New("телефон уже работает в настоящем центре — для демо укажите другой")
)

// Staff — сотрудники демо: телефоны заведомо тестовые (+996 555 09X XXX), пароль — как у владельца.
var Staff = []struct{ Phone, Name, Role string }{
	{"+996555090001", "Бермет Токтогулова", "teacher"},
	{"+996555090002", "Дмитрий Ковалёв", "teacher"},
	{"+996555090003", "Нургуль Садыкова", "admin"},
}

// Result — что создано.
type Result struct {
	CenterID   uuid.UUID
	OwnerPhone string
	Students   int
	Overdue    int // должники с просрочкой
	Pending    int // ожидают оплаты (срок ещё не наступил)
	Left       int
}

// Seed пересоздаёт демо-центр: удаляет прежний (только is_demo) и создаёт новый с владельцем phone.
// Пароль владельца (и демо-сотрудников) становится password.
func Seed(ctx context.Context, pool *pgxpool.Pool, now time.Time, phone, password string) (Result, error) {
	phone, err := auth.NormalizePhone(phone)
	if err != nil {
		return Result{}, fmt.Errorf("--phone: %w", err)
	}
	for _, st := range Staff {
		if st.Phone == phone {
			return Result{}, fmt.Errorf("--phone: %s — телефон демо-сотрудника, укажите другой", phone)
		}
	}
	if err := auth.ValidatePassword(password); err != nil {
		return Result{}, fmt.Errorf("--password: %w", err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return Result{}, err
	}
	var res Result
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		old, err := deleteDemo(ctx, tx)
		if err != nil {
			return err
		}
		owner, err := upsertUser(ctx, tx, phone, OwnerName, hash, true)
		if err != nil {
			return err
		}
		res, err = build(ctx, tx, now, owner, phone, hash, old)
		return err
	})
	return res, err
}

// Reset пересоздаёт демо-центр с нуля с тем же владельцем и паролем. Другие центры не трогает.
func Reset(ctx context.Context, pool *pgxpool.Pool, now time.Time) (Result, error) {
	var res Result
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var (
			owner       uuid.UUID
			phone, hash string
		)
		err := tx.QueryRow(ctx, `
			SELECT u.id, u.phone, u.password_hash FROM memberships m
			JOIN centers c ON c.id = m.center_id JOIN users u ON u.id = m.user_id
			WHERE c.is_demo AND m.role = 'owner' ORDER BY c.created_at DESC LIMIT 1`).Scan(&owner, &phone, &hash)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoDemo
		}
		if err != nil {
			return err
		}
		old, err := deleteDemo(ctx, tx)
		if err != nil {
			return err
		}
		res, err = build(ctx, tx, now, owner, phone, hash, old)
		return err
	})
	return res, err
}

// deleteDemo удаляет все центры с is_demo и их данные; возвращает их сотрудников.
func deleteDemo(ctx context.Context, tx pgx.Tx) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	rows, err := tx.Query(ctx, `SELECT id FROM centers WHERE is_demo FOR UPDATE`)
	if err != nil {
		return nil, err
	}
	ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	var members []uuid.UUID
	rows, err = tx.Query(ctx, `SELECT DISTINCT user_id FROM memberships WHERE center_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	if members, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
		return nil, err
	}
	// Порядок — по внешним ключам: сначала то, что ссылается на остальное.
	for _, stmt := range []string{
		`DELETE FROM attendance WHERE lesson_id IN (SELECT id FROM lessons WHERE center_id = ANY($1))`,
		`DELETE FROM lessons WHERE center_id = ANY($1)`,
		`DELETE FROM payment_links WHERE center_id = ANY($1)`,
		`DELETE FROM notification_outbox WHERE center_id = ANY($1)`,
		`DELETE FROM notifications WHERE center_id = ANY($1)`,
		`DELETE FROM broadcast_recipients WHERE center_id = ANY($1)`,
		`DELETE FROM broadcasts WHERE center_id = ANY($1)`,
		`DELETE FROM invites WHERE center_id = ANY($1)`, // telegram_link_states — каскадом
		`DELETE FROM import_sessions WHERE center_id = ANY($1)`,
		`DELETE FROM password_resets WHERE center_id = ANY($1)`,
		`DELETE FROM ledger WHERE center_id = ANY($1)`,
		`DELETE FROM enrollments WHERE center_id = ANY($1)`,
		`DELETE FROM students WHERE center_id = ANY($1)`,
		`DELETE FROM parents WHERE center_id = ANY($1)`,
		`DELETE FROM groups WHERE center_id = ANY($1)`,
		`DELETE FROM tariffs WHERE center_id = ANY($1)`,
		`DELETE FROM audit_log WHERE center_id = ANY($1)`,
		`DELETE FROM memberships WHERE center_id = ANY($1)`,
		`DELETE FROM centers WHERE id = ANY($1) AND is_demo`,
	} {
		if _, err := tx.Exec(ctx, stmt, ids); err != nil {
			return nil, fmt.Errorf("удаление демо: %w", err)
		}
	}
	return members, nil
}

// upsertUser — пользователь по телефону. Существующий, но работающий в настоящем центре, — ErrPhoneInUse
// (демо-центры к этому моменту удалены, поэтому любое оставшееся членство — настоящее).
func upsertUser(ctx context.Context, tx pgx.Tx, phone, name, hash string, setPassword bool) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM users WHERE phone = $1`, phone).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO users (phone, name, password_hash) VALUES ($1, $2, $3) RETURNING id`,
			phone, name, hash).Scan(&id)
		return id, err
	}
	if err != nil {
		return id, err
	}
	var real bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM memberships WHERE user_id = $1)`, id).Scan(&real); err != nil {
		return id, err
	}
	if real {
		return id, fmt.Errorf("%s: %w", phone, ErrPhoneInUse)
	}
	if setPassword {
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, id, hash); err != nil {
			return id, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
			return id, err
		}
	}
	return id, nil
}

// scenario — что происходит с оплатами ученика.
type scenario int

const (
	regular     scenario = iota // платит вовремя
	late                        // платит прошлые месяцы с опозданием
	overdue3                    // не платит три последних срока
	overdue2                    // не платит два последних срока
	overdue1                    // не платит последний срок
	partial                     // за последний срок заплатил половину
	newcomer                    // записался позавчера, срок ещё не наступил
	cancelSum                   // оплата с ошибкой в сумме отменена
	cancelTwice                 // оплата внесена дважды, вторая отменена
	cancelWay                   // оплата не тем способом отменена
	leftDebt                    // ушёл с долгом
	leftPaid                    // ушёл, всё оплачено
	pkg                         // абонемент, всё оплачено
	pkgPending                  // абонемент продлён вчера, срок ещё не наступил
)

type studentSpec struct {
	name, parent string
	group        int
	sc           scenario
	startMonth   int // с какого из четырёх месяцев учится (0 — три месяца назад)
}

// students — вымышленные ученики; Поповы — брат и сестра (скидка 10%).
var students = []studentSpec{
	// Английский A1
	{"Асанов Нурсултан", "Гульмира Асанова", 0, regular, 0},
	{"Бекова Айпери", "Жылдыз Бекова", 0, overdue3, 0},
	{"Попов Матвей", "Ольга Попова", 0, regular, 0},
	{"Попова Ева", "Ольга Попова", 0, regular, 0},
	{"Жумабаев Эрлан", "Канат Жумабаев", 0, partial, 0},
	{"Касымова Алина", "Динара Касымова", 0, regular, 1},
	{"Иванова Полина", "Елена Иванова", 0, late, 0},
	{"Токтосунов Бакыт", "Айнура Токтосунова", 0, newcomer, 3},
	{"Смирнов Артём", "Наталья Смирнова", 0, regular, 0},
	{"Мамытова Айсулуу", "Чынара Мамытова", 0, cancelSum, 0},
	{"Орозбеков Тимур", "Бактыгуль Орозбекова", 0, regular, 0},
	{"Сыдыкова Мээрим", "Зарина Сыдыкова", 0, leftDebt, 0},
	// Английский B1
	{"Абдыкадыров Эмир", "Нурлан Абдыкадыров", 1, regular, 0},
	{"Кузнецова Варвара", "Ирина Кузнецова", 1, overdue2, 0},
	{"Исакова Жибек", "Айгуль Исакова", 1, regular, 2},
	{"Турдубаев Адилет", "Эркин Турдубаев", 1, regular, 0},
	{"Лебедев Глеб", "Светлана Лебедева", 1, overdue1, 0},
	{"Чолпонбаева Нурай", "Гулзат Чолпонбаева", 1, regular, 0},
	{"Новикова Алиса", "Татьяна Новикова", 1, late, 0},
	{"Бакиров Арсен", "Мирлан Бакиров", 1, regular, 0},
	{"Усенова Алтынай", "Асель Усенова", 1, cancelTwice, 0},
	{"Морозов Марк", "Андрей Морозов", 1, leftPaid, 0},
	// Математика, 5 класс
	{"Кадырова Сезим", "Бурул Кадырова", 2, regular, 0},
	{"Шаршеев Бекзат", "Таалай Шаршеев", 2, overdue3, 0},
	{"Волкова София", "Марина Волкова", 2, regular, 0},
	{"Эсенова Айдана", "Нургуль Эсенова", 2, late, 0},
	{"Молдокулов Нурбол", "Жамиля Молдокулова", 2, overdue2, 0},
	{"Павлов Лев", "Юлия Павлова", 2, regular, 1},
	{"Абылкасымова Аруузат", "Сабира Абылкасымова", 2, partial, 0},
	{"Семёнова Вера", "Оксана Семёнова", 2, regular, 0},
	{"Осмонов Данияр", "Улан Осмонов", 2, newcomer, 3},
	{"Голубев Степан", "Мария Голубева", 2, regular, 0},
	{"Темирова Айзада", "Назгуль Темирова", 2, cancelWay, 0},
	{"Сатаров Эльдар", "Индира Сатарова", 2, regular, 0},
	// Робототехника (абонемент)
	{"Виноградова Мия", "Анна Виноградова", 3, pkg, 0},
	{"Алиев Расул", "Махабат Алиева", 3, pkgPending, 0},
	{"Белов Фёдор", "Сергей Белов", 3, pkg, 0},
	{"Нурланова Самира", "Айгерим Нурланова", 3, pkgPending, 0},
	{"Козлов Илья", "Виктория Козлова", 3, pkg, 0},
	{"Жусупов Алинур", "Мээрим Жусупова", 3, pkgPending, 0},
}

type slot struct {
	Dow   int    `json:"dow"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type groupSpec struct {
	name    string
	teacher int // индекс в Staff
	tariff  int
	slots   []slot
}

var groupSpecs = []groupSpec{
	{"Английский A1", 0, 0, []slot{{1, "15:00", "16:30"}, {3, "15:00", "16:30"}}},
	{"Английский B1", 0, 1, []slot{{2, "17:00", "18:30"}, {4, "17:00", "18:30"}}},
	{"Математика, 5 класс", 1, 0, []slot{{1, "14:00", "15:00"}, {3, "14:00", "15:00"}, {5, "14:00", "15:00"}}},
	{"Робототехника", 1, 2, []slot{{6, "11:00", "13:00"}}},
}

var tariffSpecs = []struct {
	name    string
	kind    string
	price   int64
	lessons int32
}{
	{"Стандарт, месяц", "monthly", 350_000, 0},
	{"Интенсив, месяц", "monthly", 400_000, 0},
	{"Абонемент на 8 занятий", "package", 400_000, 8},
}

// auditRow — запись журнала; вставляются в конце по времени, чтобы id шли по порядку.
type auditRow struct {
	at             time.Time
	user           *uuid.UUID
	action, entity string
	entityID       *uuid.UUID
	before, after  any
}

type seeder struct {
	ctx   context.Context
	tx    pgx.Tx
	rng   *rand.Rand
	loc   *time.Location
	now   time.Time
	today civil.Date
	cid   uuid.UUID
	owner uuid.UUID
	admin uuid.UUID
	audit []auditRow
}

// at — момент даты d в поясе центра; не позже чем за минуту до «сейчас».
func (s *seeder) at(d civil.Date, h, m int) time.Time {
	return s.notFuture(time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, s.loc))
}

// notFuture — t, но не позже чем за минуту до «сейчас» (утро дня, на который выпало событие).
func (s *seeder) notFuture(t time.Time) time.Time {
	if limit := s.now.Add(-time.Minute); t.After(limit) {
		return limit
	}
	return t
}

func (s *seeder) exec(sql string, args ...any) error {
	_, err := s.tx.Exec(s.ctx, sql, args...)
	return err
}

func (s *seeder) log(at time.Time, user *uuid.UUID, action, entity string, id uuid.UUID, before, after any) {
	s.audit = append(s.audit, auditRow{s.notFuture(at), user, action, entity, &id, before, after})
}

// cashier — кто принял оплату: чаще администратор, иногда владелец.
func (s *seeder) cashier() uuid.UUID {
	if s.rng.IntN(10) < 7 {
		return s.admin
	}
	return s.owner
}

func (s *seeder) method() string {
	switch n := s.rng.IntN(20); {
	case n < 10:
		return "cash"
	case n < 17:
		return "transfer"
	default:
		return "card"
	}
}

func build(ctx context.Context, tx pgx.Tx, now time.Time, owner uuid.UUID, ownerPhone, hash string, old []uuid.UUID) (Result, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return Result{}, err
	}
	today := civil.DateOf(now.In(loc))
	s := &seeder{ctx: ctx, tx: tx, rng: rand.New(rand.NewPCG(uint64(today.Year()), uint64(today.Month())*31+uint64(today.Day()))),
		loc: loc, now: now, today: today, cid: uuid.New(), owner: owner}

	staff := make([]uuid.UUID, len(Staff))
	for i, st := range Staff {
		id, err := upsertUser(ctx, tx, st.Phone, st.Name, hash, true)
		if err != nil {
			return Result{}, err
		}
		staff[i] = id
		if st.Role == "admin" {
			s.admin = id
		}
	}
	// Сотрудники прежнего демо, которых больше нигде нет (например, прежний владелец), — удаляем.
	keep := append([]uuid.UUID{owner}, staff...)
	for _, id := range old {
		if slices.Contains(keep, id) {
			continue
		}
		if err := s.exec(`DELETE FROM users WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM memberships WHERE user_id = $1)`, id); err != nil {
			return Result{}, fmt.Errorf("удаление прежнего сотрудника демо: %w", err)
		}
	}

	periods := make([]civil.Date, 4)
	for k := range periods {
		periods[k] = today.FirstOfMonth().AddMonths(k - 3)
	}
	setup := periods[0].AddDays(-14)
	if err := s.exec(`INSERT INTO centers (id, name, phone, timezone, billing_day, due_days, pay_details, notify_absent, is_demo, created_at)
		VALUES ($1, $2, '+996555000100', $3, 1, $4, $5, true, true, $6)`,
		s.cid, CenterName, timezone, dueDays, PayDetails, s.at(setup, 10, 0)); err != nil {
		return Result{}, err
	}
	if err := s.exec(`INSERT INTO memberships (user_id, center_id, role) VALUES ($1, $2, 'owner')`, owner, s.cid); err != nil {
		return Result{}, err
	}
	for i, st := range Staff {
		if err := s.exec(`INSERT INTO memberships (user_id, center_id, role) VALUES ($1, $2, $3)`, staff[i], s.cid, st.Role); err != nil {
			return Result{}, err
		}
	}

	tariffs := make([]uuid.UUID, len(tariffSpecs))
	for i, t := range tariffSpecs {
		tariffs[i] = uuid.New()
		var lessons *int32
		if t.lessons > 0 {
			lessons = &t.lessons
		}
		if err := s.exec(`INSERT INTO tariffs (id, center_id, name, kind, price, lessons_count) VALUES ($1, $2, $3, $4, $5, $6)`,
			tariffs[i], s.cid, t.name, t.kind, t.price, lessons); err != nil {
			return Result{}, err
		}
		s.log(s.at(setup, 11, i), &owner, "tariff.create", "tariff", tariffs[i], nil,
			map[string]any{"name": t.name, "kind": t.kind, "price": t.price, "lessons_count": lessons})
	}
	groups := make([]uuid.UUID, len(groupSpecs))
	for i, g := range groupSpecs {
		groups[i] = uuid.New()
		sched, _ := json.Marshal(g.slots)
		if err := s.exec(`INSERT INTO groups (id, center_id, name, teacher_id, schedule, default_tariff_id) VALUES ($1, $2, $3, $4, $5, $6)`,
			groups[i], s.cid, g.name, staff[g.teacher], sched, tariffs[g.tariff]); err != nil {
			return Result{}, err
		}
		s.log(s.at(setup, 12, i), &owner, "group.create", "group", groups[i], nil,
			map[string]any{"name": g.name, "teacher_id": staff[g.teacher], "schedule": g.slots, "default_tariff_id": tariffs[g.tariff]})
	}

	// lastDue — последний месяц, срок которого уже прошёл (в начале месяца — прошлый).
	lastDue := 3
	if !periods[3].AddDays(dueDays).Before(today) {
		lastDue = 2
	}

	res := Result{CenterID: s.cid, OwnerPhone: ownerPhone, Students: len(students)}
	parents := map[string]uuid.UUID{}
	type enrolled struct {
		student, enrollment uuid.UUID
		group               int
		start               civil.Date
		end                 *civil.Date
		pkg                 bool
	}
	var active []enrolled
	for i, st := range students {
		// Родитель (у Поповых — общий).
		pid, ok := parents[st.parent]
		if !ok {
			pid = uuid.New()
			parents[st.parent] = pid
			phone := fmt.Sprintf("+996555%06d", 10001+len(parents))
			tg, consent := "active", any(s.at(setup, 9, 0))
			switch {
			case i%5 == 3:
				tg, consent = "none", nil
			case i == 26:
				tg = "stopped"
			}
			if err := s.exec(`INSERT INTO parents (id, center_id, name, phone, tg_status, consent_at) VALUES ($1, $2, $3, $4, $5, $6)`,
				pid, s.cid, st.parent, phone, tg, consent); err != nil {
				return Result{}, err
			}
		}

		start := periods[st.startMonth].AddDays(s.rng.IntN(10))
		switch st.sc {
		case newcomer:
			start = latest(today.AddDays(-2), periods[3])
		case overdue3, overdue2, overdue1, partial, leftDebt, leftPaid:
			start = periods[0].AddDays(s.rng.IntN(5))
		}
		var discount map[string]any
		var dKind *string
		var dValue *int64
		if st.parent == "Ольга Попова" {
			k, v := "percent", int64(10)
			dKind, dValue = &k, &v
			discount = map[string]any{"kind": k, "value": v}
		}
		sid, eid := uuid.New(), uuid.New()
		studentAt := s.at(start, 10+s.rng.IntN(3), s.rng.IntN(60))
		if err := s.exec(`INSERT INTO students (id, center_id, parent_id, full_name, discount_kind, discount_value, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, sid, s.cid, pid, st.name, dKind, dValue, studentAt); err != nil {
			return Result{}, err
		}
		s.log(studentAt, &s.admin, "student.create", "student", sid, nil,
			map[string]any{"full_name": st.name, "birth_date": nil, "discount": discount, "parent_id": pid, "parent_existing": ok})

		g := groupSpecs[st.group]
		tariff := tariffSpecs[g.tariff]
		price := tariff.price
		var disc int64
		if dValue != nil {
			disc = price * *dValue / 100
		}
		e := enrolled{student: sid, enrollment: eid, group: st.group, start: start, pkg: tariff.kind == "package"}
		// Запись — до начислений (на неё ссылается ledger); итоговые поля — в конце.
		if err := s.exec(`INSERT INTO enrollments (id, center_id, student_id, group_id, tariff_id, start_date) VALUES ($1, $2, $3, $4, $5, $6)`,
			eid, s.cid, sid, groups[st.group], tariffs[g.tariff], start); err != nil {
			return Result{}, err
		}
		enrollAudit := map[string]any{
			"student_id": sid, "group_id": groups[st.group], "tariff_id": tariffs[g.tariff], "start_date": start,
			"lessons_left": nil, "first_month": "full", "manual_amount": nil, "billed_through": start.FirstOfMonth(), "package_imported": false,
		}
		if e.pkg {
			enrollAudit["lessons_left"], enrollAudit["billed_through"] = tariff.lessons, nil
		}
		s.log(studentAt.Add(time.Minute), &s.admin, "enrollment.create", "enrollment", eid, nil, enrollAudit)

		var (
			billed      *civil.Date
			lessonsLeft *int32
			leftOn      *civil.Date
		)
		if e.pkg {
			n := int32(2 + s.rng.IntN(5))
			if i == 36 {
				n = 1 // «осталось 1 занятие»
			}
			if st.sc == pkgPending {
				n = 8
			}
			lessonsLeft = &n
			// Покупки: при записи и каждые 8 недель; у pkgPending последняя — вчера, ещё не оплачена.
			var buys []civil.Date
			for d := start; d.Before(today.AddDays(-1)); d = d.AddDays(56) {
				buys = append(buys, d)
			}
			if st.sc == pkgPending {
				buys = append(buys, today.AddDays(-1))
			}
			for b, d := range buys {
				at := s.at(d, 12, 30)
				if b == len(buys)-1 && st.sc == pkgPending {
					at = s.at(d, 19, 10)
				}
				if b > 0 {
					s.log(at, &s.admin, "enrollment.renew", "enrollment", eid,
						map[string]any{"lessons_left": 0}, map[string]any{"lessons_left": tariff.lessons})
				}
				if err := s.charge(sid, eid, nil, d.AddDays(dueDays), price, disc, at, &s.admin); err != nil {
					return Result{}, err
				}
				if st.sc == pkgPending && b == len(buys)-1 {
					continue
				}
				if err := s.payment(sid, price-disc, earliest(d.AddDays(s.rng.IntN(3)), today.AddDays(-1)), s.method()); err != nil {
					return Result{}, err
				}
			}
			if st.sc == pkgPending {
				res.Pending++
			}
		} else {
			first := 0
			for first < 3 && periods[first+1].Compare(start) <= 0 {
				first++
			}
			last := 3
			switch st.sc {
			case leftDebt:
				last, leftOn = 2, ptr(periods[2].AddDays(14))
			case leftPaid:
				last, leftOn = 1, ptr(periods[1].AddDays(20))
			}
			billed = &periods[last]
			// unpaidFrom — с какого месяца не платит; partial — за этот месяц половина.
			unpaidFrom := 4
			switch st.sc {
			case overdue3:
				unpaidFrom = lastDue - 2
			case overdue2:
				unpaidFrom = lastDue - 1
			case overdue1, partial:
				unpaidFrom = lastDue
			case leftDebt:
				unpaidFrom = 2
			case newcomer:
				unpaidFrom = 3
			}
			for k := first; k <= last; k++ {
				p := periods[k]
				due := p.AddDays(dueDays)
				var at time.Time
				var by *uuid.UUID
				if k == first {
					due = start.AddDays(dueDays)
					at, by = studentAt.Add(2*time.Minute), &s.admin // начисление при записи (R5)
				} else {
					at = s.at(p, 0, 5) // billing.monthly
				}
				if err := s.charge(sid, eid, &p, due, price, disc, at, by); err != nil {
					return Result{}, err
				}
				amount := price - disc
				switch {
				case k > unpaidFrom || (k == unpaidFrom && st.sc != partial):
					continue
				case k == unpaidFrom:
					amount = amount / 2 / 10000 * 10000 // половина, круглая сумма в сомах
				}
				payOn := due.AddDays(-s.rng.IntN(7))
				if st.sc == late && k < 3 {
					payOn = due.AddDays(3 + s.rng.IntN(10))
				}
				payOn = earliest(latest(payOn, start, p.AddDays(-3)), today.AddDays(-1))
				if err := s.paymentScenario(st.sc, k, sid, amount, payOn); err != nil {
					return Result{}, err
				}
			}
			switch st.sc {
			case overdue3, overdue2, overdue1, partial, leftDebt:
				res.Overdue++
				if err := s.reminded(pid, sid, periods[max(unpaidFrom, first)].AddDays(dueDays+3)); err != nil {
					return Result{}, err
				}
			case newcomer:
				res.Pending++
			}
		}

		if err := s.exec(`UPDATE enrollments SET end_date = $2, lessons_left = $3, billed_through = $4 WHERE id = $1`,
			eid, leftOn, lessonsLeft, billed); err != nil {
			return Result{}, err
		}
		if leftOn != nil {
			res.Left++
			at := s.at(*leftOn, 18, 0)
			if err := s.exec(`UPDATE students SET status = 'left', left_at = $2 WHERE id = $1`, sid, at); err != nil {
				return Result{}, err
			}
			s.log(at, &s.owner, "enrollment.end", "enrollment", eid,
				map[string]any{"end_date": nil}, map[string]any{"end_date": *leftOn, "reason": "student.left"})
			s.log(at, &s.owner, "student.update", "student", sid,
				map[string]any{"status": "active"}, map[string]any{"status": "left", "left_at": *leftOn})
		}
		e.end = leftOn
		active = append(active, e)
	}

	// Посещаемость за две недели до сегодня (сегодня не отмечено). Одно занятие отменено.
	cancelled := false
	for d := today.AddDays(-14); d.Before(today); d = d.AddDays(1) {
		for gi, g := range groupSpecs {
			for _, sl := range g.slots {
				if sl.Dow != d.ISOWeekday() {
					continue
				}
				lid := uuid.New()
				cancel := !cancelled && gi == 1
				if err := s.exec(`INSERT INTO lessons (id, center_id, group_id, date, start_time, cancelled) VALUES ($1, $2, $3, $4, $5, $6)`,
					lid, s.cid, groups[gi], d, sl.Start, cancel); err != nil {
					return Result{}, err
				}
				if cancel {
					cancelled = true
					s.log(s.at(d.AddDays(-1), 16, 20), &s.owner, "lesson.cancel", "lesson", lid,
						map[string]any{"cancelled": false}, map[string]any{"cancelled": true})
					continue
				}
				var eh, em int
				fmt.Sscanf(sl.End, "%d:%d", &eh, &em)
				marked := s.at(d, eh, em+5+s.rng.IntN(20))
				before, after := map[string]any{}, map[string]any{}
				for _, e := range active {
					if e.group != gi || d.Before(e.start) || (e.end != nil && e.end.Before(d)) {
						continue
					}
					status := "present"
					switch n := s.rng.IntN(20); {
					case n < 2:
						status = "absent"
					case n < 3:
						status = "excused"
					}
					if err := s.exec(`INSERT INTO attendance (lesson_id, student_id, status, marked_by, marked_at, deducted) VALUES ($1, $2, $3, $4, $5, $6)`,
						lid, e.student, status, staff[g.teacher], marked, e.pkg && status == "present"); err != nil {
						return Result{}, err
					}
					before[e.student.String()], after[e.student.String()] = nil, status
				}
				if len(after) > 0 {
					s.log(marked, &staff[g.teacher], "attendance.mark", "lesson", lid, before, after)
				}
			}
		}
	}

	slices.SortStableFunc(s.audit, func(a, b auditRow) int { return a.at.Compare(b.at) })
	for _, a := range s.audit {
		before, err := jsonOrNil(a.before)
		if err != nil {
			return Result{}, err
		}
		after, err := jsonOrNil(a.after)
		if err != nil {
			return Result{}, err
		}
		if err := s.exec(`INSERT INTO audit_log (center_id, user_id, action, entity, entity_id, before, after, at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			s.cid, a.user, a.action, a.entity, a.entityID, before, after, a.at); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

func (s *seeder) charge(sid, eid uuid.UUID, period *civil.Date, due civil.Date, price, disc int64, at time.Time, by *uuid.UUID) error {
	id, at := uuid.New(), s.notFuture(at)
	if err := s.exec(`INSERT INTO ledger (id, center_id, student_id, enrollment_id, kind, amount, period, due_date, list_price, discount, created_by, created_at)
		VALUES ($1, $2, $3, $4, 'charge', $5, $6, $7, $8, $9, $10, $11)`,
		id, s.cid, sid, eid, -(price - disc), period, due, price, disc, by, at); err != nil {
		return err
	}
	s.log(at, by, "ledger.charge", "ledger", id, nil, map[string]any{
		"student_id": sid, "enrollment_id": eid, "amount": -(price - disc), "list_price": price, "discount": disc,
		"period": period, "due_date": due, "comment": nil,
	})
	return nil
}

// payment — оплата в день d; возвращает id и момент.
func (s *seeder) pay(sid uuid.UUID, amount int64, d civil.Date, method string, by uuid.UUID) (uuid.UUID, time.Time, error) {
	id := uuid.New()
	at := s.at(d, 9+s.rng.IntN(10), s.rng.IntN(60))
	if err := s.exec(`INSERT INTO ledger (id, center_id, student_id, kind, amount, method, created_by, created_at)
		VALUES ($1, $2, $3, 'payment', $4, $5, $6, $7)`, id, s.cid, sid, amount, method, by, at); err != nil {
		return id, at, err
	}
	s.log(at, &by, "ledger.payment", "ledger", id, nil,
		map[string]any{"student_id": sid, "kind": "payment", "amount": amount, "method": method, "comment": nil})
	return id, at, nil
}

func (s *seeder) payment(sid uuid.UUID, amount int64, d civil.Date, method string) error {
	_, _, err := s.pay(sid, amount, d, method, s.cashier())
	return err
}

// paymentScenario — оплата месяца k; у учеников с отменой во втором месяце — ошибочная оплата и её отмена.
func (s *seeder) paymentScenario(sc scenario, k int, sid uuid.UUID, amount int64, d civil.Date) error {
	if k != 1 || (sc != cancelSum && sc != cancelTwice && sc != cancelWay) {
		return s.payment(sid, amount, d, s.method())
	}
	wrong, reason, method := amount, "", "cash"
	switch sc {
	case cancelSum:
		wrong, reason = amount*10, "Ошибка в сумме: лишний ноль"
	case cancelTwice:
		reason = "Оплата внесена дважды"
	case cancelWay:
		reason, method = "Не тот способ: оплата была переводом", "transfer"
	}
	if sc == cancelTwice {
		if err := s.payment(sid, amount, d, "cash"); err != nil {
			return err
		}
	}
	id, at, err := s.pay(sid, wrong, d, "cash", s.admin)
	if err != nil {
		return err
	}
	cancelAt := at.Add(time.Duration(4+s.rng.IntN(40)) * time.Minute)
	if err := s.exec(`UPDATE ledger SET cancelled_at = $2, cancelled_by = $3, cancel_reason = $4 WHERE id = $1`,
		id, cancelAt, s.owner, reason); err != nil {
		return err
	}
	s.log(cancelAt, &s.owner, "ledger.cancel", "ledger", id,
		map[string]any{"cancelled": false, "kind": "payment", "amount": wrong, "student_id": sid},
		map[string]any{"cancelled": true, "reason": reason})
	if sc == cancelTwice {
		return nil
	}
	_, _, err = s.pay(sid, amount, d, method, s.admin)
	return err
}

// reminded — напоминание о просрочке, «ушедшее» в прошлом: только запись и дата у родителя.
// telegram_chat_id у демо-родителей пуст — отправить что-то некуда.
func (s *seeder) reminded(pid, sid uuid.UUID, d civil.Date) error {
	if !d.Before(s.today) {
		return nil
	}
	at := s.at(d, 10, 0)
	if err := s.exec(`INSERT INTO notifications (center_id, parent_id, kind, status, sent_at, student_ids)
		SELECT center_id, id, 'overdue_3', 'sent', $2, $3 FROM parents WHERE id = $1 AND tg_status = 'active'`,
		pid, at, []uuid.UUID{sid}); err != nil {
		return err
	}
	return s.exec(`UPDATE parents SET last_reminder_at = $2 WHERE id = $1 AND tg_status = 'active'`, pid, at)
}

func jsonOrNil(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

func ptr[T any](v T) *T { return &v }

func earliest(d civil.Date, more ...civil.Date) civil.Date {
	for _, o := range more {
		if o.Before(d) {
			d = o
		}
	}
	return d
}

func latest(d civil.Date, more ...civil.Date) civil.Date {
	for _, o := range more {
		if d.Before(o) {
			d = o
		}
	}
	return d
}
