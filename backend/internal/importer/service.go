package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"

	"okuu/internal/audit"
	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/groups"
	"okuu/internal/students"
	"okuu/internal/validate"
)

// Статусы строк превью.
const (
	StatusOK      = "ok"
	StatusWarning = "warning"
	StatusError   = "error"
)

// InitialDebtComment — комментарий поправки с начальным долгом.
const InitialDebtComment = "Начальный долг (импорт)"

var (
	ErrNotFound         = errors.New("импорт не найден")
	ErrAlreadyCommitted = errors.New("импорт уже подтверждён")
	ErrExpired          = errors.New("превью импорта устарело")
)

// Row — строка превью. Строки error и строки-дубликаты (Skip) при подтверждении не импортируются.
type Row struct {
	Num         int         `json:"row"` // номер строки как в Excel
	Status      string      `json:"status"`
	Messages    []string    `json:"messages"`
	Skip        bool        `json:"skip"` // дубликат — будет пропущен
	FullName    string      `json:"full_name"`
	ParentName  string      `json:"parent_name"`
	ParentPhone string      `json:"parent_phone"`
	Group       string      `json:"group"`
	GroupID     *uuid.UUID  `json:"group_id"` // nil — группа будет создана
	Tariff      string      `json:"tariff"`
	TariffID    *uuid.UUID  `json:"tariff_id"`
	TariffKind  string      `json:"tariff_kind"` // monthly | package
	StartDate   *civil.Date `json:"start_date"`  // nil — 1-е число текущего месяца
	Debt        int64       `json:"debt"`        // тыйыны
	// LessonsLeft — остаток абонемента из файла: начисление за абонемент не создаётся (он куплен раньше).
	LessonsLeft *int32 `json:"lessons_left"`
}

func (r *Row) importable() bool { return r.Status != StatusError && !r.Skip }

func (r *Row) warn(format string, args ...any) {
	r.Messages = append(r.Messages, fmt.Sprintf(format, args...))
	if r.Status == StatusOK {
		r.Status = StatusWarning
	}
}

func (r *Row) fail(format string, args ...any) {
	r.Messages = append(r.Messages, fmt.Sprintf(format, args...))
	r.Status = StatusError
}

// Summary — итоги превью.
type Summary struct {
	Total    int `json:"total"`
	OK       int `json:"ok"`
	Warning  int `json:"warning"`
	Error    int `json:"error"`
	Skipped  int `json:"skipped"` // дубликаты
	ToImport int `json:"to_import"`
}

// Preview — результат разбора файла.
type Preview struct {
	ID        uuid.UUID
	ExpiresAt time.Time
	Summary   Summary
	Rows      []Row
}

// Result — итоги подтверждения.
type Result struct {
	StudentsCreated    int   `json:"students_created"`
	ParentsCreated     int   `json:"parents_created"`
	GroupsCreated      int   `json:"groups_created"`
	EnrollmentsCreated int   `json:"enrollments_created"`
	Skipped            int   `json:"skipped"`
	DebtTotal          int64 `json:"debt_total"` // тыйыны: сумма начальных долгов
}

// Service — импорт.
type Service struct {
	pool     *pgxpool.Pool
	q        *sqlc.Queries
	students *students.Service
	groups   *groups.Service
	now      func() time.Time
}

// NewService создаёт сервис импорта.
func NewService(pool *pgxpool.Pool, st *students.Service, gr *groups.Service, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, q: sqlc.New(pool), students: st, groups: gr, now: now}
}

