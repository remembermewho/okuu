// Package students — ученики, родители и записи в группы (F3, R5–R8).
// Родитель создаётся вместе с учеником; родитель с тем же телефоном в центре переиспользуется,
// так у одного родителя бывает несколько детей.
package students

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/audit"
	"okuu/internal/auth"
	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/notify"
	"okuu/internal/optional"
	"okuu/internal/tariffs"
	"okuu/internal/validate"
)

// Статусы ученика.
const (
	StatusActive = "active"
	StatusPaused = "paused"
	StatusLeft   = "left"
)

const (
	DefaultLimit = 50
	MaxLimit     = 200
)

var (
	// ErrNotFound — ученика (или записи) нет в этом центре.
	ErrNotFound = errors.New("не найдено")
	// ErrParentPhoneTaken — телефон занят другим родителем этого центра.
	ErrParentPhoneTaken = errors.New("телефон занят другим родителем")
	// ErrAlreadyEnrolled — у ученика уже есть активная запись в эту группу.
	ErrAlreadyEnrolled = errors.New("ученик уже в этой группе")
	// ErrEnrollmentEnded — запись уже завершена.
	ErrEnrollmentEnded = errors.New("запись завершена")
	// ErrNotPackage — продлевать можно только абонемент.
	ErrNotPackage = errors.New("тариф записи — не абонемент")
	// ErrStudentLeft — ученик ушёл (R7): новые записи и продления не делаем.
	ErrStudentLeft = errors.New("ученик ушёл")
)

// Service — ученики, родители, записи.
type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
	now  func() time.Time
}

// NewService создаёт сервис; now — часы (nil — time.Now), «сегодня» считается в поясе центра.
func NewService(pool *pgxpool.Pool, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, q: sqlc.New(pool), now: now}
}

// Parent — родитель ученика.
type Parent struct {
	ID             uuid.UUID
	Name           string
	Phone          string
	TgStatus       string
	LastReminderAt *time.Time // последнее напоминание в Telegram
	LastWhatsAppAt *time.Time // когда последний раз открывали WhatsApp-напоминание
}

// Enrollment — запись ученика в группу с данными для карточки.
type Enrollment struct {
	ID            uuid.UUID
	GroupID       uuid.UUID
	GroupName     string
	TariffID      uuid.UUID
	TariffName    string
	TariffKind    string
	TariffPrice   int64
	StartDate     civil.Date
	EndDate       *civil.Date
	LessonsLeft   *int32
	BilledThrough *civil.Date // monthly: последний период, по которому решение о начислении принято
}

// Active — запись не завершена.
func (e Enrollment) Active() bool { return e.EndDate == nil }

// Student — карточка ученика.
type Student struct {
	ID          uuid.UUID
	FullName    string
	BirthDate   *civil.Date
	Status      string
	Discount    *billing.Discount
	CreatedAt   time.Time
	Parent      *Parent
	Enrollments []Enrollment
	Balance     int64
}

// GroupRef — группа в строке списка.
type GroupRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ListItem — строка списка учеников.
type ListItem struct {
	ID        uuid.UUID
	FullName  string
	BirthDate *civil.Date
	Status    string
	Discount  *billing.Discount
	Parent    *Parent
	Groups    []GroupRef
	Balance   int64
}

// ParentInput — родитель нового ученика.
type ParentInput struct {
	Name  string
	Phone string
}

// CreateInput — новый ученик.
type CreateInput struct {
	FullName  string
	BirthDate *civil.Date
	Parent    ParentInput
	Discount  *billing.Discount
}

// Create создаёт ученика и, если нужно, родителя. parentExisting=true — родитель с этим
// телефоном уже был в центре: используем его как есть, имя не перезаписываем.
func (s *Service) Create(ctx context.Context, actor, centerID uuid.UUID, in CreateInput) (st Student, parentExisting bool, err error) {
	var id uuid.UUID
	err = s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		id, parentExisting, err = s.CreateTx(ctx, tx, actor, centerID, in)
		return err
	})
	if err != nil {
		return Student{}, false, err
	}
	st, err = s.Get(ctx, centerID, id)
	return st, parentExisting, err
}

