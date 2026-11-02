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

// ruCommands — подсказка по командам; withLang — есть ли /lang (BOT_LANGUAGES с кыргызским).
func ruCommands(withLang bool) string {
	s := "Баланс — /balance\nПосещения — /attendance\nКак оплатить — кнопка «💳 Как оплатить» внизу\n"
	if withLang {
		s += "Язык — /lang\n"
	}
	return s + "Отключить уведомления — /stop"
}

func ruLinked(withLang bool) func(center string, children []string) string {
	return func(center string, children []string) string {
		return fmt.Sprintf("Готово! Вы подключены к центру «%s». Сюда будут приходить напоминания об оплате: %s.\n\n"+
			"Кнопки внизу — баланс, как оплатить и посещения.\n%s", center, strings.Join(children, ", "), ruCommands(withLang))
	}
}

func ruWelcome(withLang bool) func(centers []string) string {
	return func(centers []string) string {
		return fmt.Sprintf("Вы подключены: %s.\n\n%s", quoted(centers), ruCommands(withLang))
	}
}

func ruHelp(withLang bool) string {
	s := "Команды:\n/balance — баланс и абонемент\n/attendance — посещения за месяц\n"
	if withLang {
		s += "/lang — язык\n"
	}
	return s + "/stop — отключить уведомления\n/start — включить снова\n/help — помощь\n\n" +
		"«💳 Как оплатить» — реквизиты центра и куда отправить чек.\n" +
		"Подключить ещё один центр — откройте его ссылку-приглашение."
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
	Linked: ruLinked(true),

	StartHint: "Здравствуйте! Это бот Окуу для родителей. Чтобы подключиться, попросите администратора вашего центра " +
		"прислать ссылку-приглашение (в WhatsApp или СМС) и откройте её — бот подключится сам.",
	Welcome: ruWelcome(true),
	Resumed: func(centers []string) string {
		return fmt.Sprintf("Уведомления снова включены: %s.", quoted(centers))
	},
	Help: ruHelp(true),
	Stopped: func(centers []string) string {
		return fmt.Sprintf("Уведомления отключены: %s. Включить снова — /start.", quoted(centers))
	},
	NotLinked: "Вы ещё не подключены. Попросите администратора вашего центра прислать ссылку-приглашение " +
		"(в WhatsApp или СМС) и откройте её — бот подключится сам.",
	LangChanged:   "Язык: русский.",
	Unknown:       "Не понял сообщение. Выберите, что показать, — кнопки внизу, или /help.",
	BalanceButton: "Баланс и посещения",

	MenuBalance:    "💰 Баланс",
	MenuHowToPay:   "💳 Как оплатить",
	MenuAttendance: "📅 Посещения",
	MenuHelp:       "❓ Помощь",
	MenuPrompt:     "Что показать?",
	MenuBack:       "« Меню",
	ChooseChild:    "Выберите ребёнка:",
	AllChildren:    "Все дети",
	AttendanceHeader: func(month int) string {
		return "Посещения в " + monthsRU[month-1] + ":"
	},
	AttendanceChild: func(child, center string, present, absent, excused int32) string {
		return fmt.Sprintf("👤 %s (%s)\nбыл %d, не был %d, по уважительной %d", child, center, present, absent, excused)
	},
	AttendanceMark: func(date civil.Date, status, group string) string {
		label := map[string]string{MarkPresent: "был", MarkAbsent: "не был", MarkExcused: "по уважительной"}[status]
		if group != "" {
			label += " (" + group + ")"
		}
		return ddmm(date) + " — " + label
	},

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
		case KindOverdue3, KindOverdue7, KindOverdueWeekly:
			return fmt.Sprintf("🏫 Центр «%s»: оплата просрочена.", center)
		}
		return fmt.Sprintf("🏫 Центр «%s»: напоминание об оплате.", center)
	},
	ReminderChild: func(c ChildDebt) string {
		s := "• " + c.Name + " — " + Som(c.Debt) + " сом"
		switch {
		case c.DaysOverdue > 0:
			s += fmt.Sprintf(", просрочено на %d %s", c.DaysOverdue, pluralRU(int64(c.DaysOverdue), "день", "дня", "дней"))
		case c.DueToday:
			s += ", срок сегодня"
		case c.Due != nil:
			s += ", срок до " + ddmm(*c.Due)
		}
		return s
	},
	ReminderTotal:  func(total int64) string { return "Итого: " + Som(total) + " сом" },
	ReminderFooter: "Если вы уже оплатили, не обращайте внимания: администратор отметит оплату, и бот пришлёт подтверждение.",

	HowToPayHeader:    "Как оплатить:",
	PayDetailsMissing: "Уточните способ оплаты у администратора центра.",

	Paid: func(center, child string, amount, balance int64, others []ChildDebt) string {
		head := fmt.Sprintf("Оплата получена: %s сом — %s. Спасибо!", Som(amount), child)
		bal := balanceRU(balance)
		if balance == 0 && len(others) > 0 {
			bal = "Баланс: 0 сом" // «долгов нет» — только если не должен никто из детей
		}
		return paidMessage("🏫 Центр «"+center+"»", head, bal, "Ещё к оплате", "Итого к оплате", balance, others)
	},
	PackageLow: func(center, child, group string) string {
		return fmt.Sprintf("🏫 Центр «%s»: %s — в абонементе (%s) осталось 1 занятие. Пора продлить абонемент.", center, child, group)
	},
	Absent: func(center, child, group string, date civil.Date, start string) string {
		return fmt.Sprintf("🏫 Центр «%s»: %s — не был(а) на занятии «%s» %s в %s.", center, child, group, ddmm(date), start)
	},
}
