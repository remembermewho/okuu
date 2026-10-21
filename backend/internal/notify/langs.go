package notify

import (
	"fmt"
	"strings"
)

// Langs — языки сообщений родителям (настройка BOT_LANGUAGES). Русский есть всегда. Нулевое значение —
// только русский: команды /lang и кнопок выбора языка нет, все сообщения на русском независимо от
// parents.lang (выбор родителя сохраняется и снова заработает, когда включат кыргызский).
type Langs struct {
	ky bool
}

// ParseLangs разбирает BOT_LANGUAGES: «ru» или «ru,ky» (пробелы и регистр не важны; пусто — ru).
func ParseLangs(s string) (Langs, error) {
	var l Langs
	hasRU := false
	for _, part := range strings.Split(s, ",") {
		switch p := strings.ToLower(strings.TrimSpace(part)); p {
		case "":
		case LangRU:
			hasRU = true
		case LangKY:
			l.ky = true
		default:
			return Langs{}, fmt.Errorf("неизвестный язык %q (ru, ky)", p)
		}
	}
	if !hasRU && strings.TrimSpace(s) != "" {
		return Langs{}, fmt.Errorf("в списке обязательно ru")
	}
	return l, nil
}

// MustLangs — для тестов: разбор без ошибки.
func MustLangs(s string) Langs {
	l, err := ParseLangs(s)
	if err != nil {
		panic(err)
	}
	return l
}

// Multi — родитель может выбрать язык (/lang, кнопки выбора).
func (l Langs) Multi() bool { return l.ky }

// List — включённые языки для логов: «ru» или «ru,ky».
func (l Langs) List() string {
	if l.ky {
		return LangRU + "," + LangKY
	}
	return LangRU
}

// Resolve — язык, на котором писать родителю: его выбор, если язык включён, иначе русский.
func (l Langs) Resolve(lang string) string {
	if l.ky && lang == LangKY {
		return LangKY
	}
	return LangRU
}

// For — тексты на языке родителя с учётом включённых языков. Только русский — тексты без упоминания /lang.
func (l Langs) For(lang string) *Texts {
	if !l.ky {
		return ruOnly
	}
	return For(lang)
}

// ruOnly — русские тексты для режима «только русский»: без команды /lang в подсказках.
var ruOnly = func() *Texts {
	t := *ru
	t.Linked = ruLinked(false)
	t.Welcome = ruWelcome(false)
	t.Help = ruHelp(false)
	return &t
}()