// CreateTx — то же, что Create, в транзакции вызывающего (импорт); возвращает id ученика.
func (s *Service) CreateTx(ctx context.Context, tx pgx.Tx, actor, centerID uuid.UUID, in CreateInput) (id uuid.UUID, parentExisting bool, err error) {
	name, err := validate.Name("full_name", "ФИО", in.FullName)
	if err != nil {
		return uuid.Nil, false, err
	}
	if err := billing.ValidateDiscount(in.Discount, tariffs.MaxPrice); err != nil {
		return uuid.Nil, false, err
	}
	pName, pPhone, err := validateParent(in.Parent.Name, in.Parent.Phone)
	if err != nil {
		return uuid.Nil, false, err
	}
	q := s.q.WithTx(tx)
	today, err := s.centerToday(ctx, q, centerID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if err := validateBirthDate(in.BirthDate, today); err != nil {
		return uuid.Nil, false, err
	}
	parent, existing, err := upsertParent(ctx, q, centerID, pName, pPhone)
	if err != nil {
		return uuid.Nil, false, err
	}
	kind, value := discountColumns(in.Discount)
	row, err := q.CreateStudent(ctx, sqlc.CreateStudentParams{
		CenterID: centerID, ParentID: &parent.ID, FullName: name, BirthDate: in.BirthDate,
		DiscountKind: kind, DiscountValue: value,
	})
	if err != nil {
		return uuid.Nil, false, err
	}
	if err := audit.Record(ctx, tx, audit.Entry{
		CenterID: centerID, UserID: &actor, Action: audit.StudentCreate, Entity: "student", EntityID: &row.ID,
		After: map[string]any{
			"full_name": name, "birth_date": in.BirthDate, "discount": in.Discount,
			"parent_id": parent.ID, "parent_existing": existing,
		},
	}); err != nil {
		return uuid.Nil, false, err
	}
	return row.ID, existing, nil
}

// Get — карточка ученика: родитель, все записи, баланс.
func (s *Service) Get(ctx context.Context, centerID, id uuid.UUID) (Student, error) {
	row, err := s.q.GetStudent(ctx, sqlc.GetStudentParams{CenterID: centerID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Student{}, ErrNotFound
	}
	if err != nil {
		return Student{}, err
	}
	enr, err := s.listEnrollments(ctx, s.q, centerID, id)
	if err != nil {
		return Student{}, err
	}
	return Student{
		ID: row.ID, FullName: row.FullName, BirthDate: row.BirthDate, Status: row.Status,
		Discount: discountOf(row.DiscountKind, row.DiscountValue), CreatedAt: row.CreatedAt,
		Parent:      parentOf(row.ParentID, row.ParentName, row.ParentPhone, row.ParentTgStatus, row.ParentLastReminderAt, row.ParentLastWhatsappAt),
		Enrollments: enr, Balance: row.Balance,
	}, nil
}

// ListFilter — фильтры списка; пустые значения не фильтруют.
type ListFilter struct {
	Q       string     // ФИО или телефон родителя
	GroupID *uuid.UUID // активная запись в группу
	Status  string     // active | paused | left
	Tg      string     // tg_status родителя: none | active | stopped | blocked
	// HideLeft — без ушедших (список учеников по умолчанию); не действует, если задан Status.
	HideLeft bool
	Limit    int
	Cursor   string
}

// Page — страница списка учеников: Total — всего под фильтрами (на всех страницах).
type Page struct {
	Items      []ListItem
	NextCursor string // "" — страница последняя
	Total      int64
}

// List — ученики по алфавиту с балансом.
func (s *Service) List(ctx context.Context, centerID uuid.UUID, f ListFilter) (Page, error) {
	items, next, total, err := s.list(ctx, centerID, f)
	return Page{Items: items, NextCursor: next, Total: total}, err
}

func (s *Service) list(ctx context.Context, centerID uuid.UUID, f ListFilter) (items []ListItem, nextCursor string, total int64, err error) {
	p := sqlc.ListStudentsParams{CenterID: centerID, GroupID: f.GroupID, HideLeft: f.HideLeft && f.Status == ""}
	switch f.Status {
	case "":
	case StatusActive, StatusPaused, StatusLeft:
		p.Status = &f.Status
	default:
		return nil, "", 0, validate.Errorf("status", "Статус — active, paused или left")
	}
	switch f.Tg {
	case "":
	case "none", "active", "stopped", "blocked":
		p.Tg = &f.Tg
	default:
		return nil, "", 0, validate.Errorf("tg", "tg — none, active, stopped или blocked")
	}
	limit := f.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return nil, "", 0, validate.Errorf("limit", "limit — от 1 до %d", MaxLimit)
	}
	p.Lim = int32(limit + 1) // +1 — узнать, есть ли следующая страница
	if q := strings.TrimSpace(f.Q); q != "" {
		name := "%" + escapeLike(q) + "%"
		p.NameQ = &name
		if digits := phoneDigits(q); len(digits) >= 3 {
			phone := "%" + digits + "%"
			p.PhoneQ = &phone
		}
	}
	if f.Cursor != "" {
		c, err := decodeCursor(f.Cursor)
		if err != nil {
			return nil, "", 0, err
		}
		p.AfterName, p.AfterID = &c.Name, &c.ID
	}

	rows, err := s.q.ListStudents(ctx, p)
	if err != nil {
		return nil, "", 0, err
	}
	// Всего под фильтрами (без курсора) — подпись «38 учеников» вместо «30+».
	if total, err = s.q.CountStudents(ctx, sqlc.CountStudentsParams{CenterID: centerID, Status: p.Status, HideLeft: p.HideLeft,
		Tg: p.Tg, GroupID: p.GroupID, NameQ: p.NameQ, PhoneQ: p.PhoneQ}); err != nil {
		return nil, "", 0, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		nextCursor = encodeCursor(cursor{Name: last.FullName, ID: last.ID})
	}
	items = make([]ListItem, 0, len(rows))
	for _, r := range rows {
		it := ListItem{
			ID: r.ID, FullName: r.FullName, BirthDate: r.BirthDate, Status: r.Status,
			Discount: discountOf(r.DiscountKind, r.DiscountValue),
			Parent:   parentOf(r.ParentID, r.ParentName, r.ParentPhone, r.ParentTgStatus, r.ParentLastReminderAt, r.ParentLastWhatsappAt),
			Balance:  r.Balance,
		}
		if err := json.Unmarshal(r.Groups, &it.Groups); err != nil {
			return nil, "", 0, fmt.Errorf("группы ученика %s: %w", r.ID, err)
		}
		items = append(items, it)
	}
	return items, nextCursor, total, nil
}

// ParentPatch — изменение родителя ученика (и всех его детей: родитель общий).
type ParentPatch struct {
	Name  optional.Field[string]
	Phone optional.Field[string]
}

// Patch — частичное изменение ученика.
type Patch struct {
	FullName  optional.Field[string]
	BirthDate optional.Field[civil.Date]
	Status    optional.Field[string]
	Discount  optional.Field[billing.Discount]
	Parent    *ParentPatch
	// LeftDate — последний день занятий; только вместе со Status = left (без него — сегодня).
	LeftDate *civil.Date
	// CancelFutureCharges — при уходе отменить неотменённые начисления за месяцы после месяца последнего дня
	// (обычная отмена с причиной LeftCancelReason и записью в журнал); только вместе со Status = left.
	CancelFutureCharges bool
}

// LeftCancelReason — причина отмены начислений за месяцы после ухода.
const LeftCancelReason = "ученик ушёл"

// MaxLeftAhead — дату ухода можно поставить не дальше сегодня + столько дней.
const MaxLeftAhead = 30

// Update меняет ученика и родителя. Скидка действует со следующего начисления (R6).
// status=left закрывает все активные записи сегодняшним днём (R7); paused записи не трогает.
func (s *Service) Update(ctx context.Context, actor, centerID, id uuid.UUID, p Patch) (Student, error) {
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		cur, err := q.GetStudentForUpdate(ctx, sqlc.GetStudentForUpdateParams{CenterID: centerID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		center, err := s.center(ctx, q, centerID)
		if err != nil {
			return err
		}
		today := center.Today

		before := studentSnapshot(cur)
		next := cur
		if p.FullName.Set {
			if p.FullName.Value == nil {
				return validate.Errorf("full_name", "ФИО — обязательное поле")
			}
			if next.FullName, err = validate.Name("full_name", "ФИО", *p.FullName.Value); err != nil {
				return err
			}
		}
		if p.BirthDate.Set {
			next.BirthDate = p.BirthDate.Value
			if err := validateBirthDate(next.BirthDate, today); err != nil {
				return err
			}
		}
		if p.Status.Set {
			if p.Status.Value == nil {
				return validate.Errorf("status", "Статус — active, paused или left")
			}
			switch *p.Status.Value {
			case StatusActive, StatusPaused, StatusLeft:
				next.Status = *p.Status.Value
			default:
				return validate.Errorf("status", "Статус — active, paused или left")
			}
		}
		if p.Discount.Set {
			if err := billing.ValidateDiscount(p.Discount.Value, tariffs.MaxPrice); err != nil {
				return err
			}
			if err := checkFixedDiscount(ctx, q, centerID, id, p.Discount.Value); err != nil {
				return err
			}
			next.DiscountKind, next.DiscountValue = discountColumns(p.Discount.Value)
		}

		if p.Parent != nil {
			parentID, err := s.updateParent(ctx, tx, q, actor, centerID, cur.ParentID, *p.Parent)
			if err != nil {
				return err
			}
			next.ParentID = parentID
		}

		leaving := next.Status == StatusLeft && cur.Status != StatusLeft
		if p.LeftDate != nil && !leaving {
			return validate.Errorf("left_date", "Дата ухода — только вместе с переводом в «ушёл»")
		}
		if p.CancelFutureCharges && !leaving {
			return validate.Errorf("cancel_future_charges", "Отмена начислений — только вместе с переводом в «ушёл»")
		}
		var leftAt *time.Time
		if leaving {
			leftDate := today
			if p.LeftDate != nil {
				leftDate = *p.LeftDate
				if err := s.checkLeftDate(ctx, q, centerID, id, leftDate, today); err != nil {
					return err
				}
			}
			at, err := civil.StartOfDay(leftDate, center.Timezone)
			if err != nil {
				return err
			}
			leftAt = &at
			// Записи закрываются последним днём занятий: на занятиях после него ученика нет.
			ended, err := q.EndActiveEnrollments(ctx, sqlc.EndActiveEnrollmentsParams{CenterID: centerID, StudentID: id, Today: leftDate})
			if err != nil {
				return err
			}
			for _, e := range ended {
				if err := audit.Record(ctx, tx, audit.Entry{
					CenterID: centerID, UserID: &actor, Action: audit.EnrollEnd, Entity: "enrollment", EntityID: &e.ID,
					Before: map[string]any{"end_date": nil}, After: map[string]any{"end_date": e.EndDate, "reason": "student.left"},
				}); err != nil {
					return err
				}
			}
			if p.CancelFutureCharges {
				if err := cancelChargesAfter(ctx, tx, q, actor, centerID, id, leftDate.FirstOfMonth()); err != nil {
					return err
				}
			}
		}

		after := studentSnapshot(next)
		if jsonEqual(before, after) {
			return nil
		}
		if _, err := q.UpdateStudent(ctx, sqlc.UpdateStudentParams{
			CenterID: centerID, ID: id, FullName: next.FullName, BirthDate: next.BirthDate, Status: next.Status,
			DiscountKind: next.DiscountKind, DiscountValue: next.DiscountValue, ParentID: next.ParentID, LeftAt: leftAt,
		}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.StudentUpdate, Entity: "student", EntityID: &id,
			Before: before, After: after,
		})
	})
	if err != nil {
		return Student{}, err
	}
	return s.Get(ctx, centerID, id)
}

