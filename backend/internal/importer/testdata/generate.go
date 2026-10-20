//go:build ignore

// Генератор тестовых файлов импорта: go run ./internal/importer/testdata/generate.go (из backend/).
// Файлы коммитятся; генератор — чтобы их можно было пересоздать и видеть, что внутри.
package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"
)

const dir = "internal/importer/testdata"

func main() {
	writeXLSX()

	// Одни и те же 4 ученика в трёх вариантах CSV. Заголовки — синонимы, регистр разный.
	rows := [][]string{
		{"Касымов Тимур", "Касымова Айгерим", "0555 111 222", "Английский A1", "Английский, месяц", "01.09.2025", "1 500,50", ""},
		{"Ёлкина Алёна", "Ёлкин Пётр", "+996 777 333 444", "Робототехника", "Абонемент 8", "2025-10-01", "", "3"},
		{"Нурланова Дана", "Нурланов Азамат", "0700-555-666", "английский a1", "АНГЛИЙСКИЙ, МЕСЯЦ", "", "300", ""},
		{"Ошибка Строки", "Родитель", "123", "Английский A1", "Нет такого", "99.99.2025", "abc", ""},
	}
	write("utf8_bom_comma.csv", "\xef\xbb\xbf"+csvText(",", []string{"ФИО", "Родитель", "Телефон", "Группа", "Тариф", "Дата", "Долг", "Остаток"}, rows), false)
	header := []string{"фио ученика*", "имя родителя*", "телефон родителя*", "группа*", "тариф*", "дата начала", "текущий долг (сом)", "занятий осталось"}
	write("utf8_semicolon.csv", csvText(";", header, rows), false)
	write("cp1251_semicolon.csv", csvText(";", header, rows), true)
}

func writeXLSX() {
	f := excelize.NewFile()
	const s = "Sheet1"
	set := func(cell string, v any) {
		if err := f.SetCellValue(s, cell, v); err != nil {
			log.Fatal(err)
		}
	}
	headers := []string{"ФИО ученика*", "Имя родителя*", "Телефон родителя*", "Группа*", "Тариф*", "Дата начала", "Текущий долг (сом)", "Осталось занятий"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		set(cell, h)
	}
	data := [][]any{
		// 2: всё в порядке; дата — серийное число Excel (45536 = 2024-09-01), долг — число.
		{"Асанов Бакыт", "Асанова Гульнара", "0700 123 456", "Английский A1", "Английский, месяц", 45536, 1500},
		// 3: телефон числом без ведущего 0; новая группа; тариф в другом регистре; дата текстом ДД.ММ.ГГГГ;
		//    абонемент с остатком 5 занятий — без начисления.
		{"Бекова Айпери", "Бекова Нургуль", 700222333, "Шахматы", "абонемент 8", "15.10.2024", nil, 5},
		// 4: ошибки — нет имени родителя, плохой телефон, нет тарифа, несуществующая дата, отрицательный долг.
		{"Ошибкин Тест", "", "12345", "Английский A1", "Несуществующий тариф", "32.13.2024", -100},
		// 5: дубликат строки 2 (ФИО в другом регистре, тот же телефон в другом формате).
		{"асанов  бакыт", "Асанова Гульнара", "+996700123456", "Английский A1", "Английский, месяц", nil, nil},
		// 6: пустая строка — пропускается.
		{},
		// 7: телефон 996…, ISO-дата, долг с тыйынами; остаток у месячного тарифа — игнорируется (warning).
		{"Давлетов Эрлан", "Давлетова Динара", "996 555 444 333", "Шахматы", "Английский, месяц", "2025-03-01", 2500.75, 4},
		// 8: абонемент без остатка — ошибка.
		{"Пакетова Без Остатка", "Пакетова Мать", "0700 800 001", "Шахматы", "Абонемент 8", nil, nil, nil},
		// 9: абонемент с остатком больше 200 — ошибка.
		{"Пакетов Много", "Пакетов Отец", "0700 800 002", "Шахматы", "Абонемент 8", nil, nil, 201},
	}
	for r, row := range data {
		for c, v := range row {
			if v == nil {
				continue
			}
			cell, _ := excelize.CoordinatesToCellName(c+1, r+2)
			set(cell, v)
		}
	}
	if err := f.SaveAs(filepath.Join(dir, "students.xlsx")); err != nil {
		log.Fatal(err)
	}
}

func csvText(sep string, header []string, rows [][]string) string {
	var b strings.Builder
	line := func(cells []string) {
		for i, c := range cells {
			if i > 0 {
				b.WriteString(sep)
			}
			if strings.ContainsAny(c, sep+"\"\n") {
				c = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
			}
			b.WriteString(c)
		}
		b.WriteString("\r\n")
	}
	line(header)
	for _, r := range rows {
		line(r)
	}
	return b.String()
}

func write(name, text string, cp1251 bool) {
	data := []byte(text)
	if cp1251 {
		var err error
		if data, err = charmap.Windows1251.NewEncoder().Bytes(data); err != nil {
			log.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		log.Fatal(err)
	}
}
