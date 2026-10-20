package notify

import (
	"fmt"
	"strings"
)

// monthsRU — «в октябре».
var monthsRU = [...]string{"январе", "феврале", "марте", "апреле", "мае", "июне",
	"июле", "августе", "сентябре", "октябре", "ноябре", "декабре"}

var ru = &Texts{
	Invite: func(parent, center, botURL string) string {
		return fmt.Sprintf("Здравствуйте, %s! Центр «%s» присылает напоминания об оплате и посещениях в Telegram. "+
			"Чтобы подключиться, откройте ссылку и нажмите «Запустить»: %s\nСсылка действует 30 дней.", parent, center, botURL)
	},

	InviteInvalid: "Ссылка недействительна или устарела. Попросите администратора центра прислать новую.",
	ConsentPrompt: func(center string, children []string) string {
		return fmt.Sprintf("Центр «%s» приглашает вас получать в Telegram напоминания об оплате и посещениях.\n"+
			"Дети: %s.\n\n"+
			"Нажимая «Согласен», вы разрешаете центру «%s» и сервису Окуу обрабатывать ваш номер телефона "+
			"и данные детей (имя, посещения, оплаты), чтобы присылать вам уведомления. "+
			"Отключить уведомления можно в любой момент командой /stop.",
			center, strings.Join(children, ", "), center)
	},
	ConsentButton:        "Согласен",
	ConsentAccepted:      "Спасибо, согласие получено.",
	ContactPrompt:        "Чтобы подтвердить, что вы родитель, нажмите кнопку «Поделиться номером» ниже. Номер должен совпадать с тем, что записан в центре.",
	ContactButton:        "📱 Поделиться номером",
	ContactNotOwn:        "Это не ваш контакт. Нажмите кнопку «Поделиться номером» — Telegram отправит ваш собственный номер.",
	ContactBeforeConsent: "Сначала нажмите «Согласен» в сообщении выше.",
	ContactNoInvite:      "Чтобы подключиться, откройте ссылку-приглашение от вашего центра.",
	PhoneMismatch: func(center string) string {
		return fmt.Sprintf("Этот номер не совпадает с номером, который записан в центре «%s». "+
			"Попросите администратора проверить номер и нажмите кнопку ещё раз — ссылка остаётся действительной.", center)
	},
	Linked: func(center string, children []string) string {
		return fmt.Sprintf("Готово! Вы подключены к центру «%s». Сюда будут приходить напоминания об оплате: %s.\n\n"+
			"Баланс и посещения — /balance\nЯзык — /lang\nОтключить уведомления — /stop", center, strings.Join(children, ", "))
	},

	StartHint: "Здравствуйте! Это бот Окуу для родителей. Чтобы подключиться, откройте ссылку-приглашение, которую прислал администратор вашего центра.",
	Welcome: func(centers []string) string {
		return fmt.Sprintf("Вы подключены: %s.\n\nБаланс и посещения — /balance\nЯзык — /lang\nОтключить уведомления — /stop", quoted(centers))
	},
	Resumed: func(centers []string) string {
		return fmt.Sprintf("Уведомления снова включены: %s.", quoted(centers))
	},
	Help: "Команды:\n/balance — баланс, абонемент и посещения\n/lang — язык\n/stop — отключить уведомления\n/help — помощь\n\n" +
		"Подключить ещё один центр — откройте его ссылку-приглашение.",
	Stopped: func(centers []string) string {
		return fmt.Sprintf("Уведомления отключены: %s. Включить снова — /start.", quoted(centers))
	},
	NotLinked:     "Вы ещё не подключены. Откройте ссылку-приглашение от вашего центра.",
	LangChanged:   "Язык: русский.",
	Unknown:       "Не понял сообщение. /help — список команд.",
	BalanceButton: "Баланс и посещения",

	BalanceCenter: func(center string) string { return "🏫 Центр «" + center + "»" },
	Balance: func(t int64) string {
		switch {
		case t < 0:
			return "Баланс: долг " + Som(-t) + " сом"
		case t > 0:
			return "Баланс: аванс " + Som(t) + " сом"
		}
		return "Баланс: 0 сом, долгов нет"
	},
	Package: func(group string, left int32) string {
		if left <= 0 {
			return fmt.Sprintf("Абонемент (%s): занятия закончились — пора продлить", group)
		}
		return fmt.Sprintf("Абонемент (%s): осталось %d %s", group, left, pluralRU(int64(left), "занятие", "занятия", "занятий"))
	},
	Attendance: func(month int, present, absent, excused int32) string {
		return fmt.Sprintf("Посещения в %s: был %d, не был %d, по уважительной %d", monthsRU[month-1], present, absent, excused)
	},
	NoChildren: "Учеников в центре нет.",
}
