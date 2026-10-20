package notify

import (
	"reflect"
	"strings"
	"testing"
)

func TestSom(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0"}, {49, "0"}, {50, "1"}, {100, "1"}, {150_000, "1 500"}, {150_050, "1 501"}, {150_049, "1 500"},
		{100_000_000, "1 000 000"}, {-150_000, "-1 500"}, {-49, "0"}, {99_999, "1 000"}, {12_345_678, "123 457"},
	}
	for _, tt := range tests {
		if got := Som(tt.in); got != tt.want {
			t.Errorf("Som(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPluralRU(t *testing.T) {
	for n, want := range map[int64]string{1: "занятие", 2: "занятия", 4: "занятия", 5: "занятий", 11: "занятий",
		12: "занятий", 14: "занятий", 21: "занятие", 22: "занятия", 25: "занятий", 101: "занятие", 111: "занятий"} {
		if got := pluralRU(n, "занятие", "занятия", "занятий"); got != want {
			t.Errorf("%d → %q, want %q", n, got, want)
		}
	}
}

// В обоих языках заполнены все тексты; в сообщениях о деньгах и привязке есть название центра.
func TestTextsComplete(t *testing.T) {
	for _, lang := range []string{LangRU, LangKY} {
		tx := For(lang)
		v := reflect.ValueOf(tx).Elem()
		for i := range v.NumField() {
			f := v.Field(i)
			if (f.Kind() == reflect.String && f.String() == "") || (f.Kind() == reflect.Func && f.IsNil()) {
				t.Errorf("%s: поле %s не заполнено", lang, v.Type().Field(i).Name)
			}
		}
		const center = "Билим Плюс"
		for name, s := range map[string]string{
			"Invite":        tx.Invite("Айгуль", center, "https://t.me/okuu_bot?start=x"),
			"ConsentPrompt": tx.ConsentPrompt(center, []string{"Бакыт"}),
			"PhoneMismatch": tx.PhoneMismatch(center),
			"Linked":        tx.Linked(center, []string{"Бакыт"}),
			"Stopped":       tx.Stopped([]string{center}),
			"BalanceCenter": tx.BalanceCenter(center),
		} {
			if !strings.Contains(s, center) {
				t.Errorf("%s.%s без названия центра: %s", lang, name, s)
			}
		}
		if !strings.Contains(tx.Invite("Айгуль", center, "https://t.me/okuu_bot?start=x"), "https://t.me/okuu_bot?start=x") {
			t.Errorf("%s: в приглашении нет ссылки", lang)
		}
		if s := tx.Balance(-150_000); !strings.Contains(s, "1 500 сом") {
			t.Errorf("%s: долг = %q", lang, s)
		}
		if s := tx.Attendance(10, 6, 1, 0); !strings.Contains(s, "6") {
			t.Errorf("%s: посещения = %q", lang, s)
		}
	}
	if For("en") != For(LangRU) {
		t.Error("неизвестный язык — русский")
	}
	if got := ru.Package("Робототехника", 1); got != "Абонемент (Робототехника): осталось 1 занятие" {
		t.Errorf("абонемент = %q", got)
	}
	if got := ru.Attendance(10, 6, 1, 0); got != "Посещения в октябре: был 6, не был 1, по уважительной 0" {
		t.Errorf("посещения = %q", got)
	}
}
