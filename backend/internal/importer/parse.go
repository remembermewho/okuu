// Package importer — импорт учеников из Excel (.xlsx) и CSV (F4): разбор файла, превью
// со статусами по строкам и подтверждение одной транзакцией.
package importer

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"

	"okuu/internal/auth"
	"okuu/internal/civil"
	"okuu/internal/validate"
)

const (
	// MaxFileBytes — предел размера файла импорта.
	MaxFileBytes = 5 << 20
	// MaxRows — предел строк с учениками (без заголовка и пустых строк).
	MaxRows = 2000
)

// ErrFileTooLarge — файл больше MaxFileBytes.
var ErrFileTooLarge = errors.New("файл больше 5 МБ")

// Колонки шаблона.
type column int

const (
	colFullName column = iota
	colParentName
	colParentPhone
	colGroup
	colTariff
	colStartDate
	colDebt
	colLessonsLeft
)

// TemplateHeaders — заголовки шаблона (* — обязательные).
var TemplateHeaders = []string{
	"ФИО ученика*", "Имя родителя*", "Телефон родителя*", "Группа*", "Тариф*", "Дата начала", "Текущий долг (сом)",
	"Осталось занятий (для абонемента)",
}

var columnLabels = map[column]string{
	colFullName: "ФИО ученика", colParentName: "Имя родителя", colParentPhone: "Телефон родителя",
	colGroup: "Группа", colTariff: "Тариф", colStartDate: "Дата начала", colDebt: "Текущий долг",
	colLessonsLeft: "Осталось занятий",
}

var requiredColumns = []column{colFullName, colParentName, colParentPhone, colGroup, colTariff}

// headerSynonyms — заголовки после normHeader (нижний регистр, ё→е, без «*» и скобок).
var headerSynonyms = map[string]column{
	"фио ученика": colFullName, "фио": colFullName, "ученик": colFullName, "имя ученика": colFullName,
	"фио ребенка": colFullName, "ребенок": colFullName, "ученица": colFullName, "ф.и.о": colFullName,
	"ф.и.о.": colFullName, "студент": colFullName,

	"имя родителя": colParentName, "родитель": colParentName, "фио родителя": colParentName,
	"контактное лицо": colParentName,

	"телефон родителя": colParentPhone, "телефон": colParentPhone, "тел": colParentPhone, "тел.": colParentPhone,
	"номер телефона": colParentPhone, "номер": colParentPhone, "телефон мамы": colParentPhone, "whatsapp": colParentPhone,

	"группа": colGroup, "класс": colGroup,

	"тариф": colTariff, "тариф обучения": colTariff,

	"дата начала": colStartDate, "начало": colStartDate, "дата записи": colStartDate, "дата": colStartDate,
	"начало обучения": colStartDate,

	"текущий долг": colDebt, "долг": colDebt, "задолженность": colDebt, "остаток долга": colDebt,

	"осталось занятий": colLessonsLeft, "остаток": colLessonsLeft, "занятий осталось": colLessonsLeft,
	"остаток занятий": colLessonsLeft, "осталось уроков": colLessonsLeft,
}

// tableRow — строка таблицы; Num — номер строки как в Excel (с 1).
type tableRow struct {
	Num   int
	Cells []string
}

// readTable читает xlsx (первый лист) или csv. Тип определяется по содержимому, не по имени файла.
func readTable(data []byte) ([]tableRow, error) {
	if len(data) > MaxFileBytes {
		return nil, ErrFileTooLarge
	}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return readXLSX(data)
	}
	return readCSV(data)
}

func readXLSX(data []byte) ([]tableRow, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, validate.Errorf("file", "Не удалось прочитать файл Excel — сохраните его как .xlsx или .csv")
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, validate.Errorf("file", "В файле Excel нет листов")
	}
	// Сырые значения: даты — серийные числа Excel, телефоны и суммы — как есть, без формата ячейки.
	rows, err := f.GetRows(sheets[0], excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, validate.Errorf("file", "Не удалось прочитать лист «%s»", sheets[0])
	}
	out := make([]tableRow, 0, len(rows))
	for i, r := range rows {
		out = append(out, tableRow{Num: i + 1, Cells: r})
	}
	return out, nil
}

