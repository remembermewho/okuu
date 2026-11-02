package demo

import (
	"okuu/internal/civil"
	"okuu/internal/notify"
	"okuu/internal/telegram/bot"
)

// SampleCenter — название центра в макете чата: «Центр «Пример»» (без «Демо:», как у настоящего центра).
const SampleCenter = "Пример"

// Message — сообщение бота для макета чата на лендинге.
type Message struct {
	Text    string   `json:"text"`
	Buttons []string `json:"buttons"` // по строке на кнопку, сверху вниз
}

// SampleReminder — настоящее автонапоминание «через 3 дня срок оплаты» тем же кодом, что и рассылка
// (notify.DebtReminder, bot.BalanceKeyboard): двое детей одного родителя — в одном сообщении (R18),
// «Как оплатить» — реквизиты и инструкция демо-центра.
func SampleReminder(today civil.Date) Message {
	tx := notify.For(notify.LangRU)
	due := today.AddDays(3)
	kids := []notify.ChildDebt{
		{Name: "Попов Матвей", Debt: 315_000, Due: &due},
		{Name: "Попова Ева", Debt: 315_000, Due: &due},
	}
	m := Message{Text: notify.DebtReminder(tx, notify.KindDueSoon, SampleCenter, due, kids, PayDetails)}
	for _, row := range bot.BalanceKeyboard(tx).InlineKeyboard {
		for _, b := range row {
			m.Buttons = append(m.Buttons, b.Text)
		}
	}
	return m
}