// Template — шаблон .xlsx: лист «Ученики» (заголовки и строка-пример) и «Справка» с тарифами и группами центра.
func (s *Service) Template(ctx context.Context, centerID uuid.UUID) ([]byte, error) {
	tariffs, err := s.q.ListTariffs(ctx, sqlc.ListTariffsParams{CenterID: centerID, Archived: false})
	if err != nil {
		return nil, err
	}
	gs, err := s.q.ListActiveGroupNames(ctx, centerID)
	if err != nil {
		return nil, err
	}
	// Пример — по месячному тарифу центра; если есть только абонементы — с остатком занятий.
	exampleTariff, exampleGroup, exampleLessons := "Английский, месяц", "Английский A1", any(nil)
	for _, t := range tariffs {
		if t.Kind == "monthly" {
			exampleTariff, exampleLessons = t.Name, nil
			break
		}
		if exampleLessons == nil && t.LessonsCount != nil {
			exampleTariff, exampleLessons = t.Name, *t.LessonsCount
		}
	}
	if len(gs) > 0 {
		exampleGroup = gs[0].Name
	}

	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Ученики"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	text, _ := f.NewStyle(&excelize.Style{NumFmt: 49}) // «Текстовый»: Excel не съест ноль в телефоне
	for i, h := range TemplateHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellStr(sheet, cell, h)
	}
	_ = f.SetRowStyle(sheet, 1, 1, bold)
	_ = f.SetColStyle(sheet, "C", text)
	example := []any{"Асанов Бакыт", "Асанова Гульнара", "0700 123 456", exampleGroup, exampleTariff, "01.09.2024", 1500, exampleLessons}
	for i, v := range example {
		if v == nil {
			continue
		}
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		_ = f.SetCellValue(sheet, cell, v)
	}
	for col, w := range map[string]float64{"A": 28, "B": 26, "C": 20, "D": 22, "E": 24, "F": 14, "G": 20, "H": 30} {
		_ = f.SetColWidth(sheet, col, col, w)
	}

	const help = "Справка"
	if _, err := f.NewSheet(help); err != nil {
		return nil, err
	}
	_ = f.SetCellStr(help, "A1", "Тарифы центра (колонка «Тариф» — точное название)")
	_ = f.SetCellStr(help, "B1", "Вид")
	_ = f.SetCellStr(help, "C1", "Группы центра (новые будут созданы без расписания)")
	_ = f.SetCellStr(help, "E1", "Абонемент: заполните «Осталось занятий» (0–200) — абонемент не начисляется заново, долг — только из «Текущий долг».")
	for i, t := range tariffs {
		_ = f.SetCellStr(help, fmt.Sprintf("A%d", i+2), t.Name)
		kind := "в месяц"
		if t.Kind == "package" {
			kind = "абонемент — нужен «Осталось занятий»"
		}
		_ = f.SetCellStr(help, fmt.Sprintf("B%d", i+2), kind)
	}
	for i, g := range gs {
		_ = f.SetCellStr(help, fmt.Sprintf("C%d", i+2), g.Name)
	}
	_ = f.SetRowStyle(help, 1, 1, bold)
	_ = f.SetColWidth(help, "A", "C", 40)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Preview разбирает файл, проверяет каждую строку и сохраняет превью на час.
