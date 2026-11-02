package notify

import (
	"strings"
	"testing"
)

func TestParseLangs(t *testing.T) {
	tests := []struct {
		in      string
		multi   bool
		wantErr bool
	}{
		{"", false, false},
		{"ru", false, false},
		{" RU ", false, false},
		{"ru,ky", true, false},
		{"ky, ru", true, false},
		{"ky", false, true}, // русский обязателен
		{"ru,en", false, true},
	}
	for _, tt := range tests {
		l, err := ParseLangs(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseLangs(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if err == nil && l.Multi() != tt.multi {
			t.Errorf("ParseLangs(%q).Multi() = %v, want %v", tt.in, l.Multi(), tt.multi)
		}
	}
}

// Только русский (по умолчанию): кыргызский выбор родителя не действует, в подсказках нет /lang.
// С кыргызским — как раньше.
func TestLangsFor(t *testing.T) {
	var ruOnlyL Langs // нулевое значение — только ru
	if ruOnlyL.Resolve(LangKY) != LangRU || ruOnlyL.For(LangKY).LangChanged != ru.LangChanged {
		t.Error("только ru: родителю с lang=ky должно уйти по-русски")
	}
	for name, s := range map[string]string{
		"Help":    ruOnlyL.For(LangRU).Help,
		"Linked":  ruOnlyL.For(LangRU).Linked("Билим", []string{"Бакыт"}),
		"Welcome": ruOnlyL.For(LangRU).Welcome([]string{"Билим"}),
	} {
		if strings.Contains(s, "/lang") {
			t.Errorf("только ru: %s упоминает /lang: %s", name, s)
		}
		// Оплаты в боте нет: вместо /pay — кнопка «Как оплатить».
		if strings.Contains(s, "/pay") || !strings.Contains(s, "Как оплатить") || !strings.Contains(s, "/attendance") {
			t.Errorf("только ru: %s: нужны «Как оплатить» и /attendance, без /pay: %s", name, s)
		}
	}
	both := MustLangs("ru,ky")
	if both.Resolve(LangKY) != LangKY || both.For(LangKY) != ky || both.For(LangRU) != ru {
		t.Error("ru,ky: выбор родителя должен действовать")
	}
	if !strings.Contains(both.For(LangRU).Help, "/lang") || !strings.Contains(both.For(LangKY).Help, "/lang") {
		t.Error("ru,ky: в помощи есть /lang")
	}
}
