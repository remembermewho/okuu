package civil

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// TimeOfDay — время суток с точностью до минуты (колонки time, например lessons.start_time).
// В API — строго "HH:MM" (00:00–23:59).
type TimeOfDay struct {
	minutes int // от полуночи, 0–1439
}

// NewTimeOfDay — время hh:mm; вне диапазона — ошибка.
func NewTimeOfDay(h, m int) (TimeOfDay, error) {
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return TimeOfDay{}, fmt.Errorf("время %02d:%02d вне 00:00–23:59", h, m)
	}
	return TimeOfDay{h*60 + m}, nil
}

// ParseTimeOfDay разбирает строго "HH:MM".
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	bad := fmt.Errorf("время %q: ожидается ЧЧ:ММ", s)
	if len(s) != 5 || s[2] != ':' {
		return TimeOfDay{}, bad
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return TimeOfDay{}, bad
		}
	}
	t, err := NewTimeOfDay(int(s[0]-'0')*10+int(s[1]-'0'), int(s[3]-'0')*10+int(s[4]-'0'))
	if err != nil {
		return TimeOfDay{}, bad
	}
	return t, nil
}

func (t TimeOfDay) Hour() int      { return t.minutes / 60 }
func (t TimeOfDay) Minute() int    { return t.minutes % 60 }
func (t TimeOfDay) String() string { return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute()) }

// Compare: -1, 0, +1.
func (t TimeOfDay) Compare(o TimeOfDay) int {
	switch {
	case t.minutes < o.minutes:
		return -1
	case t.minutes > o.minutes:
		return 1
	}
	return 0
}

func (t TimeOfDay) MarshalJSON() ([]byte, error) { return json.Marshal(t.String()) }

func (t *TimeOfDay) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("время: ожидается строка ЧЧ:ММ")
	}
	v, err := ParseTimeOfDay(s)
	if err != nil {
		return err
	}
	*t = v
	return nil
}

// ScanTime реализует pgtype.TimeScanner; секунды отбрасываются (в БД пишем только минуты).
func (t *TimeOfDay) ScanTime(v pgtype.Time) error {
	if !v.Valid {
		return errors.New("civil.TimeOfDay: NULL в непустое время")
	}
	t.minutes = int(v.Microseconds / 60_000_000)
	return nil
}

// TimeValue реализует pgtype.TimeValuer.
func (t TimeOfDay) TimeValue() (pgtype.Time, error) {
	return pgtype.Time{Microseconds: int64(t.minutes) * 60_000_000, Valid: true}, nil
}

// ISOWeekday — день недели даты: 1 = понедельник … 7 = воскресенье (как dow в расписании групп).
func (d Date) ISOWeekday() int {
	w := int(d.time().Weekday())
	if w == 0 {
		return 7
	}
	return w
}
