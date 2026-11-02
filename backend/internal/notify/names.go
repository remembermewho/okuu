package notify

import (
	"strings"
	"unicode"
)

// AccusativeRU — «за кого»: полное имя в винительном падеже, «Айдана Токтосунова» → «Айдану Токтосунову»,
// «Бекжан Асанов» → «Бекжана Асанова», «Бекжан Асан уулу» → «Бекжана Асан уулу». Пол — по фамилии
// или отчеству (-ова, -ина, -овна, кызы / -ов, -ин, -ович, уулу), а если их нет — по окончанию имени.
// ok=false — уверенности нет (пол не понять, латиница, дефис): тогда текст обходится без склонения,
// чтобы родитель не увидел имя ребёнка с ошибкой.
func AccusativeRU(full string) (string, bool) {
	words := strings.Fields(full)
	if len(words) == 0 {
		return "", false
	}
	gender := genderUnknown
	for _, w := range words {
		g := genderOf(strings.ToLower(w))
		if g == genderUnknown {
			continue
		}
		if gender != genderUnknown && gender != g {
			return "", false
		}
		gender = g
	}
	out := make([]string, len(words))
	for i, w := range words {
		lw := strings.ToLower(w)
		// «Асан уулу», «Асан кызы»: имя отца и «уулу/кызы» не склоняются.
		if lw == "уулу" || lw == "кызы" || (i+1 < len(words) && isPatronymicTail(words[i+1])) {
			out[i] = w
			continue
		}
		acc, ok := accusativeWord(w, gender)
		if !ok {
			return "", false
		}
		out[i] = acc
	}
	return strings.Join(out, " "), true
}

const (
	genderUnknown = iota
	genderMale
	genderFemale
)

func isPatronymicTail(w string) bool {
	lw := strings.ToLower(w)
	return lw == "уулу" || lw == "кызы"
}

// genderOf — пол по фамилии или отчеству (женские окончания проверяются первыми: «-ова» длиннее «-ов»).
func genderOf(lw string) int {
	switch {
	case lw == "кызы" || hasSuffix(lw, "ова", "ева", "ёва", "ина", "ына", "ская", "цкая", "овна", "евна", "ична"):
		return genderFemale
	case lw == "уулу" || hasSuffix(lw, "ов", "ев", "ёв", "ин", "ын", "ский", "цкий", "ович", "евич"):
		return genderMale
	}
	return genderUnknown
}

// fleeting — мужские имена с беглой гласной.
var fleeting = map[string]string{"павел": "павла", "лев": "льва", "пётр": "петра"}

// accusativeWord — одно слово имени в винительном падеже.
func accusativeWord(w string, gender int) (string, bool) {
	r := []rune(w)
	if len(r) < 2 || strings.ContainsRune(w, '-') {
		return "", false
	}
	for _, c := range r {
		if !unicode.Is(unicode.Cyrillic, c) {
			return "", false
		}
	}
	lw := strings.ToLower(w)
	if acc, ok := fleeting[lw]; ok { // Павел → Павла
		return string(r[:1]) + acc[len(string(unicode.ToLower(r[0]))):], true
	}
	switch {
	case hasSuffix(lw, "ская", "цкая"): // Ковальская → Ковальскую
		return replaceTail(r, 2, "ую"), true
	case strings.HasSuffix(lw, "а"): // Айдана → Айдану, Никита → Никиту
		return replaceTail(r, 1, "у"), true
	case strings.HasSuffix(lw, "я"): // София → Софию, Илья → Илью
		return replaceTail(r, 1, "ю"), true
	}
	last := unicode.ToLower(r[len(r)-1])
	if strings.ContainsRune("оеиуыэюё", last) {
		return w, true // Шевченко, Мади — не склоняются
	}
	switch gender {
	case genderFemale:
		return w, true // Айгерим, Асель, Ким — у женских имён на согласную форма та же
	case genderMale:
		switch {
		case hasSuffix(lw, "ых", "их"): // Черных — не склоняется
			return w, true
		case hasSuffix(lw, "ский", "цкий"): // Ковальский → Ковальского
			return replaceTail(r, 2, "ого"), true
		case hasSuffix(lw, "ий", "ей", "ай", "уй", "яй", "юй"): // Дмитрий → Дмитрия, Андрей → Андрея
			return replaceTail(r, 1, "я"), true
		case last == 'ь': // Игорь → Игоря
			return replaceTail(r, 1, "я"), true
		case last == 'й' || last == 'ъ':
			return "", false // Толстой, Белый — прилагательные, не угадываем
		}
		return w + matchCase(r, "а"), true // Бекжан → Бекжана, Асанов → Асанова
	}
	return "", false // согласная и пол не понять: «Бакыт Ким» — без склонения
}

func hasSuffix(s string, suffixes ...string) bool {
	for _, x := range suffixes {
		if strings.HasSuffix(s, x) {
			return true
		}
	}
	return false
}

// replaceTail — заменить n последних букв на tail в регистре слова (АЙДАНА → АЙДАНУ).
func replaceTail(r []rune, n int, tail string) string {
	return string(r[:len(r)-n]) + matchCase(r, tail)
}

func matchCase(r []rune, tail string) string {
	if unicode.IsUpper(r[len(r)-1]) && len(r) > 1 && unicode.IsUpper(r[len(r)-2]) {
		return strings.ToUpper(tail)
	}
	return tail
}
