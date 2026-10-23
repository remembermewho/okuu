package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"okuu/internal/notify"
)

// Демо-центр (is_demo) молчит: автонапоминания, событийные уведомления, «Напомнить» и бот его пропускают,
// приглашение родителя не создаётся. Даже если у демо-родителя каким-то образом есть chat_id.
func TestDemoCenterSilent(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)

	// Обычный центр — контроль: ему напоминание уходит.
	normal := newCatalogOn(t, env.api, "+996707900001")
	normal.patchSettings(t, map[string]any{"due_days": 3})
	rs := normal.debtor(t, "Обычный Должник", "0707 900 101")
	const realChat = 7900101
	linkDirect(t, env.api, rs.Parent.ID, realChat)

	demo := newCatalogOn(t, env.api, "+996707900002")
	demo.patchSettings(t, map[string]any{"due_days": 3})
	ds := demo.debtor(t, "Демо Должник", "0707 900 201")
	const demoChat = 7900201
	linkDirect(t, env.api, ds.Parent.ID, demoChat)
	_, staleToken := inviteParent(t, demo, ds.Parent.ID) // выдано до того, как центр стал демо
	other := demo.student(t, "Демо Без Бота", "0707 900 202", nil)
	if _, err := env.api.pool.Exec(context.Background(), `UPDATE centers SET is_demo = true WHERE id = $1`, demo.center); err != nil {
		t.Fatal(err)
	}

	if res := env.runAuto(t); res.Sent != 1 {
		t.Errorf("автонапоминания: %+v, want одно — обычному центру", res)
	}
	if len(env.tg.Messages(realChat)) != 1 {
		t.Error("обычному центру напоминание не ушло")
	}

	// Оплата в демо — «спасибо» не уходит, событие из очереди убирается.
	demo.mustOp(t, ds.ID, pay(100_000))
	if n := env.dispatch(t); n != 0 {
		t.Errorf("dispatch: отправлено %d", n)
	}
	if n := outboxCount(t, env.api); n != 0 {
		t.Errorf("в очереди осталось %d событий демо", n)
	}

	assertError(t, remind(demo, map[string]any{"all": true}), http.StatusConflict, "demo_center")
	assertError(t, demo.owner.do(http.MethodPost, centerPath(demo.center, "/parents/"+other.Parent.ID.String()+"/invite"), nil),
		http.StatusConflict, "demo_center")

	// Бот: чат демо-родителя как будто не подключён; старое приглашение недействительно.
	u := env.user(t, demoChat)
	tx := notify.For(notify.LangRU)
	for _, cmd := range []string{"/balance", "/attendance"} {
		if m := one(t, u.send(cmd)); m.Text != tx.NotLinked {
			t.Errorf("%s в демо: %q", cmd, m.Text)
		}
	}
	if m := one(t, env.user(t, 7900299).send("/start "+staleToken)); m.Text != tx.InviteInvalid {
		t.Errorf("приглашение демо: %q", m.Text)
	}
	if msgs := env.tg.Messages(demoChat); len(msgs) != 2 {
		t.Errorf("в чат демо-родителя: %d сообщений, want 2 (ответы на команды)", len(msgs))
	} else {
		for _, m := range msgs {
			if strings.Contains(m.Text, "Демо Должник") {
				t.Errorf("бот показал ученика демо: %q", m.Text)
			}
		}
	}
}
