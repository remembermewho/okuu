package demo

import (
	"github.com/google/uuid"

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
// (notify.DebtReminder, bot.DebtKeyboard): двое детей одного родителя — в одном сообщении (R18).
func SampleReminder(today civil.Date) Message {
	tx := notify.For(notify.LangRU)
	due := today.AddDays(3)
	kids := []notify.ChildDebt{
		{Name: "Попов Матвей", Debt: 315_000, Due: &due},
		{Name: "Попова Ева", Debt: 315_000, Due: &due},
	}
	pay := make([]bot.PayChild, len(kids))
	for i, k := range kids {
		pay[i] = bot.PayChild{StudentID: uuid.New(), Name: k.Name, Debt: k.Debt}
	}
	m := Message{Text: notify.DebtReminder(tx, notify.KindDueSoon, SampleCenter, due, kids, PayDetails)}
	for _, row := range bot.DebtKeyboard(tx, pay).InlineKeyboard {
		for _, b := range row {
			m.Buttons = append(m.Buttons, b.Text)
		}
	}
	return m
}