// readCSV: кодировка UTF-8 (с BOM или без) или windows-1251 (Excel на русской Windows),
// разделитель ; , или табуляция — по первой непустой строке.
func readCSV(data []byte) ([]tableRow, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		decoded, err := charmap.Windows1251.NewDecoder().Bytes(data)
		if err != nil {
			return nil, validate.Errorf("file", "Не удалось определить кодировку CSV — сохраните файл в UTF-8")
		}
		data = decoded
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = detectDelimiter(data)
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	var out []tableRow
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, validate.Errorf("file", "Не удалось разобрать CSV: %v", err)
		}
		line, _ := r.FieldPos(0)
		out = append(out, tableRow{Num: line, Cells: rec})
	}
	return out, nil
}

func detectDelimiter(data []byte) rune {
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		best, bestN := ',', bytes.Count(line, []byte(","))
		for _, d := range []rune{';', '\t'} {
			if n := bytes.Count(line, []byte(string(d))); n > bestN {
				best, bestN = d, n
			}
		}
		return best
	}
	return ','
}

// rawRow — строка данных: значения по колонкам, без пробелов по краям.
type rawRow struct {
	Num    int
	Values map[column]string
}

// parseTable находит заголовок (первая непустая строка), сопоставляет колонки и отдаёт строки данных.
func parseTable(rows []tableRow) ([]rawRow, error) {
	hdr := -1
	for i, r := range rows {
		if !emptyRow(r.Cells) {
			hdr = i
			break
		}
	}
	if hdr < 0 {
		return nil, validate.Errorf("file", "Файл пустой")
	}
	cols := map[column]int{}
	for i, h := range rows[hdr].Cells {
		if c, ok := headerSynonyms[normHeader(h)]; ok {
			if _, dup := cols[c]; !dup {
				cols[c] = i
			}
		}
	}
	var missing []string
	for _, c := range requiredColumns {
		if _, ok := cols[c]; !ok {
			missing = append(missing, "«"+columnLabels[c]+"»")
		}
	}
	if len(missing) > 0 {
		return nil, validate.Errorf("file", "Не найдены колонки %s — скачайте шаблон и проверьте заголовки в строке %d",
			strings.Join(missing, ", "), rows[hdr].Num)
	}

	var out []rawRow
	for _, r := range rows[hdr+1:] {
		if emptyRow(r.Cells) {
			continue
		}
		if len(out) == MaxRows {
			return nil, validate.Errorf("file", "В файле больше %d строк с учениками — разбейте его на части", MaxRows)
		}
		v := map[column]string{}
		for c, i := range cols {
			if i < len(r.Cells) {
				v[c] = strings.TrimSpace(r.Cells[i])
			}
		}
		out = append(out, rawRow{Num: r.Num, Values: v})
	}
	if len(out) == 0 {
		return nil, validate.Errorf("file", "В файле нет строк с учениками — только заголовок")
	}
	return out, nil
}

var parens = regexp.MustCompile(`\([^)]*\)`)

func normHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "ё", "е")
	s = parens.ReplaceAllString(s, "")
	s = strings.NewReplacer("*", "", "_", " ", " ", " ").Replace(s)
	s = strings.TrimRight(strings.TrimSpace(s), ":")
	return strings.Join(strings.Fields(s), " ")
}

// normName — ключ для сравнения ФИО и названий: нижний регистр, ё→е, одинарные пробелы.
func normName(s string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(strings.ToLower(s)), " "), "ё", "е")
}