func (s *Service) Preview(ctx context.Context, actor, centerID uuid.UUID, filename string, data []byte) (Preview, error) {
	table, err := readTable(data)
	if err != nil {
		return Preview{}, err
	}
	raws, err := parseTable(table)
	if err != nil {
		return Preview{}, err
	}
	lk, err := s.lookups(ctx, s.q, centerID)
	if err != nil {
		return Preview{}, err
	}

	rows := make([]Row, 0, len(raws))
	seen := map[string]int{}           // ключ ФИО+телефон → номер строки в файле
	fileParents := map[string]string{} // телефон → имя родителя в файле
	for _, raw := range raws {
		r := checkRow(raw, lk)
		if r.Status != StatusError {
			key := normName(r.FullName) + "|" + r.ParentPhone
			switch {
			case lk.students[key]:
				r.Skip = true
				r.warn("Ученик «%s» с телефоном родителя %s уже есть в центре — строка будет пропущена", r.FullName, r.ParentPhone)
			case seen[key] != 0:
				r.Skip = true
				r.warn("Повторяет строку %d — будет пропущена", seen[key])
			default:
				seen[key] = r.Num
			}
			if !r.Skip {
				if name, ok := lk.parents[r.ParentPhone]; ok && normName(name) != normName(r.ParentName) {
					r.warn("Родитель с телефоном %s уже есть как «%s» — имя не изменится", r.ParentPhone, name)
				} else if name, ok := fileParents[r.ParentPhone]; ok && normName(name) != normName(r.ParentName) {
					r.warn("Телефон %s выше в файле указан для родителя «%s» — будет один родитель с этим именем", r.ParentPhone, name)
				} else if !ok {
					fileParents[r.ParentPhone] = r.ParentName
				}
			}
		}
		rows = append(rows, r)
	}

	data, err = json.Marshal(rows)
	if err != nil {
		return Preview{}, err
	}
	// Имя файла приходит от клиента: может быть не в UTF-8 (Postgres такое не примет) и любой длины.
	var fn *string
	if name := strings.ToValidUTF8(filename, "?"); name != "" {
		if r := []rune(name); len(r) > 200 {
			name = string(r[:200])
		}
		fn = &name
	}
	sess, err := s.q.CreateImportSession(ctx, sqlc.CreateImportSessionParams{CenterID: centerID, CreatedBy: actor, Filename: fn, Rows: data})
	if err != nil {
		return Preview{}, err
	}
	return Preview{ID: sess.ID, ExpiresAt: sess.ExpiresAt, Summary: summarize(rows), Rows: rows}, nil
}

type lookups struct {
	today    civil.Date
	tariffs  map[string][]sqlc.Tariff // normName → активные тарифы
	groups   map[string]uuid.UUID     // normName → активная группа
	students map[string]bool          // normName(ФИО)|телефон
	parents  map[string]string        // телефон → имя
}

func (s *Service) lookups(ctx context.Context, q *sqlc.Queries, centerID uuid.UUID) (lookups, error) {
	lk := lookups{tariffs: map[string][]sqlc.Tariff{}, groups: map[string]uuid.UUID{}, students: map[string]bool{}, parents: map[string]string{}}
	c, err := q.GetCenter(ctx, centerID)
	if err != nil {
		return lk, err
	}
	if lk.today, err = civil.Today(s.now(), c.Timezone); err != nil {
		return lk, err
	}
	ts, err := q.ListTariffs(ctx, sqlc.ListTariffsParams{CenterID: centerID, Archived: false})
	if err != nil {
		return lk, err
	}
	for _, t := range ts {
		lk.tariffs[normName(t.Name)] = append(lk.tariffs[normName(t.Name)], t)
	}
	gs, err := q.ListActiveGroupNames(ctx, centerID)
	if err != nil {
		return lk, err
	}
	for _, g := range gs {
		if _, ok := lk.groups[normName(g.Name)]; !ok {
			lk.groups[normName(g.Name)] = g.ID
		}
	}
	keys, err := q.ListExistingStudentKeys(ctx, centerID)
	if err != nil {
		return lk, err
	}
	for _, k := range keys {
		lk.students[normName(k.FullName)+"|"+k.Phone] = true
	}
	ps, err := q.ListParentsByCenter(ctx, centerID)
	if err != nil {
		return lk, err
	}
	for _, p := range ps {
		lk.parents[p.Phone] = p.Name
	}
	return lk, nil
}

