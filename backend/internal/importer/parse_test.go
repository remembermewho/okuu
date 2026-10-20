package importer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"okuu/internal/civil"
	"okuu/internal/validate"
)

func readFixture(t *testing.T, name string) []rawRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	table, err := readTable(data)
	if err != nil {
		t.Fatalf("%s: readTable: %v", name, err)
	}
	rows, err := parseTable(table)
	if err != nil {
		t.Fatalf("%s: parseTable: %v", name, err)
	}
	return rows
}

func TestReadXLSX(t *testing.T) {
	rows := readFixture(t, "students.xlsx")
	// Пустая строка 6 пропущена; номера — как в Excel.
	var nums []int
	for _, r := range rows {
		nums = append(nums, r.Num)
	}
	if fmt.Sprint(nums) != "[2 3 4 5 7 8 9]" {
		t.Fatalf("номера строк = %v, want [2 3 4 5 7 8 9]", nums)
	}
	r2, r3, r7 := rows[0].Values, rows[1].Values, rows[4].Values
	if r3[colLessonsLeft] != "5" || r2[colLessonsLeft] != "" || rows[6].Values[colLessonsLeft] != "201" {
		t.Errorf("остаток занятий: строка 3 %q, строка 2 %q, строка 9 %q", r3[colLessonsLeft], r2[colLessonsLeft], rows[6].Values[colLessonsLeft])
	}
	if r2[colFullName] != "Асанов Бакыт" || r2[colParentPhone] != "0700 123 456" || r2[colStartDate] != "45536" || r2[colDebt] != "1500" {
		t.Errorf("строка 2 = %v", r2)
	}
	if r3[colParentPhone] != "700222333" || r3[colTariff] != "абонемент 8" || r3[colStartDate] != "15.10.2024" || r3[colDebt] != "" {
		t.Errorf("строка 3 = %v", r3)
	}
	if r7[colDebt] != "2500.75" {
		t.Errorf("строка 7: долг = %q", r7[colDebt])
	}
}

// Один и тот же набор учеников в трёх CSV: UTF-8 с BOM и запятыми, UTF-8 с ;, windows-1251 с ;.
func TestReadCSVEncodingsAndDelimiters(t *testing.T) {
	var first []rawRow
	for _, name := range []string{"utf8_bom_comma.csv", "utf8_semicolon.csv", "cp1251_semicolon.csv"} {
		t.Run(name, func(t *testing.T) {
			rows := readFixture(t, name)
			if len(rows) != 4 || rows[0].Num != 2 || rows[3].Num != 5 {
				t.Fatalf("строки = %+v", rows)
			}
			if v := rows[0].Values; v[colFullName] != "Касымов Тимур" || v[colDebt] != "1 500,50" || v[colStartDate] != "01.09.2025" {
				t.Errorf("строка 2 = %v", v)
			}
			if v := rows[1].Values; v[colFullName] != "Ёлкина Алёна" || v[colParentName] != "Ёлкин Пётр" || v[colLessonsLeft] != "3" {
				t.Errorf("кириллица с ё: %v", v)
			}
			if first == nil {
				first = rows
				return
			}
			for i := range rows {
				for c, v := range first[i].Values {
					if rows[i].Values[c] != v {
						t.Errorf("строка %d колонка %d: %q, а в первом файле %q", rows[i].Num, c, rows[i].Values[c], v)
					}
				}
			}
		})
	}
}

