package reminders

import "testing"

func TestWhatsAppPhone(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"0700123456", "996700123456", true},
		{"0700 123 456", "996700123456", true},
		{"+996700123456", "996700123456", true},
		{"+996 700 123 456", "996700123456", true},
		{"996700123456", "996700123456", true},
		{"(0700) 12-34-56", "996700123456", true},
		{"  0555 111 222  ", "996555111222", true},
		{"", "", false},
		{"12345", "", false},
		{"0700 123 45", "", false},
		{"+7 700 123 45 67", "", false},
		{"абв", "", false},
	} {
		got, ok := WhatsAppPhone(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("WhatsAppPhone(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNormalizeBroadcast(t *testing.T) {
	long := make([]rune, MaxBroadcastText+1)
	for i := range long {
		long[i] = 'я'
	}
	exact := string(long[:MaxBroadcastText])
	for _, tc := range []struct {
		name string
		in   BroadcastInput
		ok   bool
	}{
		{"центр", BroadcastInput{Audience: AudienceCenter, Text: "Текст"}, true},
		{"ровно 1000 символов кириллицей", BroadcastInput{Audience: AudienceCenter, Text: exact}, true},
		{"1001 символ", BroadcastInput{Audience: AudienceCenter, Text: string(long)}, false},
		{"только пробелы", BroadcastInput{Audience: AudienceCenter, Text: " \n "}, false},
		{"неизвестные получатели", BroadcastInput{Audience: "all", Text: "x"}, false},
		{"группы без групп", BroadcastInput{Audience: AudienceGroups, Text: "x"}, false},
	} {
		if _, err := normalizeBroadcast(tc.in); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}
