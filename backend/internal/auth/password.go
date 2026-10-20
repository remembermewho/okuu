package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"okuu/internal/validate"
)

// Параметры argon2id по рекомендации OWASP: m=19 MiB, t=2, p=1.
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16

	MinPasswordLen = 8
	// MaxPasswordLen ограничивает работу хэширования на один запрос.
	MaxPasswordLen = 128
)

var errBadHash = errors.New("неверный формат хэша пароля")

// ValidatePassword проверяет длину пароля в символах.
func ValidatePassword(pw string) error {
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordLen {
		return validate.Errorf("password", "Пароль — минимум %d символов", MinPasswordLen)
	}
	if n > MaxPasswordLen {
		return validate.Errorf("password", "Пароль — максимум %d символов", MaxPasswordLen)
	}
	return nil
}

// HashPassword возвращает хэш argon2id в PHC-формате:
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash> (base64 без паддинга).
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword сравнивает пароль с PHC-хэшем за постоянное время.
// Параметры берутся из самого хэша, поэтому старые хэши проверяются и после смены параметров.
func VerifyPassword(pw, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, errBadHash
	}
	// Защита от хэша с абсурдными параметрами (до 1 GiB, 10 проходов).
	if m == 0 || m > 1<<20 || t == 0 || t > 10 || p == 0 {
		return false, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false, errBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 64 {
		return false, errBadHash
	}

	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash используется при входе с несуществующим телефоном, чтобы время
// ответа не выдавало, зарегистрирован ли номер.
var dummyHash = sync.OnceValue(func() string {
	h, err := HashPassword("okuu-dummy-password")
	if err != nil {
		panic(err)
	}
	return h
})
