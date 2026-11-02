package notify

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"okuu/internal/civil"
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

// previewDue — срок в примере предпросмотра (settings.preview.child): 05.11.
var previewDue = civil.New(2026, 11, 5)

// Предпросмотр напоминания в «Настройках» (frontend/src/i18n/ru.ts, settings.preview) повторяет тексты бота —
// дословно, иначе владелец увидит не то, что получит родитель.
func TestFrontendPreviewMatchesBot(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "frontend", "src", "i18n", "ru.ts"))
	if err != nil {
		t.Skipf("нет фронтенда рядом: %v", err)
	}
	for name, s := range map[string]string{
		"ReminderFooter":    ru.ReminderFooter,
		"HowToPayHeader":    ru.HowToPayHeader,
		"PayDetailsMissing": ru.PayDetailsMissing,
		"BalanceButton":     ru.BalanceButton,
		"ReminderHeader":    strings.ReplaceAll(ru.ReminderHeader(KindManual, "X", civil.Date{}), "«X»", "«${center}»"),
		"ReminderChild":     ru.ReminderChild(ChildDebt{Name: "Асанов Бакыт", Debt: 350_000, Due: &previewDue}),
	} {
		if !strings.Contains(string(src), "'"+s+"'") && !strings.Contains(string(src), "`"+s+"`") {
			t.Errorf("%s бота нет в settings.preview (ru.ts): %q", name, s)
		}
	}
}

// Напоминание: сначала просроченные (дольше — выше), затем ожидающие по сроку; «срок сегодня», «срок до».
func TestDebtReminderOrder(t *testing.T) {
	d := func(s string) *civil.Date {
		v, err := civil.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return &v
	}
	text := DebtReminder(ru, KindManual, "Билим", *d("2026-11-02"), []ChildDebt{
		{Name: "Азамат", Debt: 100_000, Due: d("2026-11-05")},
		{Name: "Бакыт", Debt: 200_000, Due: d("2026-11-02"), DueToday: true},
		{Name: "Вика", Debt: 150_000, Due: d("2026-11-01"), DaysOverdue: 1},
		{Name: "Гуля", Debt: 50_000, Due: d("2026-10-20"), DaysOverdue: 13},
	}, "")
	want := "• Гуля — 500 сом, просрочено на 13 дней\n• Вика — 1 500 сом, просрочено на 1 день\n" +
		"• Бакыт — 2 000 сом, срок сегодня\n• Азамат — 1 000 сом, срок до 05.11\nИтого: 5 000 сом"
	if !strings.Contains(text, want) {
		t.Errorf("напоминание:\n%s", text)
	}
}

// Имя ребёнка в сообщениях бота не склоняется: всегда как в карточке, через тире.
func TestChildNameNominative(t *testing.T) {
	const name = "Айдана Токтосунова"
	date := civil.New(2026, 11, 2)
	for _, tt := range []struct{ msg, got, want string }{
		{"Paid", ru.Paid("Билим", name, 150_000, 0, nil),
			"🏫 Центр «Билим»\nОплата получена: 1 500 сом — Айдана Токтосунова. Спасибо!\nБаланс: 0 сом, долгов нет"},
		{"Paid ky", ky.Paid("Билим", name, 150_000, 0, nil),
			"🏫 «Билим» борбору\nТөлөм алынды: 1 500 сом — Айдана Токтосунова. Рахмат!\nБаланс: 0 сом, карыз жок"},
		{"PackageLow", ru.PackageLow("Билим", name, "English"),
			"🏫 Центр «Билим»: Айдана Токтосунова — в абонементе (English) осталось 1 занятие. Пора продлить абонемент."},
		{"Absent", ru.Absent("Билим", name, "English", date, "15:00"),
			"🏫 Центр «Билим»: Айдана Токтосунова — не был(а) на занятии «English» 02.11 в 15:00."},
		{"Absent ky", ky.Absent("Билим", name, "English", date, "15:00"),
			"🏫 «Билим» борбору: Айдана Токтосунова — 02.11 саат 15:00 «English» сабагына келген жок."},
	} {
		if tt.got != tt.want {
			t.Errorf("%s:\n got %q\nwant %q", tt.msg, tt.got, tt.want)
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
	// «Как оплатить»: реквизиты как есть (с переносами), пустые — «уточните у администратора».
	if got := HowToPay(ru, " MBank: 0700\r\nЧек — в WhatsApp \n"); got != "Как оплатить:\nMBank: 0700\nЧек — в WhatsApp" {
		t.Errorf("HowToPay = %q", got)
	}
	if got := HowToPay(ru, "  "); got != "Как оплатить:\nУточните способ оплаты у администратора центра." {
		t.Errorf("HowToPay без реквизитов = %q", got)
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
