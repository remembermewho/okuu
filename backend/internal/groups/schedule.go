package groups

import (
	"cmp"
	"fmt"
	"slices"

	"okuu/internal/validate"
)

// MaxSlots — не больше 14 занятий в неделю у группы.
const MaxSlots = 14

// Slot — регулярное занятие: день недели (1 = пн … 7 = вс) и время "HH:MM" в часовом поясе центра.
type Slot struct {
	Dow   int    `json:"dow"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// ValidateSchedule: dow 1–7, время "HH:MM", end > start, не больше MaxSlots,
// занятия одного дня не пересекаются (стык 17:00–17:00 допустим).
func ValidateSchedule(slots []Slot) error {
	if len(slots) > MaxSlots {
		return validate.Errorf("schedule", "Не больше %d занятий в неделю", MaxSlots)
	}
	type interval struct{ i, start, end int }
	byDay := map[int][]interval{}
	for i, s := range slots {
		if s.Dow < 1 || s.Dow > 7 {
			return validate.Errorf("schedule", "Занятие %d: день недели от 1 (пн) до 7 (вс)", i+1)
		}
		start, ok := parseHHMM(s.Start)
		if !ok {
			return validate.Errorf("schedule", "Занятие %d: начало в формате ЧЧ:ММ", i+1)
		}
		end, ok := parseHHMM(s.End)
		if !ok {
			return validate.Errorf("schedule", "Занятие %d: конец в формате ЧЧ:ММ", i+1)
		}
		if end <= start {
			return validate.Errorf("schedule", "Занятие %d: конец должен быть позже начала", i+1)
		}
		byDay[s.Dow] = append(byDay[s.Dow], interval{i, start, end})
	}
	for _, day := range byDay {
		slices.SortFunc(day, func(a, b interval) int { return cmp.Compare(a.start, b.start) })
		for k := 1; k < len(day); k++ {
			if day[k].start < day[k-1].end {
				a, b := min(day[k-1].i, day[k].i), max(day[k-1].i, day[k].i)
				return validate.Errorf("schedule", "Занятия %d и %d пересекаются по времени", a+1, b+1)
			}
		}
	}
	return nil
}

// parseHHMM разбирает строго "HH:MM" (00:00–23:59) в минуты от полуночи.
func parseHHMM(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	var h, m int
	if _, err := fmt.Sscanf(s, "%02d:%02d", &h, &m); err != nil {
		return 0, false
	}
	for _, c := range []byte{s[0], s[1], s[3], s[4]} {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}
