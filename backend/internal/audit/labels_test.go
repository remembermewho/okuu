package audit_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"okuu/internal/billing"
)

// У каждого действия, которое пишется в audit_log, есть русская подпись в «Журнале»
// (frontend/src/i18n/ru.ts, audit.actions). Действия — все строковые константы audit.go
// и «ledger.<вид операции>» (оплата, возврат, поправка пишутся по виду). Добавили действие
// без подписи — тест падает, а в журнале не появится код вроде invite.reissue.
func TestEveryActionHasRussianLabel(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "frontend", "src", "i18n", "ru.ts"))
	if err != nil {
		t.Skipf("нет фронтенда рядом: %v", err)
	}
	block := regexp.MustCompile(`(?s)\n    actions: \{\n(.*?)\n    \} as Record<string, string>`).FindSubmatch(src)
	if block == nil {
		t.Fatal("в ru.ts не найден блок audit.actions")
	}
	labels := map[string]string{}
	for _, m := range regexp.MustCompile(`'([a-z_]+\.[a-z_]+)': '([^']+)'`).FindAllStringSubmatch(string(block[1]), -1) {
		labels[m[1]] = m[2]
	}

	actions := auditConstants(t)
	for _, kind := range []string{billing.KindPayment, billing.KindRefund, billing.KindCorrection, billing.KindCharge} {
		actions = append(actions, "ledger."+kind)
	}
	if len(actions) < 30 {
		t.Fatalf("найдено только %d действий — разбор audit.go сломался?", len(actions))
	}
	for _, a := range actions {
		if l := strings.TrimSpace(labels[a]); l == "" {
			t.Errorf("действие %q без русской подписи — добавьте его в audit.actions (frontend/src/i18n/ru.ts)", a)
		}
	}
}

// auditConstants — значения всех строковых констант audit.go вида «сущность.действие».
func auditConstants(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "audit.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, spec := range g.Specs {
			for _, v := range spec.(*ast.ValueSpec).Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(s, ".") {
					out = append(out, s)
				}
			}
		}
	}
	return out
}
