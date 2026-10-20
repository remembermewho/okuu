package billing

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"okuu/internal/civil"
)

// R13–R15: баланс, FIFO-покрытие, дни просрочки, должник.
func TestComputeDebt(t *testing.T) {
	today := civil.New(2026, 10, 20)
	d := func(month time.Month, day int) civil.Date { return civil.New(2026, month, day) }
	at := func(day int) time.Time { return time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC) }
	charge := func(amount int64, due civil.Date, createdDay int) Op {
		return Op{ID: uuid.New(), Kind: KindCharge, Amount: -amount, DueDate: &due, CreatedAt: at(createdDay), CreatedOn: d(10, createdDay)}
	}
	pay := func(amount int64, day int) Op {
		return Op{ID: uuid.New(), Kind: KindPayment, Amount: amount, CreatedAt: at(day), CreatedOn: d(10, day)}
	}
	refund := func(amount int64, day int) Op {
		return Op{ID: uuid.New(), Kind: KindRefund, Amount: -amount, CreatedAt: at(day), CreatedOn: d(10, day)}
	}
	correction := func(amount int64, day int) Op {
		return Op{ID: uuid.New(), Kind: KindCorrection, Amount: amount, CreatedAt: at(day), CreatedOn: d(10, day)}
	}
	ptrDate := func(v civil.Date) *civil.Date { return &v }

	tests := []struct {
		name string
		ops  []Op
		want Debt
	}{
		{"нет операций", nil, Debt{}},
		{"начисление, срок не наступил", []Op{charge(300, d(10, 25), 1)},
			Debt{Balance: -300, Debt: 300, OldestDue: ptrDate(d(10, 25))}},
		{"начисление, срок сегодня — ещё не просрочка", []Op{charge(300, today, 1)},
			Debt{Balance: -300, Debt: 300, OldestDue: ptrDate(today)}},
		{"просрочено на 14 дней", []Op{charge(300, d(10, 6), 1)},
			Debt{Balance: -300, Debt: 300, OldestDue: ptrDate(d(10, 6)), DaysOverdue: 14, IsDebtor: true}},
		{"оплачено полностью", []Op{charge(300, d(10, 6), 1), pay(300, 3)},
			Debt{Balance: 0}},
		{"частичная оплата — долг остаётся, срок тот же", []Op{charge(300, d(10, 6), 1), pay(100, 3)},
			Debt{Balance: -200, Debt: 200, OldestDue: ptrDate(d(10, 6)), DaysOverdue: 14, IsDebtor: true}},
		{"переплата — аванс, долга нет", []Op{charge(300, d(10, 6), 1), pay(500, 3)},
			Debt{Balance: 200}},
		{"аванс гасит следующее начисление", []Op{pay(500, 1), charge(300, d(9, 6), 1), charge(300, d(10, 6), 1)},
			// 500 покрывает сентябрь (300) и 200 из октября; непогашен октябрь.
			Debt{Balance: -100, Debt: 100, OldestDue: ptrDate(d(10, 6)), DaysOverdue: 14, IsDebtor: true}},
		{"FIFO: оплата гасит самое старое", []Op{charge(300, d(10, 6), 2), charge(300, d(9, 6), 1), pay(300, 10)},
			// оплата пошла на сентябрь — старейший непогашенный октябрь.
			Debt{Balance: -300, Debt: 300, OldestDue: ptrDate(d(10, 6)), DaysOverdue: 14, IsDebtor: true}},
		{"несколько начислений, старое не погашено", []Op{charge(300, d(8, 6), 1), charge(300, d(9, 6), 1), charge(300, d(10, 6), 1), pay(100, 15)},
			Debt{Balance: -800, Debt: 800, OldestDue: ptrDate(d(8, 6)), DaysOverdue: 75, IsDebtor: true}},
		{"одинаковый срок — по времени создания", []Op{charge(300, d(10, 6), 5), charge(200, d(10, 6), 1), pay(200, 7)},
			Debt{Balance: -300, Debt: 300, OldestDue: ptrDate(d(10, 6)), DaysOverdue: 14, IsDebtor: true}},
		{"возврат — списание со сроком в день возврата", []Op{pay(300, 1), charge(300, d(10, 6), 1), refund(300, 18)},
			Debt{Balance: -300, Debt: 300, OldestDue: ptrDate(d(10, 18)), DaysOverdue: 2, IsDebtor: true}},
		{"возврат сегодня — не просрочка", []Op{pay(100, 1), refund(150, 20)},
			Debt{Balance: -50, Debt: 50, OldestDue: ptrDate(today)}},
		{"положительная поправка гасит долг", []Op{charge(300, d(10, 6), 1), correction(300, 10)},
			Debt{Balance: 0}},
		{"отрицательная поправка — долг со дня поправки", []Op{correction(-500, 12)},
			Debt{Balance: -500, Debt: 500, OldestDue: ptrDate(d(10, 12)), DaysOverdue: 8, IsDebtor: true}},
		{"отменённое начисление не передаётся — как будто его нет", []Op{pay(100, 1)},
			Debt{Balance: 100}},
		{"нулевое начисление (скидка 100%) не долг", []Op{charge(0, d(10, 1), 1)},
			Debt{Balance: 0}},
		{"начисление без срока — срок = дата создания", []Op{{ID: uuid.New(), Kind: KindCharge, Amount: -300, CreatedAt: at(5), CreatedOn: d(10, 5)}},
			Debt{Balance: -300, Debt: 300, OldestDue: ptrDate(d(10, 5)), DaysOverdue: 15, IsDebtor: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// UnpaidDebits согласован с ComputeDebt: первое непогашенное — OldestDue, сумма остатков — долг.
			unpaid := UnpaidDebits(tt.ops)
			var rest int64
			for _, u := range unpaid {
				rest += u.Amount
			}
			if tt.want.Balance < 0 && (len(unpaid) == 0 || unpaid[0].Due != *tt.want.OldestDue || rest != tt.want.Debt) {
				t.Errorf("UnpaidDebits = %+v, want первое со сроком %v и сумму %d", unpaid, tt.want.OldestDue, tt.want.Debt)
			}
			got := ComputeDebt(tt.ops, today)
			sameDue := (got.OldestDue == nil) == (tt.want.OldestDue == nil) &&
				(got.OldestDue == nil || *got.OldestDue == *tt.want.OldestDue)
			if got.Balance != tt.want.Balance || got.Debt != tt.want.Debt || !sameDue ||
				got.DaysOverdue != tt.want.DaysOverdue || got.IsDebtor != tt.want.IsDebtor {
				t.Fatalf("ComputeDebt =\n %+v (oldest %v)\nwant\n %+v (oldest %v)", got, got.OldestDue, tt.want, tt.want.OldestDue)
			}
		})
	}
}

