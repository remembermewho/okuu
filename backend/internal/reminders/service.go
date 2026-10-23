// Package reminders — напоминания и уведомления родителям в Telegram (R17–R19):
//   - reminders.auto (раз в час): due_soon, due_today, overdue_3, overdue_7 и (overdue_repeat = weekly)
//     overdue_weekly на 14, 21, 28… день по срокам непогашенных начислений; одно сообщение на родителя
//     со всеми детьми центра (R18), не больше одного в день (R17);
//   - событийные из notification_outbox (paid, package_low, absent) — диспетчер, сразу;
//   - ручные «Напомнить» — не чаще раза в час на ученика.
//
// Всё отправляется только 9:00–20:00 по поясу центра (ручные — в любое время: их шлёт сотрудник)
// и только родителям с tg_status=active. Каждая отправка — строка notifications (sent/failed/blocked).
package reminders

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/audit"
	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/notify"
	"okuu/internal/telegram"
	"okuu/internal/telegram/bot"
	"okuu/internal/validate"
)

// Окно отправки (R17): с 9:00 включительно до 20:00 по поясу центра.
const (
	WindowStart = 9
	WindowEnd   = 20
)

const (
	// ManualInterval — ручное напоминание об ученике не чаще раза в этот срок (R17).
	ManualInterval = time.Hour
	// MaxManualStudents — учеников в одном ручном запросе.
	MaxManualStudents = 500

	outboxBatch       = 100
	maxOutboxAttempts = 3
	maxErrorLen       = 500
)

// Причины, по которым ученик пропущен в ручном напоминании.
const (
	ReasonNoTelegram  = "no_telegram"  // родитель не подключён к боту
	ReasonStopped     = "stopped"      // родитель отключил уведомления (/stop)
	ReasonBlocked     = "blocked"      // бот заблокирован (403)
	ReasonRateLimited = "rate_limited" // напоминали меньше часа назад
	ReasonNoDebt      = "no_debt"      // долга нет
	ReasonSendFailed  = "send_failed"  // Telegram не принял сообщение
)

var (
	// ErrTelegramOff — бот выключен (нет TELEGRAM_BOT_TOKEN): отправлять нечем.
	ErrTelegramOff = errors.New("telegram-бот не запущен")
	// ErrNotFound — ученика из запроса нет в центре.
	ErrNotFound = errors.New("ученик не найден")
	// ErrDemoCenter — демо-центр: сообщения родителям (вымышленным) не отправляются.
	ErrDemoCenter = errors.New("демо-центр: сообщения не отправляются")
)

// Service — напоминания. tg == nil — Telegram выключен: автозадача и диспетчер ничего не делают.
type Service struct {
	pool   *pgxpool.Pool
	q      *sqlc.Queries
	tg     *telegram.Sender
	now    func() time.Time
	logger *slog.Logger
	langs  notify.Langs // языки сообщений (BOT_LANGUAGES); по умолчанию — только русский
}

// WithLangs — языки сообщений родителям (BOT_LANGUAGES). Без вызова — всё по-русски.
func (s *Service) WithLangs(l notify.Langs) *Service {
	s.langs = l
	return s
}

// NewService создаёт сервис; now — часы (nil — time.Now).
func NewService(pool *pgxpool.Pool, tg *telegram.Sender, now func() time.Time, logger *slog.Logger) *Service {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{pool: pool, q: sqlc.New(pool), tg: tg, now: now, logger: logger}
}

// InWindow — можно ли сейчас слать по поясу tz (9:00–20:00).
func InWindow(now time.Time, tz string) (bool, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return false, fmt.Errorf("часовой пояс %q: %w", tz, err)
	}
	h := now.In(loc).Hour()
	return h >= WindowStart && h < WindowEnd, nil
}

// AutoKind — вид автонапоминания сегодня по сроку непогашенного начисления ("" — не напоминаем):
// за 3 дня, в день срока, через 3 и 7 дней просрочки; weekly = true — и дальше раз в 7 дней
// (14, 21, 28… день просрочки), пока долг не оплачен; иначе после 7-го дня — только вручную.
func AutoKind(due, today civil.Date, weekly bool) string {
	switch days := today.DaysSince(due); {
	case days == -3:
		return notify.KindDueSoon
	case days == 0:
		return notify.KindDueToday
	case days == 3:
		return notify.KindOverdue3
	case days == 7:
		return notify.KindOverdue7
	case weekly && days > 7 && days%7 == 0:
		return notify.KindOverdueWeekly
	}
	return ""
}

