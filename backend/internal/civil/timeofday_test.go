package civil

import (
	"encoding/json"
	"testing"
)

func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		in   string
		want string // "" — ошибка
	}{
		{"00:00", "00:00"}, {"09:05", "09:05"}, {"16:30", "16:30"}, {"23:59", "23:59"},
		{"24:00", ""}, {"12:60", ""}, {"9:00", ""}, {"09:00:00", ""}, {"0900", ""}, {"ab:cd", ""}, {"", ""}, {"-1:00", ""},
	}
	for _, tt := range tests {
		got, err := ParseTimeOfDay(tt.in)
		if tt.want == "" {
			if err == nil {
				t.Errorf("ParseTimeOfDay(%q) = %v, want ошибку", tt.in, got)
			}
			continue
		}
		if err != nil || got.String() != tt.want {
			t.Errorf("ParseTimeOfDay(%q) = %v, %v; want %s", tt.in, got, err, tt.want)
		}
	}

	var v struct {
		T TimeOfDay `json:"t"`
	}
	if err := json.Unmarshal([]byte(`{"t":"16:00"}`), &v); err != nil || v.T.Hour() != 16 || v.T.Minute() != 0 {
		t.Fatalf("unmarshal: %v %v", v, err)
	}
	if b, _ := json.Marshal(v); string(b) != `{"t":"16:00"}` {
		t.Errorf("marshal = %s", b)
	}
	if err := json.Unmarshal([]byte(`{"t":"4pm"}`), &v); err == nil {
		t.Error("ожидалась ошибка")
	}
	a, _ := ParseTimeOfDay("09:00")
	b, _ := ParseTimeOfDay("10:30")
	if a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
		t.Error("Compare")
	}
}

func TestISOWeekday(t *testing.T) {
	// 2026-10-19 — понедельник.
	for i, want := range []int{1, 2, 3, 4, 5, 6, 7, 1} {
		if got := New(2026, 10, 19).AddDays(i).ISOWeekday(); got != want {
			t.Errorf("2026-10-%d: %d, want %d", 19+i, got, want)
		}
	}
}
