package auth

import (
	"strings"

	"okuu/internal/validate"
)

// NormalizePhone приводит номер к формату +996XXXXXXXXX.
// Принимает 0700123456, 996700123456, +996 700 123 456 (пробелы, дефисы и скобки игнорируются).
func NormalizePhone(s string) (string, error) {
	invalid := validate.Errorf("phone", "Телефон в формате +996XXXXXXXXX или 0XXXXXXXXX")

	var b strings.Builder
	for i, r := range strings.TrimSpace(s) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')':
		default:
			return "", invalid
		}
	}

	d := b.String()
	switch {
	case strings.HasPrefix(d, "+996"):
		d = d[len("+996"):]
	case strings.HasPrefix(d, "+"):
		return "", invalid
	case len(d) == 12 && strings.HasPrefix(d, "996"):
		d = d[len("996"):]
	case len(d) == 10 && d[0] == '0':
		d = d[1:]
	default:
		return "", invalid
	}
	if len(d) != 9 {
		return "", invalid
	}
	return "+996" + d, nil
}
