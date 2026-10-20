package groups

import (
	"errors"
	"strings"
	"testing"

	"okuu/internal/validate"
)

func TestValidateSchedule(t *testing.T) {
	slot := func(dow int, start, end string) Slot { return Slot{Dow: dow, Start: start, End: end} }
	many := func(n int) []Slot {
		var s []Slot
		for i := range n {
			s = append(s, slot(i%7+1, []string{"08:00", "10:00", "12:00"}[i/7], []string{"09:00", "11:00", "13:00"}[i/7]))
		}
		return s
	}

	tests := []struct {
		name    string
		in      []Slot
		wantErr string // подстрока сообщения; "" — ошибки нет
	}{
		{"пусто", nil, ""},
		{"пн и ср", []Slot{slot(1, "16:00", "17:30"), slot(3, "16:00", "17:30")}, ""},
		{"два занятия в день без пересечения", []Slot{slot(2, "09:00", "10:00"), slot(2, "11:00", "12:00")}, ""},
		{"стык 17:00", []Slot{slot(5, "16:00", "17:00"), slot(5, "17:00", "18:00")}, ""},
		{"в обратном порядке", []Slot{slot(5, "17:00", "18:00"), slot(5, "16:00", "17:00")}, ""},
		{"вс и полночь", []Slot{slot(7, "00:00", "23:59")}, ""},
		{"ровно 14", many(14), ""},

		{"15 занятий", many(15), "Не больше 14"},
		{"пересечение", []Slot{slot(1, "16:00", "17:30"), slot(1, "17:00", "18:00")}, "Занятия 1 и 2 пересекаются"},
		{"пересечение через одно", []Slot{slot(1, "10:00", "12:00"), slot(2, "10:00", "11:00"), slot(1, "11:59", "13:00")}, "Занятия 1 и 3 пересекаются"},
		{"вложенное", []Slot{slot(4, "10:00", "14:00"), slot(4, "11:00", "12:00")}, "пересекаются"},
		{"одинаковые", []Slot{slot(6, "10:00", "11:00"), slot(6, "10:00", "11:00")}, "пересекаются"},
		{"end = start", []Slot{slot(1, "16:00", "16:00")}, "конец должен быть позже"},
		{"end < start", []Slot{slot(1, "17:00", "16:00")}, "конец должен быть позже"},
		{"dow 0", []Slot{slot(0, "10:00", "11:00")}, "день недели"},
		{"dow 8", []Slot{slot(8, "10:00", "11:00")}, "день недели"},
		{"час 24", []Slot{slot(1, "24:00", "24:30")}, "начало"},
		{"минуты 60", []Slot{slot(1, "10:60", "11:00")}, "начало"},
		{"без ведущего нуля", []Slot{slot(1, "9:00", "10:00")}, "начало"},
		{"секунды", []Slot{slot(1, "09:00", "10:00:00")}, "конец"},
		{"знак", []Slot{slot(1, "+9:00", "10:00")}, "начало"},
		{"пустое время", []Slot{slot(1, "", "10:00")}, "начало"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSchedule(tt.in)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			var verr *validate.Error
			if !errors.As(err, &verr) || verr.Field != "schedule" || !strings.Contains(verr.Message, tt.wantErr) {
				t.Fatalf("err = %v, want schedule: …%s…", err, tt.wantErr)
			}
		})
	}
}
