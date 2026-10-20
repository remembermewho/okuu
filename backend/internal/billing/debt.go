package billing

import (
	"cmp"
	"slices"
	"time"

	"github.com/google/uuid"

	"okuu/internal/civil"
)

// Виды операций ledger.
const (
	KindCharge     = "charge"
	KindPayment    = "payment"
	KindRefund     = "refund"
	KindCorrection = "correction"
)

// Op — неотменённая операция ученика для расчёта долга. CreatedOn — дата создания
// в часовом поясе центра: срок возврата и отрицательной поправки.
type Op struct {
	ID        uuid.UUID
	Kind      string
	Amount    int64 // со знаком, как в ledger
	DueDate   *civil.Date
	CreatedAt time.Time
	CreatedOn civil.Date
}

// Debt — состояние ученика по R13–R14.
type Debt struct {
	Balance     int64       // сумма операций; < 0 — долг
	Debt        int64       // -Balance, если баланс отрицательный, иначе 0
	OldestDue   *civil.Date // срок самого старого непогашенного списания; nil — всё погашено
	DaysOverdue int         // сегодня − OldestDue, если > 0
	IsDebtor    bool        // Balance < 0 и срок прошёл
}

// ComputeDebt считает долг по FIFO (R14):
//   - списания (debits): начисления, возвраты и отрицательные поправки; срок у начисления — due_date,
//     у возврата и поправки — дата создания;
//   - зачисления (credits): оплаты и положительные поправки;
//   - зачисления гасят списания в порядке срока, затем времени создания; переплата — аванс,
//     она гасит следующие начисления (R13).
//
// Отменённые операции сюда не передаются (R15).
func ComputeDebt(ops []Op, today civil.Date) Debt {
	type debit struct {
		amount  int64
		due     civil.Date
		created time.Time
		id      uuid.UUID
	}
	var debits []debit
	var credits, balance int64
	for _, op := range ops {
		balance += op.Amount
		switch {
		case op.Amount > 0:
			credits += op.Amount
		case op.Amount < 0:
			due := op.CreatedOn
			if op.Kind == KindCharge && op.DueDate != nil {
				due = *op.DueDate
			}
			debits = append(debits, debit{amount: -op.Amount, due: due, created: op.CreatedAt, id: op.ID})
		}
	}

	d := Debt{Balance: balance}
	if balance >= 0 {
		return d
	}
	d.Debt = -balance

	slices.SortFunc(debits, func(a, b debit) int {
		if c := a.due.Compare(b.due); c != 0 {
			return c
		}
		if c := a.created.Compare(b.created); c != 0 {
			return c
		}
		return cmp.Compare(a.id.String(), b.id.String())
	})
	for _, db := range debits {
		if credits >= db.amount {
			credits -= db.amount
			continue
		}
		due := db.due
		d.OldestDue = &due
		if days := today.DaysSince(due); days > 0 {
			d.DaysOverdue = days
		}
		break
	}
	d.IsDebtor = d.Balance < 0 && d.DaysOverdue > 0
	return d
}
