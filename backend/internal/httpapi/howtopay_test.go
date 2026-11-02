package httpapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"okuu/internal/notify"
)

// «Как оплатить»: реквизиты и инструкция центра как есть (с переносами строк), блок на каждый центр чата;
// пустые реквизиты — «Уточните способ оплаты у администратора центра»; старые кнопки «Оплатить» — то же без ошибки.
func TestBotHowToPay(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	tx := notify.For("ru")
	details := "Оплата на MBank: 0700 123 456 (Айгуль А.).\nПосле оплаты отправьте чек администратору в WhatsApp: +996 700 123 456."

	a := newCatalogOn(t, env.api, "+996707970001")
	a.patchSettings(t, map[string]any{"pay_details": details})
	sa := a.debtor(t, "Асанов Бакыт", "0707 970 101")
	u := env.user(t, 7970101)
	linkDirect(t, env.api, sa.Parent.ID, u.id)

	// Один центр: заголовок центра, «Как оплатить:» и текст как есть.
	blockA := tx.BalanceCenter(a.centerName(t)) + "\n" + tx.HowToPayHeader + "\n" + details
	m := one(t, u.send("💳 Как оплатить"))
	if m.Text != blockA || fmt.Sprint(m.Buttons()) != "[« Меню|menu]" {
		t.Errorf("один центр:\n%s\n%v", m.Text, m.Buttons())
	}

	// Два центра: блок по каждому; у второго реквизитов нет.
	b := newCatalogOn(t, env.api, "+996707970002")
	sb := b.debtor(t, "Бекова Айжан", "0707 970 101")
	linkDirect(t, env.api, sb.Parent.ID, u.id)
	blockB := tx.BalanceCenter(b.centerName(t)) + "\n" + tx.HowToPayHeader + "\n" + tx.PayDetailsMissing
	m = one(t, u.send("💳 Как оплатить"))
	if !strings.Contains(m.Text, blockA) || !strings.Contains(m.Text, blockB) || strings.Count(m.Text, tx.HowToPayHeader) != 2 {
		t.Errorf("два центра:\n%s", m.Text)
	}

	// Старые кнопки из уже отправленных сообщений: «Оплатить: <ребёнок>» и «Оплатить» в «« Меню».
	for _, data := range []string{"pay:" + sa.ID.String(), "pay:" + uuid.NewString(), "pay:кривое", "paylist"} {
		if m := one(t, u.press(m, data)); m.Text != one(t, u.send("/pay")).Text {
			t.Errorf("старая кнопка %q: %q", data, m.Text)
		}
	}

	// Не подключённому — как подключиться, без чужих реквизитов.
	if m := one(t, env.user(t, 7970199).send("💳 Как оплатить")); m.Text != tx.NotLinked {
		t.Errorf("не подключён: %q", m.Text)
	}
	// Ссылок оплаты бот больше не создаёт.
	var links int
	if err := env.api.pool.QueryRow(context.Background(), `SELECT count(*) FROM payment_links`).Scan(&links); err != nil || links != 0 {
		t.Errorf("ссылок оплаты: %d %v", links, err)
	}
}