func emptyRow(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

var (
	dottedDate = regexp.MustCompile(`^(\d{1,2})\.(\d{1,2})\.(\d{4})$`)
	serialDate = regexp.MustCompile(`^\d+(\.\d+)?$`)
	excelEpoch = civil.New(1899, 12, 30)
)

// parseDate: ГГГГ-ММ-ДД, ДД.ММ.ГГГГ (и Д.М.ГГГГ) или серийная дата Excel. Пустая строка — нулевая дата.
func parseDate(s string) (civil.Date, error) {
	if s == "" {
		return civil.Date{}, nil
	}
	if d, err := civil.Parse(s); err == nil {
		return d, nil
	}
	if m := dottedDate.FindStringSubmatch(s); m != nil {
		day, _ := strconv.Atoi(m[1])
		mon, _ := strconv.Atoi(m[2])
		year, _ := strconv.Atoi(m[3])
		d := civil.New(year, time.Month(mon), day)
		if d.Day() != day || int(d.Month()) != mon || d.Year() != year {
			return civil.Date{}, fmt.Errorf("дата «%s» не существует", s)
		}
		return d, nil
	}
	if serialDate.MatchString(s) {
		n, _ := strconv.Atoi(strings.SplitN(s, ".", 2)[0]) // дробная часть — время, отбрасываем
		if n < 61 || n > 2958465 {                         // до 1 марта 1900 Excel считает даты с ошибкой
			return civil.Date{}, fmt.Errorf("«%s» не похоже на дату", s)
		}
		return excelEpoch.AddDays(n), nil
	}
	return civil.Date{}, fmt.Errorf("дата «%s» — ожидается ДД.ММ.ГГГГ или ГГГГ-ММ-ДД", s)
}

var money = regexp.MustCompile(`^(\d+)(?:\.(\d+))?$`)

// parseDebt — сумма в сомах («1500», «1 500,50», «1500.5») в тыйыны без float.
// Больше двух знаков после запятой (артефакт Excel вроде 1500.4999999) — округление до тыйына.
func parseDebt(s string) (int64, error) {
	s = strings.NewReplacer(" ", "", " ", "", " ", "", ",", ".").Replace(strings.ToLower(s))
	s = strings.TrimSuffix(strings.TrimSuffix(s, "сом"), "с")
	if s == "" {
		return 0, nil
	}
	if strings.HasPrefix(s, "-") {
		return 0, errors.New("долг не может быть отрицательным — переплату внесите оплатой после импорта")
	}
	m := money.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("сумма «%s» — ожидается число в сомах, например 1500 или 1500,50", s)
	}
	if len(m[1]) > 9 {
		return 0, fmt.Errorf("сумма «%s» слишком большая", s)
	}
	som, _ := strconv.ParseInt(m[1], 10, 64)
	frac := m[2] + "00"
	tyiyn, _ := strconv.ParseInt(frac[:2], 10, 64)
	if len(frac) > 4 && frac[2] >= '5' { // после дополнения нулями третий знак — это frac[2]
		tyiyn++
	}
	return som*100 + tyiyn, nil
}

// MaxLessonsLeft — предел остатка занятий абонемента в файле (как lessons_count тарифа).
const MaxLessonsLeft = 200

var wholeNumber = regexp.MustCompile(`^\d+(\.0+)?$`)

// parseLessons — остаток занятий: целое 0–MaxLessonsLeft («5» или «5.0» из Excel). Пусто — nil.
func parseLessons(s string) (*int32, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if !wholeNumber.MatchString(s) {
		return nil, fmt.Errorf("«%s» — нужно целое число от 0 до %d", s, MaxLessonsLeft)
	}
	n, err := strconv.Atoi(strings.SplitN(s, ".", 2)[0])
	if err != nil || n > MaxLessonsLeft {
		return nil, fmt.Errorf("«%s» — нужно целое число от 0 до %d", s, MaxLessonsLeft)
	}
	v := int32(n)
	return &v, nil
}

// parsePhone — та же нормализация, что у сотрудников. Excel теряет ведущий ноль у номера,
// набранного как число («700123456»), — такой принимаем как +996… с предупреждением.
func parsePhone(s string) (phone string, warning string, err error) {
	if p, err := auth.NormalizePhone(s); err == nil {
		return p, "", nil
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		if r == ' ' || r == '-' || r == '(' || r == ')' {
			return -1
		}
		return 'x'
	}, s)
	if len(digits) == 9 && !strings.Contains(digits, "x") {
		p := "+996" + digits
		return p, fmt.Sprintf("Телефон «%s» без 0 в начале (Excel убрал ноль) — принят как %s", s, p), nil
	}
	return "", "", fmt.Errorf("телефон «%s» не распознан — нужен формат 0700 123 456 или +996 700 123 456", s)
}