func TestHeaders(t *testing.T) {
	tests := []struct {
		header  string
		want    column
		matches bool
	}{
		{"ФИО ученика*", colFullName, true},
		{"  фио  ", colFullName, true},
		{"Ученик", colFullName, true},
		{"ФИО ребёнка", colFullName, true},
		{"Телефон", colParentPhone, true},
		{"Тел.", colParentPhone, true},
		{"Текущий долг (сом)", colDebt, true},
		{"Долг, сом", colDebt, false}, // запятая — не синоним
		{"ГРУППА*", colGroup, true},
		{"Дата начала:", colStartDate, true},
		{"Осталось занятий (для абонемента)", colLessonsLeft, true},
		{"Остаток", colLessonsLeft, true},
		{"Занятий осталось", colLessonsLeft, true},
		{"Примечание", 0, false},
	}
	for _, tt := range tests {
		c, ok := headerSynonyms[normHeader(tt.header)]
		if ok != tt.matches || (ok && c != tt.want) {
			t.Errorf("%q → %v, %v; want %v, %v", tt.header, c, ok, tt.want, tt.matches)
		}
	}

	// Нет обязательных колонок — ошибка с их названиями.
	_, err := parseTable([]tableRow{{1, []string{"ФИО", "Телефон", "Группа"}}, {2, []string{"А", "0700000000", "Г"}}})
	var verr *validate.Error
	if !errors.As(err, &verr) || !strings.Contains(verr.Message, "«Имя родителя»") || !strings.Contains(verr.Message, "«Тариф»") {
		t.Errorf("нет колонок: %v", err)
	}
	// Заголовок не в первой строке (пустые строки сверху) — находится.
	rows, err := parseTable([]tableRow{{1, []string{"", ""}}, {2, strings.Split("ФИО,Родитель,Телефон,Группа,Тариф", ",")},
		{3, strings.Split("А,Б,0700000000,Г,Т", ",")}})
	if err != nil || len(rows) != 1 || rows[0].Num != 3 {
		t.Errorf("заголовок во 2-й строке: %+v %v", rows, err)
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		in   string
		want civil.Date
		ok   bool
	}{
		{"", civil.Date{}, true},
		{"2024-09-01", civil.New(2024, 9, 1), true},
		{"01.09.2024", civil.New(2024, 9, 1), true},
		{"1.9.2024", civil.New(2024, 9, 1), true},
		{"29.02.2024", civil.New(2024, 2, 29), true},
		{"45536", civil.New(2024, 9, 1), true},   // серийная дата Excel
		{"45536.5", civil.New(2024, 9, 1), true}, // с временем
		{"61", civil.New(1900, 3, 1), true},
		{"29.02.2025", civil.Date{}, false},
		{"32.13.2024", civil.Date{}, false},
		{"2024/09/01", civil.Date{}, false},
		{"сентябрь", civil.Date{}, false},
		{"60", civil.Date{}, false},
	}
	for _, tt := range tests {
		got, err := parseDate(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("parseDate(%q) = %v, %v; want %v ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestParseDebt(t *testing.T) {
	tests := []struct {
		in   string
		want int64 // тыйыны; -1 — ошибка
	}{
		{"", 0}, {"0", 0}, {"1500", 150_000}, {"1 500", 150_000}, {"1 500,50", 150_050},
		{"1500.5", 150_050}, {"2500.75", 250_075}, {"0,01", 1}, {"1500 сом", 150_000},
		{"1500.4999999999", 150_050}, {"1500.444", 150_044}, {"99.995", 10_000},
		{"-100", -1}, {"abc", -1}, {"1,5,0", -1}, {"1234567890", -1},
	}
	for _, tt := range tests {
		got, err := parseDebt(tt.in)
		if tt.want == -1 {
			if err == nil {
				t.Errorf("parseDebt(%q) = %d, want ошибку", tt.in, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("parseDebt(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
		}
	}
}

func TestParseLessons(t *testing.T) {
	tests := []struct {
		in   string
		want int32 // -1 — пусто (nil), -2 — ошибка
	}{
		{"", -1}, {" ", -1}, {"0", 0}, {"5", 5}, {"5.0", 5}, {"200", 200},
		{"201", -2}, {"-1", -2}, {"5.5", -2}, {"пять", -2}, {"1e2", -2},
	}
	for _, tt := range tests {
		got, err := parseLessons(tt.in)
		switch {
		case tt.want == -2:
			if err == nil {
				t.Errorf("parseLessons(%q) = %v, want ошибку", tt.in, *got)
			}
		case tt.want == -1:
			if err != nil || got != nil {
				t.Errorf("parseLessons(%q) = %v, %v; want nil", tt.in, got, err)
			}
		default:
			if err != nil || got == nil || *got != tt.want {
				t.Errorf("parseLessons(%q) = %v, %v; want %d", tt.in, got, err, tt.want)
			}
		}
	}
}

func TestParsePhone(t *testing.T) {
	tests := []struct {
		in, want string
		warn     bool
		ok       bool
	}{
		{"0700 123 456", "+996700123456", false, true},
		{"996700123456", "+996700123456", false, true},
		{"+996 (555) 12-34-56", "+996555123456", false, true},
		{"700222333", "+996700222333", true, true}, // Excel убрал ведущий 0
		{"700 222 333", "+996700222333", true, true},
		{"12345", "", false, false},
		{"07001234567", "", false, false},
		{"телефон", "", false, false},
	}
	for _, tt := range tests {
		got, warn, err := parsePhone(tt.in)
		if (err == nil) != tt.ok || got != tt.want || (warn != "") != tt.warn {
			t.Errorf("parsePhone(%q) = %q, %q, %v; want %q warn=%v ok=%v", tt.in, got, warn, err, tt.want, tt.warn, tt.ok)
		}
	}
}

func TestLimits(t *testing.T) {
	if _, err := readTable(make([]byte, MaxFileBytes+1)); !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("больше 5 МБ: %v", err)
	}

	csvRows := func(n int) []byte {
		var b strings.Builder
		b.WriteString("ФИО;Родитель;Телефон;Группа;Тариф\n")
		for i := range n {
			fmt.Fprintf(&b, "Ученик %d;Родитель;0700%06d;Группа;Тариф\n", i, i)
		}
		return []byte(b.String())
	}
	for _, tt := range []struct {
		n  int
		ok bool
	}{{MaxRows, true}, {MaxRows + 1, false}} {
		table, err := readTable(csvRows(tt.n))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := parseTable(table)
		if tt.ok && (err != nil || len(rows) != tt.n) {
			t.Errorf("%d строк: %d, %v", tt.n, len(rows), err)
		}
		var verr *validate.Error
		if !tt.ok && (!errors.As(err, &verr) || verr.Field != "file") {
			t.Errorf("%d строк: err = %v, want ошибку file", tt.n, err)
		}
	}

	for name, data := range map[string][]byte{
		"только заголовок": []byte("ФИО;Родитель;Телефон;Группа;Тариф\n"),
		"пустой":           []byte("\n\n"),
	} {
		table, _ := readTable(data)
		if _, err := parseTable(table); err == nil {
			t.Errorf("%s: ожидалась ошибка", name)
		}
	}
	if _, err := readTable([]byte("PK\x03\x04это не xlsx")); err == nil {
		t.Error("битый xlsx: ожидалась ошибка")
	}
}
