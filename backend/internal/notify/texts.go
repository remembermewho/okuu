// Package notify — тексты для родителей на русском и кыргызском (бот, приглашения, позже —
// напоминания R17–R19). В каждом сообщении с деньгами — название центра: один родитель может
// быть в нескольких центрах.
package notify

import (
	"strconv"
	"strings"
)

// Языки родителя (parents.lang).
const (
	LangRU = "ru"
	LangKY = "ky"
)

// Texts — все тексты одного языка. Функции принимают уже подготовленные значения
// (имена, суммы строкой через Som).
type Texts struct {
	// Приглашение (WhatsApp / SMS от администратора).
	Invite func(parent, center, botURL string) string

	// Привязка.
	InviteInvalid        string
	ConsentPrompt        func(center string, children []string) string
	ConsentButton        string
	ConsentAccepted      string
	ContactPrompt        string
	ContactButton        string
	ContactNotOwn        string
	ContactBeforeConsent string
	ContactNoInvite      string
	PhoneMismatch        func(center string) string
	Linked               func(center string, children []string) string

	// Команды.
	StartHint     string
	Welcome       func(centers []string) string
	Resumed       func(centers []string) string
	Help          string
	Stopped       func(centers []string) string
	NotLinked     string
	LangChanged   string
	Unknown       string
	BalanceButton string

	// /balance.
	BalanceCenter func(center string) string
	Balance       func(tyiyn int64) string
	Package       func(group string, lessonsLeft int32) string
	Attendance    func(month int, present, absent, excused int32) string
	NoChildren    string
}

// LangPrompt — выбор языка: на обоих языках, пока язык не выбран.
const LangPrompt = "Выберите язык / Тилди тандаңыз:"

// LangButtons — подписи кнопок выбора языка.
var LangButtons = map[string]string{LangRU: "Русский", LangKY: "Кыргызча"}

// For — тексты языка; неизвестный язык — русский.
func For(lang string) *Texts {
	if lang == LangKY {
		return ky
	}
	return ru
}

// ValidLang — язык поддерживается.
func ValidLang(lang string) bool { return lang == LangRU || lang == LangKY }

// Som — сумма в сомах для текста: тыйыны округляются до сома (R1), тысячи через пробел:
// 150_050 → «1 501», -150_000 → «-1 500».
func Som(tyiyn int64) string {
	neg := tyiyn < 0
	if neg {
		tyiyn = -tyiyn
	}
	s := strconv.FormatInt((tyiyn+50)/100, 10)
	var b strings.Builder
	if neg && s != "0" {
		b.WriteByte('-')
	}
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// quoted — «Центр», «Центр 2».
func quoted(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "«" + n + "»"
	}
	return strings.Join(out, ", ")
}

// pluralRU — форма слова для числа: 1 занятие, 2 занятия, 5 занятий.
func pluralRU(n int64, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	switch {
	case n%10 == 1 && n%100 != 11:
		return one
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return few
	}
	return many
}