// cancelChargesAfter — уход: начисления за месяцы позже after (месяц последнего дня) отменяются обычной
// отменой (R15) с причиной «ученик ушёл» и записью ledger.cancel в журнал, как кнопка «Отменить».
func cancelChargesAfter(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, actor, centerID, studentID uuid.UUID, after civil.Date) error {
	rows, err := q.ListChargesAfterPeriod(ctx, sqlc.ListChargesAfterPeriodParams{CenterID: centerID, StudentID: studentID, After: after})
	if err != nil {
		return err
	}
	reason := LeftCancelReason
	for _, cur := range rows {
		if _, err := q.CancelOperation(ctx, sqlc.CancelOperationParams{CenterID: centerID, ID: cur.ID, CancelledBy: &actor, CancelReason: &reason}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.LedgerCancel, Entity: "ledger", EntityID: &cur.ID,
			Before: map[string]any{"cancelled": false, "kind": cur.Kind, "amount": cur.Amount, "student_id": cur.StudentID},
			After:  map[string]any{"cancelled": true, "reason": reason},
		}); err != nil {
			return err
		}
	}
	return nil
}

// checkLeftDate — последний день занятий: не раньше начала самой поздней активной записи (иначе запись
// закрылась бы раньше, чем началась) и не позже сегодня + MaxLeftAhead.
func (s *Service) checkLeftDate(ctx context.Context, q *sqlc.Queries, centerID, studentID uuid.UUID, d, today civil.Date) error {
	if latest := today.AddDays(MaxLeftAhead); d.After(latest) {
		return validate.Errorf("left_date", "Последний день занятий — не позже %s", ruDate(latest))
	}
	enrollments, err := q.ListStudentEnrollments(ctx, sqlc.ListStudentEnrollmentsParams{CenterID: centerID, StudentID: studentID})
	if err != nil {
		return err
	}
	for _, e := range enrollments {
		if e.EndDate == nil && d.Before(e.StartDate) {
			return validate.Errorf("left_date", "Последний день занятий — не раньше начала записи в «%s» (%s)", e.GroupName, ruDate(e.StartDate))
		}
	}
	return nil
}

