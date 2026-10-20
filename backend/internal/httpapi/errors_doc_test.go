package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Каждый code, который может вернуть API, описан в docs/errors.md и упомянут в docs/openapi.yaml.
// Новый код в knownErrors без документации — тест красный.
func TestErrorCodesDocumented(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(filepath.FromSlash("../../../docs/" + name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	errorsMD, spec := read("errors.md"), read("openapi.yaml")

	codes := map[string]bool{}
	for _, k := range knownErrors {
		codes[k.code] = true
	}
	// Коды, которые пишутся не через knownErrors (middleware, разбор запроса, лимиты).
	for _, c := range []string{"validation", "bad_request", "unauthorized", "bad_origin", "forbidden", "not_found",
		"method_not_allowed", "too_large", "internal", "too_many_attempts", "too_many_requests", "file_too_large"} {
		codes[c] = true
	}
	// Только у вебхуков (их нет в спецификации для фронтенда).
	webhookOnly := map[string]bool{"bad_signature": true}
	for code := range codes {
		if !strings.Contains(errorsMD, "`"+code+"`") {
			t.Errorf("код %q не описан в docs/errors.md", code)
		}
		if !webhookOnly[code] && !strings.Contains(spec, "`"+code+"`") {
			t.Errorf("код %q не упомянут в docs/openapi.yaml", code)
		}
	}
}