// Частичная оплата гасит самое старое начисление; остаток у следующих — полностью.
func TestUnpaidDebits(t *testing.T) {
	d := func(day int) civil.Date { return civil.New(2026, 10, day) }
	at := func(day int) time.Time { return time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC) }
	charge := func(amount int64, due, created int) Op {
		dd := d(due)
		return Op{ID: uuid.New(), Kind: KindCharge, Amount: -amount, DueDate: &dd, CreatedAt: at(created), CreatedOn: d(created)}
	}
	ops := []Op{
		charge(300, 15, 10), charge(200, 6, 1), // старое — со сроком 6-го, хоть и вторым в списке
		{ID: uuid.New(), Kind: KindPayment, Amount: 250, CreatedAt: at(12), CreatedOn: d(12)},
		{ID: uuid.New(), Kind: KindCorrection, Amount: -100, CreatedAt: at(9), CreatedOn: d(9)},
	}
	got := UnpaidDebits(ops)
	want := []Unpaid{{KindCorrection, d(9), 50}, {KindCharge, d(15), 300}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("UnpaidDebits = %+v, want %+v", got, want)
	}
	if len(UnpaidDebits([]Op{charge(100, 5, 1), {ID: uuid.New(), Kind: KindPayment, Amount: 150, CreatedAt: at(2), CreatedOn: d(2)}})) != 0 {
		t.Error("переплата: непогашенных быть не должно")
	}
}
