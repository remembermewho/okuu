package notify

import (
	"fmt"
	"strings"

	"okuu/internal/civil"
)

// monthsRU — «в октябре».
var monthsRU = [...]string{"январе", "феврале", "марте", "апреле", "мае", "июне",
	"июле", "августе", "сентябре", "октябре", "ноябре", "декабре"}

func balanceRU(t int64) string {
	switch {
	case t < 0:
		return "Баланс: долг " + Som(-t) + " сом"
	case t > 0:
		return "Баланс: аванс " + Som(t) + " сом"
	}
	return "Баланс: 0 сом, долгов нет"
}

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
	Balance:       balanceRU,
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

	ReminderHeader: func(kind, center string, due civil.Date) string {
		switch kind {
		case KindDueSoon:
			return fmt.Sprintf("🏫 Центр «%s»: %s — срок оплаты (через 3 дня).", center, ddmm(due))
		case KindDueToday:
			return fmt.Sprintf("🏫 Центр «%s»: сегодня срок оплаты.", center)
		case KindOverdue3, KindOverdue7:
			return fmt.Sprintf("🏫 Центр «%s»: оплата просрочена.", center)
		}
		return fmt.Sprintf("🏫 Центр «%s»: напоминание об оплате.", center)
	},
	ReminderChild: func(c ChildDebt) string {
		s := "• " + c.Name + " — " + Som(c.Debt) + " сом"
		switch {
		case c.DaysOverdue > 0:
			s += fmt.Sprintf(", просрочено на %d %s", c.DaysOverdue, pluralRU(int64(c.DaysOverdue), "день", "дня", "дней"))
		case c.Due != nil:
			s += ", оплатить до " + ddmm(*c.Due)
		}
		return s
	},
	ReminderTotal:  func(total int64) string { return "Итого: " + Som(total) + " сом" },
	PayDetails:     func(details string) string { return "Реквизиты для оплаты:\n" + details },
	ReminderFooter: "Если вы уже оплатили, не обращайте внимания: администратор отметит оплату, и бот пришлёт подтверждение.",

	Paid: func(center, child string, amount, balance int64) string {
		return fmt.Sprintf("🏫 Центр «%s»: спасибо, оплата получена — %s сом (%s).\n%s", center, Som(amount), child, balanceRU(balance))
	},
	PackageLow: func(center, child, group string) string {
		return fmt.Sprintf("🏫 Центр «%s»: у ученика %s в абонементе (%s) осталось 1 занятие. Пора продлить абонемент.", center, child, group)
	},
	Absent: func(center, child, group string, date civil.Date, start string) string {
		return fmt.Sprintf("🏫 Центр «%s»: %s не был(а) на занятии «%s» %s в %s.", center, child, group, ddmm(date), start)
	},
}
