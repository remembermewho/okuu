package notify

import (
	"net/url"
	"strings"
	"testing"

	"okuu/internal/civil"
)

func mustDate(t *testing.T, s string) *civil.Date {
	t.Helper()
	d, err := civil.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return &d
}

// Telegram и WhatsApp не расходятся: тот же состав; отличия — только *жирный* и последняя строка про бота.
func TestDebtReminderWhatsApp(t *testing.T) {
	today := *mustDate(t, "2026-11-09")
	two := []ChildDebt{
		{Name: "Азамат", Debt: 100_000, Due: mustDate(t, "2026-11-12")},
		{Name: "Гуля", Debt: 50_000, Due: mustDate(t, "2026-10-20"), DaysOverdue: 20},
	}
	one := two[:1]
	const pay = "MBank 0700 123 456\r\nЧек — администратору"

	for _, tc := range []struct {
		name       string
		children   []ChildDebt
		details    string
		wantLines  []string
		wantAbsent []string
	}{
		{
			name: "несколько детей: просроченный выше, итог", children: two, details: pay,
			wantLines: []string{
				"*🏫 Центр «Билим»: напоминание об оплате.*",
				"• Гуля — 500 сом, просрочено на 20 дней",
				"• Азамат — 1 000 сом, срок до 12.11",
				"*Итого: 1 500 сом*",
				"*Как оплатить:*",
				"MBank 0700 123 456",
				"Чек — администратору",
			},
		},
		{
			name: "один ребёнок — без итога", children: one, details: pay,
			wantLines:  []string{"• Азамат — 1 000 сом, срок до 12.11"},
			wantAbsent: []string{"Итого"},
		},
		{
			name: "пустые реквизиты", children: one, details: "  ",
			wantLines: []string{"*Как оплатить:*", "Уточните способ оплаты у администратора центра."},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wa := DebtReminderWhatsApp(ru, KindManual, "Билим", today, tc.children, tc.details)
			tg := DebtReminder(ru, KindManual, "Билим", today, tc.children, tc.details)

			got := strings.Split(wa, "\n")
			for _, want := range tc.wantLines {
				if !contains(got, want) {
					t.Errorf("нет строки %q:\n%s", want, wa)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(wa, absent) {
					t.Errorf("лишнее %q:\n%s", absent, wa)
				}
			}
			if strings.ContainsAny(wa, "<>") {
				t.Errorf("HTML в тексте WhatsApp:\n%s", wa)
			}
			if strings.Contains(wa, "бот") {
				t.Errorf("WhatsApp-текст обещает бота:\n%s", wa)
			}

			// Без звёздочек и последней строки — ровно Telegram.
			norm := func(s string) string {
				lines := strings.Split(strings.ReplaceAll(s, "*", ""), "\n")
				return strings.Join(lines[:len(lines)-1], "\n")
			}
			if norm(wa) != norm(tg) {
				t.Errorf("тексты разошлись:\nWhatsApp:\n%s\nTelegram:\n%s", wa, tg)
			}
		})
	}
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

// Звёздочка в названии центра не должна ломать выделение.
func TestWhatsAppBoldEscapesAsterisk(t *testing.T) {
	got := BroadcastWhatsApp(ru, "Звезда*Плюс", "Текст *как есть*")
	want := "*🏫 Центр «ЗвездаПлюс»*\n\nТекст *как есть*"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBroadcastStartsWithCenter(t *testing.T) {
	if got, want := Broadcast(ru, "Билим", "Завтра занятий нет"), "🏫 Центр «Билим»\n\nЗавтра занятий нет"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := Broadcast(ky, "Билим", "Эртең сабак жок"); !strings.HasPrefix(got, ky.BalanceCenter("Билим")) {
		t.Errorf("ky: %q", got)
	}
}

// В ссылке пробелы — %20 (WhatsApp показал бы «+»), кириллица, переносы и звёздочки доходят как есть.
func TestWhatsAppURL(t *testing.T) {
	text := "*Итого: 1 500 сом*\nA+B & c?"
	raw := WhatsAppURL("996700123456", text)
	if !strings.HasPrefix(raw, "https://wa.me/996700123456?text=") {
		t.Fatalf("адрес: %s", raw)
	}
	if strings.Contains(raw, "+") {
		t.Errorf("плюс в ссылке (пробел или неэкранированный): %s", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("text"); got != text {
		t.Errorf("текст после разбора: %q, want %q", got, text)
	}
}