// ruDate — «19.10.2026» для сообщений об ошибках.
func ruDate(d civil.Date) string {
	return fmt.Sprintf("%02d.%02d.%d", d.Day(), int(d.Month()), d.Year())
}

// checkFixedDiscount — фиксированная скидка не больше цены каждого активного тарифа ученика (иначе
// начисление ушло бы в 0, и это почти всегда опечатка). 422 validation, поле discount.
func checkFixedDiscount(ctx context.Context, q *sqlc.Queries, centerID, studentID uuid.UUID, d *billing.Discount) error {
	if d == nil || d.Kind != billing.DiscountFixed {
		return nil
	}
	enrollments, err := q.ListStudentEnrollments(ctx, sqlc.ListStudentEnrollmentsParams{CenterID: centerID, StudentID: studentID})
	if err != nil {
		return err
	}
	for _, e := range enrollments {
		if e.EndDate == nil && d.Value > e.TariffPrice {
			return validate.Unprocessablef("discount", "Скидка %s с больше цены тарифа «%s» (%s с)",
				notify.Som(d.Value), e.TariffName, notify.Som(e.TariffPrice))
		}
	}
	return nil
}

// updateParent меняет родителя ученика; если родителя нет — находит/создаёт по телефону и привязывает.
func (s *Service) updateParent(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, actor, centerID uuid.UUID,
	parentID *uuid.UUID, p ParentPatch) (*uuid.UUID, error) {
	if parentID == nil {
		if !p.Name.Set || !p.Phone.Set || p.Name.Value == nil || p.Phone.Value == nil {
			return nil, validate.Errorf("parent", "У ученика нет родителя — укажите имя и телефон")
		}
		name, phone, err := validateParent(*p.Name.Value, *p.Phone.Value)
		if err != nil {
			return nil, err
		}
		parent, _, err := upsertParent(ctx, q, centerID, name, phone)
		if err != nil {
			return nil, err
		}
		return &parent.ID, nil
	}

	cur, err := q.GetParentForUpdate(ctx, sqlc.GetParentForUpdateParams{CenterID: centerID, ID: *parentID})
	if err != nil {
		return nil, err
	}
	name, phone := cur.Name, cur.Phone
	if p.Name.Set {
		if p.Name.Value == nil {
			return nil, validate.Errorf("parent.name", "Имя родителя — обязательное поле")
		}
		name = *p.Name.Value
	}
	if p.Phone.Set {
		if p.Phone.Value == nil {
			return nil, validate.Errorf("parent.phone", "Телефон родителя — обязательное поле")
		}
		phone = *p.Phone.Value
	}
	if name, phone, err = validateParent(name, phone); err != nil {
		return nil, err
	}
	if name == cur.Name && phone == cur.Phone {
		return parentID, nil
	}
	if _, err := q.UpdateParent(ctx, sqlc.UpdateParentParams{CenterID: centerID, ID: cur.ID, Name: name, Phone: phone}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrParentPhoneTaken
		}
		return nil, err
	}
	if err := audit.Record(ctx, tx, audit.Entry{
		CenterID: centerID, UserID: &actor, Action: audit.ParentUpdate, Entity: "parent", EntityID: &cur.ID,
		Before: map[string]any{"name": cur.Name, "phone": cur.Phone}, After: map[string]any{"name": name, "phone": phone},
	}); err != nil {
		return nil, err
	}
	return parentID, nil
}