// checkRow разбирает и проверяет одну строку (без дубликатов — их ищет Preview).
func checkRow(raw rawRow, lk lookups) Row {
	v := raw.Values
	r := Row{Num: raw.Num, Status: StatusOK, Messages: []string{},
		FullName: strings.Join(strings.Fields(v[colFullName]), " "), ParentName: strings.Join(strings.Fields(v[colParentName]), " "),
		Group: strings.Join(strings.Fields(v[colGroup]), " "), Tariff: strings.Join(strings.Fields(v[colTariff]), " ")}

	if _, err := validate.Name("full_name", "ФИО", r.FullName); err != nil {
		r.fail("ФИО ученика: %s", lowerFirst(err.(*validate.Error).Message))
	}
	if _, err := validate.Name("parent_name", "Имя", r.ParentName); err != nil {
		r.fail("Имя родителя: %s", lowerFirst(err.(*validate.Error).Message))
	}
	if v[colParentPhone] == "" {
		r.fail("Телефон родителя не указан")
	} else if phone, warning, err := parsePhone(v[colParentPhone]); err != nil {
		r.fail("Телефон родителя: %s", err)
	} else {
		r.ParentPhone = phone
		if warning != "" {
			r.warn("%s", warning)
		}
	}

	switch ts := lk.tariffs[normName(r.Tariff)]; {
	case r.Tariff == "":
		r.fail("Тариф не указан")
	case len(ts) == 0:
		r.fail("Тариф «%s» не найден — создайте его в настройках или исправьте название", r.Tariff)
	case len(ts) > 1:
		r.fail("Несколько тарифов называются «%s» — переименуйте их, чтобы различались", r.Tariff)
	default:
		r.TariffID, r.Tariff, r.TariffKind = &ts[0].ID, ts[0].Name, ts[0].Kind
	}

	if r.Group == "" {
		r.fail("Группа не указана")
	} else if id, ok := lk.groups[normName(r.Group)]; ok {
		r.GroupID = &id
	} else if _, err := validate.Name("group", "Группа", r.Group); err != nil {
		r.fail("Группа: %s", lowerFirst(err.(*validate.Error).Message))
	} else {
		r.warn("Группа «%s» не найдена — будет создана без расписания", r.Group)
	}

	if d, err := parseDate(v[colStartDate]); err != nil {
		r.fail("Дата начала: %s", err)
	} else if !d.IsZero() {
		if earliest := lk.today.AddMonths(-12 * students.MaxPastYears); d.Before(earliest) {
			r.fail("Дата начала %s раньше %s — проверьте год", d, earliest)
		} else {
			r.StartDate = &d
		}
	}

	if debt, err := parseDebt(v[colDebt]); err != nil {
		r.fail("Текущий долг: %s", err)
	} else {
		r.Debt = debt
	}

	// Абонемент из Excel уже куплен: не начисляем его заново, остаток — из файла. Долг — только «Текущий долг».
	lessons, err := parseLessons(v[colLessonsLeft])
	switch {
	case err != nil:
		r.fail("Осталось занятий: %s", err)
	case r.TariffKind == "package" && lessons == nil:
		r.fail("Осталось занятий — обязательно для абонемента «%s» (0–%d)", r.Tariff, MaxLessonsLeft)
	case r.TariffKind == "package":
		r.LessonsLeft = lessons
	case lessons != nil && r.TariffKind != "":
		r.warn("«Осталось занятий» для месячного тарифа «%s» не учитывается", r.Tariff)
	}
	return r
}

