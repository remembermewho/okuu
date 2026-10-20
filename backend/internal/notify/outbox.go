package notify

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
)

// Виды уведомлений (notifications.kind).
const (
	KindDueSoon    = "due_soon"    // за 3 дня до срока оплаты
	KindDueToday   = "due_today"   // в день срока, если не оплачено
	KindOverdue3   = "overdue_3"   // 3 дня просрочки
	KindOverdue7   = "overdue_7"   // 7 дней просрочки; дальше — только вручную
	KindManual     = "manual"      // кнопка «Напомнить»
	KindPaid       = "paid"        // после оплаты
	KindPackageLow = "package_low" // в абонементе осталось 1 занятие (R10)
	KindAbsent     = "absent"      // ребёнок отмечен «не был» (настройка центра notify_absent)
)

// Event — событийное уведомление: пишется в notification_outbox в транзакции самого события
// (оплата, отметка), отправляет его диспетчер напоминаний. Key — ключ дедупликации.
type Event struct {
	CenterID  uuid.UUID
	StudentID uuid.UUID
	Kind      string
	RefID     uuid.UUID // ledger.id | enrollment.id | lesson.id
	Key       string
}

// PaidEvent — «оплата получена» по операции ledger.
func PaidEvent(centerID, studentID, ledgerID uuid.UUID) Event {
	return Event{CenterID: centerID, StudentID: studentID, Kind: KindPaid, RefID: ledgerID, Key: "paid:" + ledgerID.String()}
}

// PackageLowEvent — осталось 1 занятие; не чаще раза в день по одной записи.
func PackageLowEvent(centerID, studentID, enrollmentID uuid.UUID, today civil.Date) Event {
	return Event{CenterID: centerID, StudentID: studentID, Kind: KindPackageLow, RefID: enrollmentID,
		Key: fmt.Sprintf("package_low:%s:%s", enrollmentID, today)}
}

// AbsentEvent — «не был» на занятии; одно сообщение на занятие и ученика.
func AbsentEvent(centerID, studentID, lessonID uuid.UUID) Event {
	return Event{CenterID: centerID, StudentID: studentID, Kind: KindAbsent, RefID: lessonID,
		Key: fmt.Sprintf("absent:%s:%s", lessonID, studentID)}
}

// Enqueue ставит событие в очередь в транзакции tx и будит диспетчер после COMMIT.
// Повтор ключа ничего не делает.
func Enqueue(ctx context.Context, tx sqlc.DBTX, e Event) error {
	q := sqlc.New(tx)
	if err := q.EnqueueOutbox(ctx, sqlc.EnqueueOutboxParams{
		CenterID: e.CenterID, Kind: e.Kind, DedupKey: e.Key, StudentID: e.StudentID, RefID: e.RefID,
	}); err != nil {
		return fmt.Errorf("уведомление %s: %w", e.Kind, err)
	}
	return q.NotifyOutbox(ctx)
}