// upsertParent — родитель по телефону в центре: существующий (existing=true) или новый.
// ON CONFLICT DO NOTHING — два одновременных создания с одним телефоном не падают.
func upsertParent(ctx context.Context, q *sqlc.Queries, centerID uuid.UUID, name, phone string) (sqlc.Parent, bool, error) {
	p, err := q.InsertParentIfAbsent(ctx, sqlc.InsertParentIfAbsentParams{CenterID: centerID, Name: name, Phone: phone})
	if err == nil {
		return p, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Parent{}, false, err
	}
	p, err = q.GetParentByPhone(ctx, sqlc.GetParentByPhoneParams{CenterID: centerID, Phone: phone})
	return p, true, err
}

func validateParent(name, phone string) (string, string, error) {
	name, err := validate.Name("parent.name", "Имя родителя", name)
	if err != nil {
		return "", "", err
	}
	norm, err := auth.NormalizePhone(phone)
	var verr *validate.Error
	if errors.As(err, &verr) {
		return "", "", validate.Errorf("parent.phone", "%s", verr.Message)
	}
	if err != nil {
		return "", "", err
	}
	return name, norm, nil
}

func validateBirthDate(d *civil.Date, today civil.Date) error {
	if d == nil {
		return nil
	}
	if d.Before(civil.New(1900, 1, 1)) || d.After(today) {
		return validate.Errorf("birth_date", "Дата рождения — не раньше 1900-01-01 и не позже сегодняшней")
	}
	return nil
}

