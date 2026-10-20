package billing

import (
	"errors"
	"testing"

	"okuu/internal/validate"
)

// R6: скидка применяется при создании начисления.
func TestDiscountAmount(t *testing.T) {
	tests := []struct {
		name    string
		price   int64
		d       *Discount
		want    int64
		want422 bool
		wantSum int64 // amount = -(price - discount)
	}{
		{"без скидки", 300_000, nil, 0, false, -300_000},
		{"fixed", 300_000, &Discount{DiscountFixed, 50_000}, 50_000, false, -250_000},
		{"fixed = цене", 300_000, &Discount{DiscountFixed, 300_000}, 300_000, false, 0},
		{"fixed больше цены", 300_000, &Discount{DiscountFixed, 300_001}, 0, true, 0},
		{"fixed 0", 300_000, &Discount{DiscountFixed, 0}, 0, false, -300_000},
		{"percent 10", 300_000, &Discount{DiscountPercent, 10}, 30_000, false, -270_000},
		{"percent с округлением вниз", 333_333, &Discount{DiscountPercent, 15}, 49_999, false, -283_334}, // 49 999.95
		{"percent 33 от 100", 100, &Discount{DiscountPercent, 33}, 33, false, -67},
		{"percent 1 от 99 тыйын", 99, &Discount{DiscountPercent, 1}, 0, false, -99}, // 0.99 → 0
		{"percent 100", 300_000, &Discount{DiscountPercent, 100}, 300_000, false, 0},
		{"percent 0", 300_000, &Discount{DiscountPercent, 0}, 0, false, -300_000},
		{"максимальная цена, 99%", 1_000_000_000, &Discount{DiscountPercent, 99}, 990_000_000, false, -10_000_000},
		{"бесплатный тариф", 0, &Discount{DiscountPercent, 50}, 0, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DiscountAmount(tt.price, tt.d)
			if tt.want422 {
				var verr *validate.Error
				if !errors.As(err, &verr) || verr.Field != "discount" || !verr.Unprocessable {
					t.Fatalf("err = %v, want 422 discount", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("DiscountAmount = %d, %v; want %d", got, err, tt.want)
			}
			if sum := -(tt.price - got); sum != tt.wantSum {
				t.Errorf("amount = %d, want %d", sum, tt.wantSum)
			}
		})
	}
}

func TestValidateDiscount(t *testing.T) {
	const maxFixed = 1_000_000_000
	tests := []struct {
		name string
		d    *Discount
		ok   bool
	}{
		{"нет", nil, true},
		{"fixed", &Discount{DiscountFixed, 50_000}, true},
		{"fixed максимум", &Discount{DiscountFixed, maxFixed}, true},
		{"fixed отрицательная", &Discount{DiscountFixed, -1}, false},
		{"fixed больше максимума", &Discount{DiscountFixed, maxFixed + 1}, false},
		{"percent 100", &Discount{DiscountPercent, 100}, true},
		{"percent 101", &Discount{DiscountPercent, 101}, false},
		{"percent -1", &Discount{DiscountPercent, -1}, false},
		{"неизвестный вид", &Discount{"brother", 10}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDiscount(tt.d, maxFixed)
			var verr *validate.Error
			if tt.ok != (err == nil) || (err != nil && (!errors.As(err, &verr) || verr.Field != "discount")) {
				t.Fatalf("err = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}
