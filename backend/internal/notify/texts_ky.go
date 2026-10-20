package notify

// Тексты на кыргызском языке — ТРЕБУЕТ ПРОВЕРКИ НОСИТЕЛЕМ ЯЗЫКА (чек-лист готовности к пилоту в ТЗ).
// Смысл каждой строки совпадает с texts_ru.go; меняя одну, меняйте и другую.

import (
	"fmt"
	"strings"

	"okuu/internal/civil"
)

// monthsKY — «октябрь айында».
var monthsKY = [...]string{"Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
	"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"}

func balanceKY(t int64) string {
	switch {
	case t < 0:
		return "Баланс: карыз " + Som(-t) + " сом"
	case t > 0:
		return "Баланс: аванс " + Som(t) + " сом"
	}
	return "Баланс: 0 сом, карыз жок"
}

var ky = &Texts{
	Invite: func(parent, center, botURL string) string {
		return fmt.Sprintf("Саламатсызбы, %s! «%s» борбору төлөм жана сабакка катышуу боюнча эскертмелерди Telegram аркылуу жөнөтөт. "+
			"Туташуу үчүн шилтемени ачып, «Баштоо» баскычын басыңыз: %s\nШилтеме 30 күн жарактуу.", parent, center, botURL)
	},

	InviteInvalid: "Шилтеме жараксыз же мөөнөтү өтүп кеткен. Борбордун администраторунан жаңы шилтеме сураңыз.",
	ConsentPrompt: func(center string, children []string) string {
		return fmt.Sprintf("«%s» борбору сизди Telegram аркылуу төлөм жана катышуу боюнча эскертмелерди алууга чакырат.\n"+
			"Балдар: %s.\n\n"+
			"«Макулмун» баскычын басуу менен сиз «%s» борборуна жана Окуу кызматына телефон номериңизди "+
			"жана балдарыңыздын маалыматтарын (аты-жөнү, катышуусу, төлөмдөрү) билдирүү жөнөтүү үчүн иштетүүгө уруксат бересиз. "+
			"Билдирүүлөрдү каалаган убакта /stop буйругу менен өчүрсөңүз болот.",
			center, strings.Join(children, ", "), center)
	},
	ConsentButton:        "Макулмун",
	ConsentAccepted:      "Рахмат, макулдугуңуз кабыл алынды.",
	ContactPrompt:        "Ата-эне экениңизди ырастоо үчүн төмөндөгү «Номерди жөнөтүү» баскычын басыңыз. Номер борбордо жазылган номер менен дал келиши керек.",
	ContactButton:        "📱 Номерди жөнөтүү",
	ContactNotOwn:        "Бул сиздин байланышыңыз эмес. «Номерди жөнөтүү» баскычын басыңыз — Telegram өз номериңизди жөнөтөт.",
	ContactBeforeConsent: "Адегенде жогорудагы билдирүүдөгү «Макулмун» баскычын басыңыз.",
	ContactNoInvite:      "Туташуу үчүн борборуңуздан келген чакыруу шилтемесин ачыңыз.",
	PhoneMismatch: func(center string) string {
		return fmt.Sprintf("Бул номер «%s» борборунда жазылган номерге дал келбейт. "+
			"Администратордон номерди текшерүүнү сураныңыз жана баскычты дагы бир жолу басыңыз — шилтеме жарактуу бойдон калат.", center)
	},
	Linked: func(center string, children []string) string {
		return fmt.Sprintf("Даяр! Сиз «%s» борборуна туташтыңыз. Бул жерге төлөм эскертмелери келет: %s.\n\n"+
			"Баланс жана катышуу — /balance\nТил — /lang\nБилдирүүлөрдү өчүрүү — /stop", center, strings.Join(children, ", "))
	},

	StartHint: "Саламатсызбы! Бул — ата-энелер үчүн Окуу боту. Туташуу үчүн борборуңуздун администратору жөнөткөн чакыруу шилтемесин ачыңыз.",
	Welcome: func(centers []string) string {
		return fmt.Sprintf("Сиз туташкансыз: %s.\n\nБаланс жана катышуу — /balance\nТил — /lang\nБилдирүүлөрдү өчүрүү — /stop", quoted(centers))
	},
	Resumed: func(centers []string) string {
		return fmt.Sprintf("Билдирүүлөр кайра күйгүзүлдү: %s.", quoted(centers))
	},
	Help: "Буйруктар:\n/balance — баланс, абонемент жана катышуу\n/lang — тил\n/stop — билдирүүлөрдү өчүрүү\n/help — жардам\n\n" +
		"Дагы бир борборго туташуу үчүн анын чакыруу шилтемесин ачыңыз.",
	Stopped: func(centers []string) string {
		return fmt.Sprintf("Билдирүүлөр өчүрүлдү: %s. Кайра күйгүзүү — /start.", quoted(centers))
	},
	NotLinked:     "Сиз али туташа элексиз. Борборуңуздан келген чакыруу шилтемесин ачыңыз.",
	LangChanged:   "Тил: кыргызча.",
	Unknown:       "Билдирүү түшүнүксүз. /help — буйруктардын тизмеси.",
	BalanceButton: "Баланс жана катышуу",

	BalanceCenter: func(center string) string { return "🏫 «" + center + "» борбору" },
	Balance:       balanceKY,
	Package: func(group string, left int32) string {
		if left <= 0 {
			return fmt.Sprintf("Абонемент (%s): сабактар бүттү — узартуу керек", group)
		}
		return fmt.Sprintf("Абонемент (%s): %d сабак калды", group, left)
	},
	Attendance: func(month int, present, absent, excused int32) string {
		return fmt.Sprintf("%s айындагы катышуу: келди %d, келген жок %d, жүйөлүү себеп менен %d",
			monthsKY[month-1], present, absent, excused)
	},
	NoChildren: "Борбордо окуучулар жок.",

	ReminderHeader: func(kind, center string, due civil.Date) string {
		switch kind {
		case KindDueSoon:
			return fmt.Sprintf("🏫 «%s» борбору: %s — төлөө мөөнөтү (3 күндөн кийин).", center, ddmm(due))
		case KindDueToday:
			return fmt.Sprintf("🏫 «%s» борбору: бүгүн төлөө мөөнөтү.", center)
		case KindOverdue3, KindOverdue7:
			return fmt.Sprintf("🏫 «%s» борбору: төлөм мөөнөтү өтүп кетти.", center)
		}
		return fmt.Sprintf("🏫 «%s» борбору: төлөм тууралуу эскертме.", center)
	},
	ReminderChild: func(c ChildDebt) string {
		s := "• " + c.Name + " — " + Som(c.Debt) + " сом"
		switch {
		case c.DaysOverdue > 0:
			s += fmt.Sprintf(", %d күн кечикти", c.DaysOverdue)
		case c.Due != nil:
			s += ", " + ddmm(*c.Due) + " чейин төлөө керек"
		}
		return s
	},
	ReminderTotal:  func(total int64) string { return "Жалпы: " + Som(total) + " сом" },
	PayDetails:     func(details string) string { return "Төлөм реквизиттери:\n" + details },
	ReminderFooter: "Эгер төлөп койгон болсоңуз, көңүл бурбаңыз: администратор төлөмдү белгилейт, бот ырастоо жөнөтөт.",

	Paid: func(center, child string, amount, balance int64) string {
		return fmt.Sprintf("🏫 «%s» борбору: рахмат, төлөм алынды — %s сом (%s).\n%s", center, Som(amount), child, balanceKY(balance))
	},
	PackageLow: func(center, child, group string) string {
		return fmt.Sprintf("🏫 «%s» борбору: %s — абонементте (%s) 1 сабак калды. Абонементти узартуу убагы келди.", center, child, group)
	},
	Absent: func(center, child, group string, date civil.Date, start string) string {
		return fmt.Sprintf("🏫 «%s» борбору: %s %s саат %s «%s» сабагына келген жок.", center, child, ddmm(date), start, group)
	},
}