// centerInfo — то, что нужно от настроек центра для дат и сроков (R2, R4).
type centerInfo struct {
	Today      civil.Date // «сегодня» в часовом поясе центра
	Timezone   string
	DueDays    int
	BillingDay int
}

func (s *Service) center(ctx context.Context, q *sqlc.Queries, centerID uuid.UUID) (centerInfo, error) {
	c, err := q.GetCenter(ctx, centerID)
	if err != nil {
		return centerInfo{}, err
	}
	today, err := civil.Today(s.now(), c.Timezone)
	if err != nil {
		return centerInfo{}, err
	}
	return centerInfo{Today: today, Timezone: c.Timezone, DueDays: int(c.DueDays),
		BillingDay: billing.EffectiveBillingDay(c.BillingDay, c.BillingDayNext, c.BillingDayNextFrom, today.FirstOfMonth())}, nil
}

func (s *Service) centerToday(ctx context.Context, q *sqlc.Queries, centerID uuid.UUID) (civil.Date, error) {
	c, err := s.center(ctx, q, centerID)
	return c.Today, err
}

func (s *Service) listEnrollments(ctx context.Context, q *sqlc.Queries, centerID, studentID uuid.UUID) ([]Enrollment, error) {
	rows, err := q.ListStudentEnrollments(ctx, sqlc.ListStudentEnrollmentsParams{CenterID: centerID, StudentID: studentID})
	if err != nil {
		return nil, err
	}
	out := make([]Enrollment, 0, len(rows))
	for _, r := range rows {
		out = append(out, Enrollment{
			ID: r.ID, GroupID: r.GroupID, GroupName: r.GroupName, TariffID: r.TariffID, TariffName: r.TariffName,
			TariffKind: r.TariffKind, TariffPrice: r.TariffPrice, StartDate: r.StartDate, EndDate: r.EndDate,
			LessonsLeft: r.LessonsLeft, BilledThrough: r.BilledThrough,
		})
	}
	return out, nil
}

