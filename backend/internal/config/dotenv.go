package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

// LoadDotEnv загружает переменные из первого найденного файла среди paths.
// Уже заданные переменные окружения не перезаписываются. Используется только в dev.
// Возвращает путь загруженного файла или "" если ни один не найден.
func LoadDotEnv(paths ...string) (string, error) {
	for _, p := range paths {
		f, err := os.Open(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		vars, err := parseDotEnv(f)
		f.Close()
		if err != nil {
			return "", fmt.Errorf("%s: %w", p, err)
		}
		for k, v := range vars {
			if _, ok := os.LookupEnv(k); !ok {
				os.Setenv(k, v)
			}
		}
		return p, nil
	}
	return "", nil
}

// parseDotEnv разбирает строки вида KEY=VALUE. Пустые строки и строки
// с # в начале пропускаются; значение можно взять в одинарные или двойные кавычки.
func parseDotEnv(r io.Reader) (map[string]string, error) {
	vars := map[string]string{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("строка %d: ожидается KEY=VALUE", n)
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		vars[key] = val
	}
	return vars, sc.Err()
}
