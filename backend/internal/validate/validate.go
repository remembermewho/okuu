// Package validate — ошибки входных данных и общие проверки полей.
package validate

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxNameLen — предел длины имён и названий.
const MaxNameLen = 100

// Error — ошибка входных данных в конкретном поле; API отвечает validation с полем.
// Unprocessable=false — значение некорректно само по себе (400);
// true — корректно по форме, но ссылается не на то: чужой центр, архив, не сотрудник (422).
type Error struct {
	Field         string
	Message       string
	Unprocessable bool
}

func (e *Error) Error() string { return e.Field + ": " + e.Message }

// Errorf создаёт ошибку поля (400).
func Errorf(field, format string, args ...any) *Error {
	return &Error{Field: field, Message: fmt.Sprintf(format, args...)}
}

// Unprocessablef создаёт ошибку ссылки в поле (422).
func Unprocessablef(field, format string, args ...any) *Error {
	return &Error{Field: field, Message: fmt.Sprintf(format, args...), Unprocessable: true}
}

// Name обрезает пробелы и проверяет имя/название: не пустое, до MaxNameLen символов, корректный UTF-8.
func Name(field, label, v string) (string, error) {
	v, err := OptionalName(field, label, v)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", Errorf(field, "%s — обязательное поле", label)
	}
	return v, nil
}

// OptionalName — как Name, но пустое значение допустимо.
func OptionalName(field, label, v string) (string, error) {
	v = strings.TrimSpace(v)
	// encoding/json заменяет байты не в UTF-8 на U+FFFD — не сохраняем такой мусор.
	if strings.ContainsRune(v, utf8.RuneError) {
		return "", Errorf(field, "%s — некорректная кодировка, нужен UTF-8", label)
	}
	if utf8.RuneCountInString(v) > MaxNameLen {
		return "", Errorf(field, "%s — не длиннее %d символов", label, MaxNameLen)
	}
	return v, nil
}