// Commit импортирует строки превью одной транзакцией: родители (по телефону), ученики, недостающие
// группы, записи (дата из файла или 1-е число текущего месяца; текущий месяц — по firstMonth, R5),
// начальный долг — поправкой «Начальный долг (импорт)». Ошибка любой строки откатывает всё.
func (s *Service) Commit(ctx context.Context, actor, centerID, id uuid.UUID, firstMonth string) (Result, error) {
	if firstMonth != students.FirstMonthNone && firstMonth != students.FirstMonthFull {
		return Result{}, validate.Errorf("first_month", "first_month — none или full")
	}
	var res Result
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		sess, err := q.LockImportSession(ctx, sqlc.LockImportSessionParams{CenterID: centerID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if sess.CommittedAt != nil {
			return ErrAlreadyCommitted
		}
		if sess.Expired {
			return ErrExpired
		}
		var rows []Row
		if err := json.Unmarshal(sess.Rows, &rows); err != nil {
			return fmt.Errorf("превью %s: %w", id, err)
		}
		// Данные могли измениться за час: дубликаты и группы проверяем заново.
		lk, err := s.lookups(ctx, q, centerID)
		if err != nil {
			return err
		}
		created := map[string]uuid.UUID{} // новые группы по normName
		for _, r := range rows {
			if !r.importable() {
				if r.Skip {
					res.Skipped++
				}
				continue
			}
			key := normName(r.FullName) + "|" + r.ParentPhone
			if lk.students[key] {
				res.Skipped++
				continue
			}
			lk.students[key] = true

			groupID, ok := lk.groups[normName(r.Group)]
			if !ok {
				if groupID, ok = created[normName(r.Group)]; !ok {
					if groupID, err = s.groups.CreateTx(ctx, tx, actor, centerID, groups.Input{Name: r.Group}); err != nil {
						return rowError(r.Num, err)
					}
					created[normName(r.Group)] = groupID
					res.GroupsCreated++
				}
			}

			sid, parentExisting, err := s.students.CreateTx(ctx, tx, actor, centerID, students.CreateInput{
				FullName: r.FullName, Parent: students.ParentInput{Name: r.ParentName, Phone: r.ParentPhone},
			})
			if err != nil {
				return rowError(r.Num, err)
			}
			res.StudentsCreated++
			if !parentExisting {
				res.ParentsCreated++
			}

			start := lk.today.FirstOfMonth()
			if r.StartDate != nil {
				start = *r.StartDate
			}
			if _, _, err := s.students.EnrollTx(ctx, tx, actor, centerID, sid, students.EnrollInput{
				GroupID: groupID, TariffID: r.TariffID, StartDate: start, FirstMonth: firstMonth,
				PackageLessonsLeft: r.LessonsLeft, // абонемент: без начисления, first_month не влияет
			}); err != nil {
				return rowError(r.Num, err)
			}
			res.EnrollmentsCreated++

			if r.Debt > 0 {
				if _, err := billing.CreateCorrectionTx(ctx, tx, actor, centerID, sid, -r.Debt, InitialDebtComment); err != nil {
					return rowError(r.Num, err)
				}
				res.DebtTotal += r.Debt
			}
		}

		result, err := json.Marshal(res)
		if err != nil {
			return err
		}
		if err := q.CommitImportSession(ctx, sqlc.CommitImportSessionParams{CenterID: centerID, ID: id, Result: result}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.ImportCommit, Entity: "import", EntityID: &id,
			After: map[string]any{
				"students_created": res.StudentsCreated, "parents_created": res.ParentsCreated,
				"groups_created": res.GroupsCreated, "enrollments_created": res.EnrollmentsCreated,
				"skipped": res.Skipped, "debt_total": res.DebtTotal, "first_month": firstMonth,
			},
		})
	})
	return res, err
}

// rowError добавляет к ошибке номер строки: ошибки данных — 422 на поле rows.
func rowError(num int, err error) error {
	var verr *validate.Error
	if errors.As(err, &verr) {
		return validate.Unprocessablef("rows", "Строка %d: %s", num, verr.Message)
	}
	if errors.Is(err, students.ErrAlreadyEnrolled) || errors.Is(err, students.ErrParentPhoneTaken) {
		return validate.Unprocessablef("rows", "Строка %d: %s", num, err)
	}
	return fmt.Errorf("строка %d: %w", num, err)
}

func summarize(rows []Row) Summary {
	s := Summary{Total: len(rows)}
	for _, r := range rows {
		switch r.Status {
		case StatusOK:
			s.OK++
		case StatusWarning:
			s.Warning++
		case StatusError:
			s.Error++
		}
		if r.Skip {
			s.Skipped++
		}
		if r.importable() {
			s.ToImport++
		}
	}
	return s
}

func lowerFirst(s string) string {
	for i, r := range s {
		return strings.ToLower(string(r)) + s[i+len(string(r)):]
	}
	return s
}

func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx, q *sqlc.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx, s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
