package tariffs

import (
	"errors"
	"strings"
	"testing"

	"okuu/internal/validate"
)

func ptr[T any](v T) *T { return &v }

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		in        Input
		wantField string // "" — ошибки нет
	}{
		{"месячный", Input{Name: "Англ. месяц", Kind: KindMonthly, Price: 300_000}, ""},
		{"абонемент", Input{Name: "8 занятий", Kind: KindPackage, Price: 400_000, LessonsCount: ptr[int32](8)}, ""},
		{"бесплатный", Input{Name: "Пробный", Kind: KindMonthly, Price: 0}, ""},
		{"максимальная цена", Input{Name: "VIP", Kind: KindMonthly, Price: MaxPrice}, ""},
		{"абонемент 1 занятие", Input{Name: "Разовое", Kind: KindPackage, Price: 50_000, LessonsCount: ptr[int32](1)}, ""},
		{"абонемент 200 занятий", Input{Name: "Год", Kind: KindPackage, Price: 50_000, LessonsCount: ptr[int32](200)}, ""},

		{"package без lessons_count", Input{Name: "x", Kind: KindPackage, Price: 100}, "lessons_count"},
		{"package 0 занятий", Input{Name: "x", Kind: KindPackage, Price: 100, LessonsCount: ptr[int32](0)}, "lessons_count"},
		{"package 201 занятие", Input{Name: "x", Kind: KindPackage, Price: 100, LessonsCount: ptr[int32](201)}, "lessons_count"},
		{"monthly с lessons_count", Input{Name: "x", Kind: KindMonthly, Price: 100, LessonsCount: ptr[int32](8)}, "lessons_count"},
		{"отрицательная цена", Input{Name: "x", Kind: KindMonthly, Price: -1}, "price"},
		{"цена больше максимума", Input{Name: "x", Kind: KindMonthly, Price: MaxPrice + 1}, "price"},
		{"неизвестный kind", Input{Name: "x", Kind: "yearly", Price: 100}, "kind"},
		{"пустой kind", Input{Name: "x", Price: 100}, "kind"},
		{"пустое название", Input{Name: "  ", Kind: KindMonthly, Price: 100}, "name"},
		{"длинное название", Input{Name: strings.Repeat("я", 101), Kind: KindMonthly, Price: 100}, "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Validate(tt.in)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			var verr *validate.Error
			if !errors.As(err, &verr) || verr.Field != tt.wantField || verr.Unprocessable {
				t.Fatalf("err = %#v, want validate.Error(%s) 400", err, tt.wantField)
			}
		})
	}
}

func TestValidateTrimsName(t *testing.T) {
	out, err := Validate(Input{Name: "  Англ  ", Kind: KindMonthly, Price: 1})
	if err != nil || out.Name != "Англ" {
		t.Fatalf("Validate = %+v, %v", out, err)
	}
}