// overdueWeek — номер недели просрочки (2 — 14-й день, 3 — 21-й …): часть ключа дедупликации повтора.
func overdueWeek(due, today civil.Date) int { return today.DaysSince(due) / 7 }

// priority — какой вид в заголовке, если у детей сработало несколько.
var priority = map[string]int{notify.KindDueSoon: 1, notify.KindDueToday: 2, notify.KindOverdue3: 3, notify.KindOverdue7: 4, notify.KindOverdueWeekly: 5}

// recipient — родитель, которому можно писать.
type recipient struct {
	parentID uuid.UUID
	chatID   int64
	lang     string
}

func recipientOf(parentID *uuid.UUID, status *string, chatID *int64, lang *string) (recipient, bool) {
	if parentID == nil || status == nil || *status != "active" || chatID == nil {
		return recipient{}, false
	}
	r := recipient{parentID: *parentID, chatID: *chatID, lang: notify.LangRU}
	if lang != nil {
		r.lang = *lang
	}
	return r, true
}

// studentDebt — долг ученика по FIFO и непогашенные списания.
type studentDebt struct {
	debt   billing.Debt
	unpaid []billing.Unpaid
}

func (s *Service) loadDebts(ctx context.Context, centerID uuid.UUID, tz string, today civil.Date, ids []uuid.UUID) (map[uuid.UUID]studentDebt, error) {
	out := map[uuid.UUID]studentDebt{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.q.ListActiveOperations(ctx, sqlc.ListActiveOperationsParams{CenterID: centerID, StudentIds: ids})
	if err != nil {
		return nil, err
	}
	ops := map[uuid.UUID][]billing.Op{}
	for _, r := range rows {
		on, err := civil.DateIn(r.CreatedAt, tz)
		if err != nil {
			return nil, err
		}
		ops[r.StudentID] = append(ops[r.StudentID], billing.Op{ID: r.ID, Kind: r.Kind, Amount: r.Amount, DueDate: r.DueDate, CreatedAt: r.CreatedAt, CreatedOn: on})
	}
	for _, id := range ids {
		out[id] = studentDebt{debt: billing.ComputeDebt(ops[id], today), unpaid: billing.UnpaidDebits(ops[id])}
	}
	return out, nil
}

func childDebt(name string, d billing.Debt) notify.ChildDebt {
	return notify.ChildDebt{Name: name, Debt: d.Debt, Due: d.OldestDue, DaysOverdue: d.DaysOverdue}
}

// payKids — кнопки «Оплатить» по детям сообщения (students и children идут парами).
func payKids(students []uuid.UUID, children []notify.ChildDebt) []bot.PayChild {
	out := make([]bot.PayChild, 0, len(students))
	for i, id := range students {
		out = append(out, bot.PayChild{StudentID: id, Name: children[i].Name, Debt: children[i].Debt})
	}
	return out
}

// payDetails — реквизиты в сообщении о долге, пока центр не подключил Finik.
func payDetails(details *string, finikConnected bool) string {
	if finikConnected || details == nil {
		return ""
	}
	return *details
}

// claimInput — запись notifications до отправки.
type claimInput struct {
	centerID uuid.UUID
	parentID uuid.UUID
	kind     string
	key      *string
	students []uuid.UUID
	actor    *uuid.UUID
}

// claim — строка notifications со статусом sent до отправки. ok=false — ключ уже занят:
// это сообщение уже ушло.
func (s *Service) claim(ctx context.Context, db sqlc.DBTX, in claimInput) (uuid.UUID, bool, error) {
	id, err := sqlc.New(db).ClaimNotification(ctx, sqlc.ClaimNotificationParams{
		CenterID: in.centerID, ParentID: in.parentID, Kind: in.kind, DedupKey: in.key,
		StudentIds: in.students, CreatedBy: in.actor, SentAt: s.now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}

// Итоги отправки.
const (
	outcomeSent    = "sent"
	outcomeBlocked = "blocked"
	outcomeFailed  = "failed"
)

// send отправляет уже записанное (claim) сообщение и уточняет его статус (R19). 403 — связь
// с родителем помечает blocked очередь отправки (OnBlocked). debt — напоминание о долге:
// обновляется parents.last_reminder_at.
func (s *Service) send(ctx context.Context, id uuid.UUID, r recipient, text string, kb telegram.ReplyMarkup, debt bool) (string, error) {
	_, sendErr := s.tg.Send(ctx, telegram.SendMessageParams{ChatID: r.chatID, Text: text, ReplyMarkup: kb})
	outcome := outcomeSent
	var errText *string
	if sendErr != nil {
		outcome = outcomeFailed
		if errors.Is(sendErr, telegram.ErrBlocked) {
			outcome = outcomeBlocked
		}
		msg := sendErr.Error()
		if len(msg) > maxErrorLen {
			msg = msg[:maxErrorLen]
		}
		errText = &msg
	}
	// Результат записываем, даже если ctx уже отменён: сообщение-то ушло (или нет).
	dbctx := context.WithoutCancel(ctx)
	if err := s.q.SetNotificationResult(dbctx, sqlc.SetNotificationResultParams{Status: outcome, Error: errText, ID: id}); err != nil {
		return outcome, err
	}
	if outcome == outcomeSent && debt {
		at := s.now()
		if err := s.q.TouchParentReminder(dbctx, sqlc.TouchParentReminderParams{RemindedAt: &at, ID: r.parentID}); err != nil {
			return outcome, err
		}
	}
	if outcome == outcomeFailed {
		s.logger.Warn("напоминание не отправлено", "notification_id", id, "err", sendErr)
	}
	return outcome, nil
}

// AutoResult — итог reminders.auto.
type AutoResult struct {
	Sent, Blocked, Failed int
	Limited               int // уже было напоминание сегодня (R17)
	Duplicate             int // это напоминание уже уходило (dedup_key)
}

func (r *AutoResult) add(o AutoResult) {
	r.Sent += o.Sent
	r.Blocked += o.Blocked
	r.Failed += o.Failed
	r.Limited += o.Limited
	r.Duplicate += o.Duplicate
}

// RunAuto — один проход reminders.auto по всем центрам с reminders_on. Идемпотентен:
// повторный запуск в тот же день ничего не шлёт повторно.
func (s *Service) RunAuto(ctx context.Context) (AutoResult, error) {
	var res AutoResult
	if s.tg == nil {
		return res, nil
	}
	centers, err := s.q.ListCentersForReminders(ctx)
	if err != nil {
		return res, err
	}
	now := s.now()
	var errs []error
	for _, c := range centers {
		r, err := s.autoCenter(ctx, c, now)
		res.add(r)
		if err != nil {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			s.logger.Error("reminders.auto: центр не обработан", "center_id", c.ID, "err", err)
			errs = append(errs, fmt.Errorf("центр %s: %w", c.ID, err))
		}
	}
	return res, errors.Join(errs...) // ошибка центра не мешает остальным, но видна задаче (алерт)
}

func (s *Service) autoCenter(ctx context.Context, c sqlc.ListCentersForRemindersRow, now time.Time) (AutoResult, error) {
	var res AutoResult
	if ok, err := InWindow(now, c.Timezone); err != nil || !ok {
		return res, err
	}
	today, err := civil.Today(now, c.Timezone)
	if err != nil {
		return res, err
	}
	rows, err := s.q.ListReminderStudents(ctx, sqlc.ListReminderStudentsParams{CenterID: c.ID, OnlyDebt: true})
	if err != nil {
		return res, err
	}

	type group struct {
		r        recipient
		children []notify.ChildDebt
		students []uuid.UUID
		kind     string
		due      civil.Date
	}
	var order []uuid.UUID
	groups := map[uuid.UUID]*group{}
	var ids []uuid.UUID
	for _, row := range rows {
		if _, ok := recipientOf(row.ParentID, row.ParentTgStatus, row.ParentChatID, row.ParentLang); ok {
			ids = append(ids, row.ID)
		}
	}
	debts, err := s.loadDebts(ctx, c.ID, c.Timezone, today, ids)
	if err != nil {
		return res, err
	}
	for _, row := range rows {
		r, ok := recipientOf(row.ParentID, row.ParentTgStatus, row.ParentChatID, row.ParentLang)
		if !ok {
			continue
		}
		g := groups[r.parentID]
		if g == nil {
			g = &group{r: r}
			groups[r.parentID] = g
			order = append(order, r.parentID)
		}
		d := debts[row.ID]
		g.children = append(g.children, childDebt(row.FullName, d.debt))
		g.students = append(g.students, row.ID)
		// Срабатывают сроки начислений; возвраты и поправки (в том числе начальный долг
		// из импорта) автоматически не напоминаются — только вручную.
		for _, u := range d.unpaid {
			if u.Kind != billing.KindCharge {
				continue
			}
			if k := AutoKind(u.Due, today, c.OverdueRepeat == "weekly"); k != "" && priority[k] > priority[g.kind] {
				g.kind, g.due = k, u.Due
			}
		}
	}

	since, err := civil.StartOfDay(today, c.Timezone)
	if err != nil {
		return res, err
	}
	details := payDetails(c.PayDetails, c.FinikConnected)
	for _, pid := range order {
		g := groups[pid]
		if g.kind == "" {
			continue
		}
		// R17: не больше одного напоминания о долге в день (ручное тоже считается).
		limited, err := s.q.DebtNotifiedSince(ctx, sqlc.DebtNotifiedSinceParams{ParentID: pid, Since: since})
		if err != nil {
			return res, err
		}
		if limited {
			res.Limited++
			continue
		}
		key := fmt.Sprintf("%s:%s:%s", g.kind, pid, g.due)
		if g.kind == notify.KindOverdueWeekly {
			key += fmt.Sprintf(":w%d", overdueWeek(g.due, today)) // повтор каждую неделю — свой ключ
		}
		id, ok, err := s.claim(ctx, s.pool, claimInput{centerID: c.ID, parentID: pid, kind: g.kind, key: &key, students: g.students})
		if err != nil {
			return res, err
		}
		if !ok {
			res.Duplicate++
			continue
		}
		tx := s.langs.For(g.r.lang)
		text := notify.DebtReminder(tx, g.kind, c.Name, g.due, g.children, details)
		outcome, err := s.send(ctx, id, g.r, text, bot.DebtKeyboard(tx, payKids(g.students, g.children)), true)
		if err != nil {
			return res, err
		}
		res.count(outcome)
	}
	return res, nil
}

func (r *AutoResult) count(outcome string) {
	switch outcome {
	case outcomeSent:
		r.Sent++
	case outcomeBlocked:
		r.Blocked++
	default:
		r.Failed++
	}
}

// ManualInput — кого напомнить: конкретных учеников или всех должников центра.
type ManualInput struct {
	StudentIDs []uuid.UUID
	All        bool
}

// Skipped — ученик, о котором не напомнили, и почему.
type Skipped struct {
	StudentID uuid.UUID
	Reason    string
}

// ManualResult — итог ручного напоминания.
type ManualResult struct {
	Sent     int // учеников в отправленных сообщениях
	Messages int // сообщений (родителей)
	Skipped  []Skipped
}

func validateManual(in ManualInput) ([]uuid.UUID, error) {
	if in.All {
		if len(in.StudentIDs) > 0 {
			return nil, validate.Errorf("student_ids", "Укажите либо student_ids, либо all: true")
		}
		return nil, nil
	}
	if len(in.StudentIDs) == 0 {
		return nil, validate.Errorf("student_ids", "Укажите учеников (student_ids) или all: true")
	}
	if len(in.StudentIDs) > MaxManualStudents {
		return nil, validate.Errorf("student_ids", "Не больше %d учеников за раз", MaxManualStudents)
	}
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, id := range in.StudentIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// Manual — «Напомнить» (owner, admin): по выбранным ученикам или по всем должникам (all).
// Дети одного родителя — одним сообщением (R18). Об ученике — не чаще раза в час (R17).
func (s *Service) Manual(ctx context.Context, actor, centerID uuid.UUID, in ManualInput) (ManualResult, error) {
	res := ManualResult{Skipped: []Skipped{}}
	ids, err := validateManual(in)
	if err != nil {
		return res, err
	}
	c, err := s.q.GetCenter(ctx, centerID)
	if err != nil {
		return res, err
	}
	// Демо — первым: объясняет, почему не отправится, даже если бот и не запущен.
	if c.IsDemo {
		return res, ErrDemoCenter
	}
	if s.tg == nil {
		return res, ErrTelegramOff
	}
	now := s.now()
	today, err := civil.Today(now, c.Timezone)
	if err != nil {
		return res, err
	}
	rows, err := s.q.ListReminderStudents(ctx, sqlc.ListReminderStudentsParams{CenterID: centerID, Ids: ids, OnlyDebt: in.All})
	if err != nil {
		return res, err
	}
	if !in.All && len(rows) != len(ids) {
		return res, ErrNotFound
	}

	type candidate struct {
		row sqlc.ListReminderStudentsRow
		r   recipient
	}
	var cands []candidate
	var debtIDs []uuid.UUID
	for _, row := range rows {
		reason := ""
		r, ok := recipientOf(row.ParentID, row.ParentTgStatus, row.ParentChatID, row.ParentLang)
		switch {
		case row.Balance >= 0:
			reason = ReasonNoDebt
		case !ok && row.ParentTgStatus != nil && *row.ParentTgStatus == "stopped":
			reason = ReasonStopped
		case !ok && row.ParentTgStatus != nil && *row.ParentTgStatus == "blocked":
			reason = ReasonBlocked
		case !ok:
			reason = ReasonNoTelegram
		}
		if reason != "" {
			res.Skipped = append(res.Skipped, Skipped{StudentID: row.ID, Reason: reason})
			continue
		}
		cands = append(cands, candidate{row: row, r: r})
		debtIDs = append(debtIDs, row.ID)
	}
	debts, err := s.loadDebts(ctx, centerID, c.Timezone, today, debtIDs)
	if err != nil {
		return res, err
	}

	type group struct {
		r        recipient
		id       uuid.UUID
		children []notify.ChildDebt
		students []uuid.UUID
	}
	var groups []*group
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		// Два одновременных «Напомнить» не обойдут лимит: проверка и запись — под блокировкой центра.
		if err := q.LockCenterReminders(ctx, lockKey(centerID)); err != nil {
			return err
		}
		recent, err := q.RecentManualStudents(ctx, sqlc.RecentManualStudentsParams{CenterID: centerID, Since: now.Add(-ManualInterval), Ids: debtIDs})
		if err != nil {
			return err
		}
		limited := map[uuid.UUID]bool{}
		for _, id := range recent {
			limited[id] = true
		}
		byParent := map[uuid.UUID]*group{}
		for _, cd := range cands {
			if limited[cd.row.ID] {
				res.Skipped = append(res.Skipped, Skipped{StudentID: cd.row.ID, Reason: ReasonRateLimited})
				continue
			}
			g := byParent[cd.r.parentID]
			if g == nil {
				g = &group{r: cd.r}
				byParent[cd.r.parentID] = g
				groups = append(groups, g)
			}
			g.children = append(g.children, childDebt(cd.row.FullName, debts[cd.row.ID].debt))
			g.students = append(g.students, cd.row.ID)
		}
		for _, g := range groups {
			id, _, err := s.claim(ctx, tx, claimInput{centerID: centerID, parentID: g.r.parentID, kind: notify.KindManual,
				students: g.students, actor: &actor})
			if err != nil {
				return err
			}
			g.id = id
		}
		return nil
	})
	if err != nil {
		return res, err
	}

	details := payDetails(c.PayDetails, len(c.FinikCreds) > 0)
	var sentIDs []uuid.UUID
	for _, g := range groups {
		tx := s.langs.For(g.r.lang)
		text := notify.DebtReminder(tx, notify.KindManual, c.Name, today, g.children, details)
		outcome, err := s.send(ctx, g.id, g.r, text, bot.DebtKeyboard(tx, payKids(g.students, g.children)), true)
		if err != nil {
			return res, err
		}
		switch outcome {
		case outcomeSent:
			res.Sent += len(g.students)
			res.Messages++
			sentIDs = append(sentIDs, g.students...)
		default:
			reason := ReasonSendFailed
			if outcome == outcomeBlocked {
				reason = ReasonBlocked
			}
			for _, id := range g.students {
				res.Skipped = append(res.Skipped, Skipped{StudentID: id, Reason: reason})
			}
		}
	}
	err = audit.Record(context.WithoutCancel(ctx), s.pool, audit.Entry{
		CenterID: centerID, UserID: &actor, Action: audit.ReminderManual, Entity: "reminder",
		After: map[string]any{"all": in.All, "sent": sentIDs, "messages": res.Messages, "skipped": len(res.Skipped)},
	})
	return res, err
}

// lockKey — ключ advisory lock ручных напоминаний центра.
func lockKey(centerID uuid.UUID) int64 {
	return int64(binary.BigEndian.Uint64(centerID[:8]))
}

// Dispatch — отправить событийные уведомления из notification_outbox. Вне окна 9–20 события
// ждут утра; неактуальные (оплату отменили, отметку исправили, родитель отключился) и старше
// 3 дней удаляются без отправки. Возвращает число отправленных.
func (s *Service) Dispatch(ctx context.Context) (int, error) {
	if s.tg == nil {
		return 0, nil
	}
	rows, err := s.q.ListOutbox(ctx, outboxBatch)
	if err != nil {
		return 0, err
	}
	now := s.now()
	sent := 0
	for _, o := range rows {
		if o.Expired || o.Silent { // демо, заявке и заблокированному центру уведомления не шлются
			if err := s.q.DeleteOutbox(ctx, o.ID); err != nil {
				return sent, err
			}
			continue
		}
		if ok, err := InWindow(now, o.Timezone); err != nil || !ok {
			if err != nil {
				s.logger.Error("уведомления: часовой пояс центра", "center_id", o.CenterID, "err", err)
			}
			continue
		}
		r, text, kb, ok, err := s.eventMessage(ctx, o)
		if err != nil {
			return sent, err
		}
		if !ok {
			if err := s.q.DeleteOutbox(ctx, o.ID); err != nil {
				return sent, err
			}
			continue
		}
		key := o.DedupKey
		id, claimed, err := s.claim(ctx, s.pool, claimInput{centerID: o.CenterID, parentID: r.parentID, kind: o.Kind, key: &key,
			students: []uuid.UUID{o.StudentID}})
		if err != nil {
			return sent, err
		}
		if !claimed { // уже отправлено (например, отметку «не был» поставили повторно)
			if err := s.q.DeleteOutbox(ctx, o.ID); err != nil {
				return sent, err
			}
			continue
		}
		outcome, err := s.send(ctx, id, r, text, kb, false)
		if err != nil {
			return sent, err
		}
		if outcome == outcomeSent {
			sent++
		}
		if outcome == outcomeFailed {
			attempts, err := s.q.BumpOutboxAttempts(ctx, o.ID)
			if err != nil {
				return sent, err
			}
			if attempts < maxOutboxAttempts {
				continue // попробуем на следующем проходе
			}
		}
		if err := s.q.DeleteOutbox(ctx, o.ID); err != nil {
			return sent, err
		}
	}
	return sent, nil
}

// eventMessage — получатель и текст события; ok=false — отправлять не нужно.
func (s *Service) eventMessage(ctx context.Context, o sqlc.ListOutboxRow) (recipient, string, telegram.ReplyMarkup, bool, error) {
	switch o.Kind {
	case notify.KindPaid:
		ev, err := s.q.GetPaidEvent(ctx, sqlc.GetPaidEventParams{CenterID: o.CenterID, ID: o.RefID})
		if errors.Is(err, pgx.ErrNoRows) {
			return recipient{}, "", nil, false, nil // оплату отменили
		}
		if err != nil {
			return recipient{}, "", nil, false, err
		}
		r, ok := recipientOf(ev.ParentID, &ev.ParentTgStatus, ev.ParentChatID, &ev.ParentLang)
		tx := s.langs.For(r.lang)
		return r, tx.Paid(o.CenterName, ev.FullName, ev.Amount, ev.Balance), bot.BalanceKeyboard(tx), ok, nil

	case notify.KindPackageLow:
		if !o.RemindersOn {
			return recipient{}, "", nil, false, nil
		}
		ev, err := s.q.GetPackageLowEvent(ctx, sqlc.GetPackageLowEventParams{CenterID: o.CenterID, ID: o.RefID})
		if errors.Is(err, pgx.ErrNoRows) {
			return recipient{}, "", nil, false, nil
		}
		if err != nil {
			return recipient{}, "", nil, false, err
		}
		if ev.LessonsLeft == nil || *ev.LessonsLeft != 1 {
			return recipient{}, "", nil, false, nil // продлили или отметку исправили
		}
		r, ok := recipientOf(ev.ParentID, &ev.ParentTgStatus, ev.ParentChatID, &ev.ParentLang)
		return r, s.langs.For(r.lang).PackageLow(o.CenterName, ev.FullName, ev.GroupName), nil, ok, nil

	case notify.KindAbsent:
		if !o.NotifyAbsent {
			return recipient{}, "", nil, false, nil
		}
		ev, err := s.q.GetAbsentEvent(ctx, sqlc.GetAbsentEventParams{CenterID: o.CenterID, LessonID: o.RefID, StudentID: o.StudentID})
		if errors.Is(err, pgx.ErrNoRows) {
			return recipient{}, "", nil, false, nil // отметку исправили или занятие отменили
		}
		if err != nil {
			return recipient{}, "", nil, false, err
		}
		r, ok := recipientOf(ev.ParentID, &ev.ParentTgStatus, ev.ParentChatID, &ev.ParentLang)
		return r, s.langs.For(r.lang).Absent(o.CenterName, ev.FullName, ev.GroupName, ev.Date, ev.StartTime.String()), nil, ok, nil
	}
	s.logger.Warn("уведомления: неизвестный вид события", "kind", o.Kind)
	return recipient{}, "", nil, false, nil
}
