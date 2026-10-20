package civil

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want Date
		ok   bool
	}{
		{"2026-10-20", New(2026, 10, 20), true},
		{"2024-02-29", New(2024, 2, 29), true},
		{"2026-02-29", Date{}, false}, // не високосный
		{"2026-13-01", Date{}, false},
		{"2026-1-05", Date{}, false},
		{"2026-01-5", Date{}, false},
		{"20-01-2026", Date{}, false},
		{"2026/01/05", Date{}, false},
		{"2026-01-05T00:00:00Z", Date{}, false},
		{" 2026-01-05", Date{}, false},
		{"", Date{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Parse(tt.in)
			if (err == nil) != tt.ok || got != tt.want {
				t.Fatalf("Parse(%q) = %v, %v; want %v ok=%v", tt.in, got, err, tt.want, tt.ok)
			}
		})
	}
}

func TestJSON(t *testing.T) {
	type v struct {
		D  Date  `json:"d"`
		P  *Date `json:"p"`
		P2 *Date `json:"p2"`
	}
	d := New(2026, 10, 1)
	b, _ := json.Marshal(v{D: d, P: &d})
	if string(b) != `{"d":"2026-10-01","p":"2026-10-01","p2":null}` {
		t.Fatalf("marshal = %s", b)
	}
	var got v
	if err := json.Unmarshal([]byte(`{"d":"2026-12-31","p":null}`), &got); err != nil || got.D != New(2026, 12, 31) || got.P != nil {
		t.Fatalf("unmarshal = %+v, %v", got, err)
	}
	for _, bad := range []string{`{"d":"31.12.2026"}`, `{"d":20261231}`, `{"d":"2026-12-31T10:00:00Z"}`} {
		if err := json.Unmarshal([]byte(bad), &got); err == nil {
			t.Errorf("unmarshal %s: ожидалась ошибка", bad)
		}
	}
}

func TestArithmetic(t *testing.T) {
	tests := []struct {
		name      string
		got, want Date
	}{
		{"+5 дней через месяц", New(2026, 10, 29).AddDays(5), New(2026, 11, 3)},
		{"+1 через год", New(2026, 12, 31).AddDays(1), New(2027, 1, 1)},
		{"-1 в високосный", New(2024, 3, 1).AddDays(-1), New(2024, 2, 29)},
		{"1-е число", New(2026, 10, 20).FirstOfMonth(), New(2026, 10, 1)},
		{"следующий месяц", New(2026, 12, 1).AddMonths(1), New(2027, 1, 1)},
		{"нормализация New", New(2026, 1, 32), New(2026, 2, 1)},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, tt.got, tt.want)
		}
	}
	a, b := New(2026, 10, 1), New(2026, 10, 31)
	if !a.Before(b) || !b.After(a) || a.Compare(a) != 0 || !a.SameMonth(b) || a.SameMonth(New(2025, 10, 1)) {
		t.Error("сравнение дат")
	}
}

func TestTodayInCenterTimezone(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		tz   string
		want Date
	}{
		// Бишкек UTC+6: 20:00 UTC 31 октября — уже 02:00 1 ноября.
		{"полночь в Бишкеке раньше UTC", time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC), "Asia/Bishkek", New(2026, 11, 1)},
		{"днём совпадает", time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC), "Asia/Bishkek", New(2026, 10, 31)},
		{"UTC", time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC), "UTC", New(2026, 10, 31)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Today(tt.now, tt.tz)
			if err != nil || got != tt.want {
				t.Fatalf("Today = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
	if _, err := Today(time.Now(), "Mars/Olympus"); err == nil {
		t.Error("неизвестный пояс: ожидалась ошибка")
	}
}