func discountOf(kind *string, value *int64) *billing.Discount {
	if kind == nil || value == nil {
		return nil
	}
	return &billing.Discount{Kind: *kind, Value: *value}
}

func discountColumns(d *billing.Discount) (*string, *int64) {
	if d == nil {
		return nil, nil
	}
	return &d.Kind, &d.Value
}

func parentOf(id *uuid.UUID, name, phone, tg *string, lastReminder, lastWhatsApp *time.Time) *Parent {
	if id == nil {
		return nil
	}
	return &Parent{ID: *id, Name: deref(name), Phone: deref(phone), TgStatus: deref(tg), LastReminderAt: lastReminder, LastWhatsAppAt: lastWhatsApp}
}

func studentSnapshot(s sqlc.Student) map[string]any {
	return map[string]any{
		"full_name": s.FullName, "birth_date": s.BirthDate, "status": s.Status,
		"discount": discountOf(s.DiscountKind, s.DiscountValue), "parent_id": s.ParentID,
	}
}

func jsonEqual(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// escapeLike экранирует спецсимволы LIKE (\ % _): поиск «50%» ищет именно «50%».
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// phoneDigits — цифры запроса для поиска по телефону родителя (+996XXXXXXXXX):
// «0700 12» → «70012», «+996 700» → «996700».
func phoneDigits(q string) string {
	var b strings.Builder
	for _, r := range q {
		if unicode.IsDigit(r) && r < unicode.MaxASCII {
			b.WriteRune(r)
		}
	}
	return strings.TrimPrefix(b.String(), "0")
}

type cursor struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"i"`
}

func encodeCursor(c cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, error) {
	var c cursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, &c) != nil || c.ID == uuid.Nil {
		return cursor{}, validate.Errorf("cursor", "Некорректный cursor — возьмите next_cursor из прошлого ответа")
	}
	return c, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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
