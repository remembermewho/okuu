// Package civil — календарная дата без времени и часового пояса (колонки date).
// В API — строго "YYYY-MM-DD", в БД — date. «Сегодня» считается в часовом поясе центра.
package civil

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	_ "time/tzdata" // часовые пояса внутри бинарника: в контейнере может не быть /usr/share/zoneinfo

	"github.com/jackc/pgx/v5/pgtype"
)

const layout = "2006-01-02"

// Date — календарная дата. Нулевое значение (IsZero) — «дата не задана».
type Date struct {
	year  int
	month time.Month
	day   int
}

// New создаёт дату; переполнение нормализуется как в time.Date (32 января → 1 февраля).
func New(year int, month time.Month, day int) Date {
	return DateOf(time.Date(year, month, day, 0, 0, 0, 0, time.UTC))
}

// DateOf — дата момента t в его часовом поясе.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

// Parse разбирает строго "YYYY-MM-DD".
func Parse(s string) (Date, error) {
	if len(s) != len(layout) {
		return Date{}, fmt.Errorf("дата %q: ожидается ГГГГ-ММ-ДД", s)
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return Date{}, fmt.Errorf("дата %q: ожидается ГГГГ-ММ-ДД", s)
	}
	return DateOf(t), nil
}

func (d Date) Year() int          { return d.year }
func (d Date) Month() time.Month  { return d.month }
func (d Date) Day() int           { return d.day }
func (d Date) IsZero() bool       { return d == Date{} }
func (d Date) String() string     { return d.time().Format(layout) }
func (d Date) time() time.Time    { return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC) }
func (d Date) AddDays(n int) Date { return DateOf(d.time().AddDate(0, 0, n)) }

// FirstOfMonth — 1-е число месяца даты (period начислений).
func (d Date) FirstOfMonth() Date { return Date{d.year, d.month, 1} }

// AddMonths сдвигает на n месяцев с нормализацией (31 января + 1 месяц → 3 марта).
func (d Date) AddMonths(n int) Date { return DateOf(d.time().AddDate(0, n, 0)) }

// SameMonth — тот же календарный месяц.
func (d Date) SameMonth(o Date) bool { return d.year == o.year && d.month == o.month }

// Compare: -1, если d раньше o; 0, если равны; +1, если позже.
func (d Date) Compare(o Date) int { return d.time().Compare(o.time()) }
func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }
func (d Date) After(o Date) bool  { return d.Compare(o) > 0 }

// --- JSON и текст (query-параметры) ---

func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(d.String())
}

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("дата: ожидается строка ГГГГ-ММ-ДД")
	}
	v, err := Parse(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

func (d *Date) UnmarshalText(b []byte) error {
	v, err := Parse(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// --- pgx: колонка date ---

// ScanDate реализует pgtype.DateScanner. NULL в не-указатель — ошибка (для NULL — *Date).
func (d *Date) ScanDate(v pgtype.Date) error {
	if !v.Valid {
		return errors.New("civil.Date: NULL в непустую дату — используйте *civil.Date")
	}
	if v.InfinityModifier != pgtype.Finite {
		return errors.New("civil.Date: бесконечная дата не поддерживается")
	}
	*d = DateOf(v.Time)
	return nil
}

// DateValue реализует pgtype.DateValuer. Нулевая дата — ошибка: это почти наверняка забытое поле.
func (d Date) DateValue() (pgtype.Date, error) {
	if d.IsZero() {
		return pgtype.Date{}, errors.New("civil.Date: запись нулевой даты")
	}
	return pgtype.Date{Time: d.time(), Valid: true}, nil
}

// --- «Сегодня» в часовом поясе центра ---

var locations sync.Map // tz → *time.Location

// Today — дата момента now в часовом поясе tz (например, centers.timezone = "Asia/Bishkek").
func Today(now time.Time, tz string) (Date, error) {
	loc, err := location(tz)
	if err != nil {
		return Date{}, err
	}
	return DateOf(now.In(loc)), nil
}

func location(tz string) (*time.Location, error) {
	if l, ok := locations.Load(tz); ok {
		return l.(*time.Location), nil
	}
	l, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("часовой пояс %q: %w", tz, err)
	}
	locations.Store(tz, l)
	return l, nil
}
