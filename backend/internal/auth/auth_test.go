package auth

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"okuu/internal/validate"
)

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		in, want string // want == "" — ошибка validation
	}{
		{"0700123456", "+996700123456"},
		{"996700123456", "+996700123456"},
		{"+996700123456", "+996700123456"},
		{"+996 700 123 456", "+996700123456"},
		{"  0 (700) 12-34-56 ", "+996700123456"},
		{"+996 (555) 12-34-56", "+996555123456"},

		{"", ""},
		{"700123456", ""},       // без кода и без 0
		{"07001234567", ""},     // лишняя цифра
		{"070012345", ""},       // не хватает цифры
		{"+99670012345", ""},    // не хватает цифры
		{"+9967001234567", ""},  // лишняя цифра
		{"+7 700 123 4567", ""}, // чужая страна
		{"99670012345", ""},
		{"0700-123-45a", ""}, // буква
		{"0700+123456", ""},  // + не в начале
		{"++996700123456", ""},
		{"+996 0700123456", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NormalizePhone(tt.in)
			if tt.want == "" {
				var verr *validate.Error
				if !errors.As(err, &verr) || verr.Field != "phone" {
					t.Fatalf("NormalizePhone(%q) = %q, %v; want validate.Error(phone)", tt.in, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("NormalizePhone(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name string
		pw   string
		ok   bool
	}{
		{"7 символов", "1234567", false},
		{"8 символов", "12345678", true},
		{"8 кириллических (16 байт)", "пароль12", true},
		{"7 кириллических (14 байт)", "парольд", false},
		{"128 символов", strings.Repeat("a", 128), true},
		{"129 символов", strings.Repeat("a", 129), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidatePassword(tt.pw); (err == nil) != tt.ok {
				t.Fatalf("ValidatePassword: err = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestHashPassword(t *testing.T) {
	h1, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	phc := regexp.MustCompile(`^\$argon2id\$v=19\$m=19456,t=2,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$`)
	if !phc.MatchString(h1) {
		t.Fatalf("хэш не в PHC-формате с параметрами OWASP: %s", h1)
	}

	h2, _ := HashPassword("correct horse")
	if h1 == h2 {
		t.Error("одинаковые хэши для одного пароля: соль не случайная")
	}

	tests := []struct {
		name    string
		pw      string
		hash    string
		ok      bool
		wantErr bool
	}{
		{"верный пароль", "correct horse", h1, true, false},
		{"верный пароль, второй хэш", "correct horse", h2, true, false},
		{"неверный пароль", "correct horsE", h1, false, false},
		{"пустой пароль", "", h1, false, false},
		{"bcrypt вместо argon2id", "x", "$2a$10$abcdefghijklmnopqrstuv", false, true},
		{"мусор", "x", "not-a-hash", false, true},
		{"argon2i", "x", strings.Replace(h1, "argon2id", "argon2i", 1), false, true},
		{"абсурдная память", "x", strings.Replace(h1, "m=19456", "m=99999999", 1), false, true},
		{"битая соль", "x", strings.Replace(h1, "$m=19456,t=2,p=1$", "$m=19456,t=2,p=1$!!", 1), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := VerifyPassword(tt.pw, tt.hash)
			if (err != nil) != tt.wantErr || ok != tt.ok {
				t.Fatalf("VerifyPassword = %v, %v; want ok=%v wantErr=%v", ok, err, tt.ok, tt.wantErr)
			}
		})
	}
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func TestLimiter(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	l := NewLimiter(5, 15*time.Minute, clock.Now)

	// 4 неудачи — ещё можно.
	for range 4 {
		l.Fail("phone:a", "ip:1")
		clock.Advance(time.Minute)
	}
	if blocked, _ := l.Blocked("phone:a", "ip:1"); blocked {
		t.Fatal("заблокировано после 4 неудач")
	}

	// 5-я — блок по обоим ключам; другие ключи свободны.
	l.Fail("phone:a", "ip:1")
	if blocked, retry := l.Blocked("phone:a"); !blocked || retry <= 0 || retry > 15*time.Minute {
		t.Fatalf("phone:a: blocked=%v retry=%v", blocked, retry)
	}
	if blocked, _ := l.Blocked("ip:1"); !blocked {
		t.Fatal("ip:1 не заблокирован")
	}
	if blocked, _ := l.Blocked("phone:b", "ip:2"); blocked {
		t.Fatal("чужие ключи заблокированы")
	}

	// Первая неудача была в 12:00 → выйдет из окна в 12:15; сейчас 12:04.
	if _, retry := l.Blocked("phone:a"); retry != 11*time.Minute {
		t.Errorf("retry = %v, want 11m", retry)
	}
	clock.Advance(11 * time.Minute)
	if blocked, _ := l.Blocked("phone:a"); blocked {
		t.Fatal("блок не снялся, когда старая неудача вышла из окна")
	}

	// Reset снимает счётчик ключа, другой ключ остаётся.
	l.Fail("phone:a", "ip:1")
	l.Reset("phone:a")
	if blocked, _ := l.Blocked("phone:a"); blocked {
		t.Error("phone:a заблокирован после Reset")
	}
	if blocked, _ := l.Blocked("ip:1"); !blocked {
		t.Error("ip:1 разблокирован чужим Reset")
	}

	// Через окно всё забывается, а карта очищается.
	clock.Advance(16 * time.Minute)
	if blocked, _ := l.Blocked("ip:1"); blocked {
		t.Error("ip:1 заблокирован после окна")
	}
	if n := len(l.fails); n != 0 {
		t.Errorf("в карте осталось %d ключей", n)
	}
}
