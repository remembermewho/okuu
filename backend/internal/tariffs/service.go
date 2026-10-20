// Package tariffs — тарифы обучения центра (F2): «в месяц» и «абонемент на N занятий».
// Удаления нет — только архив; архивный тариф нельзя назначить новой группе или ученику.
package tariffs

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/audit"
	"okuu/internal/db/sqlc"
	"okuu/internal/optional"
	"okuu/internal/validate"
)

const (
	KindMonthly = "monthly"
	KindPackage = "package"

	// MaxPrice — 10 000 000 сом в тыйынах.
	MaxPrice        = 1_000_000_000
	MinLessonsCount = 1
	MaxLessonsCount = 200
)

var (
	// ErrNotFound — тарифа нет в этом центре.
	ErrNotFound = errors.New("тариф не найден")
	// ErrInUse — на тариф есть записи учеников, менять kind нельзя.
	ErrInUse = errors.New("тариф используется")
)

// Tariff — тариф. Price — в тыйынах.
type Tariff struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	Price        int64     `json:"price"`
	LessonsCount *int32    `json:"lessons_count"`
	Archived     bool      `json:"archived"`
}

// Input — поля тарифа при создании.
type Input struct {
	Name         string
	Kind         string
	Price        int64
	LessonsCount *int32
}

// Patch — частичное изменение: непереданные поля не меняются.
type Patch struct {
	Name         optional.Field[string]
	Kind         optional.Field[string]
	Price        optional.Field[int64]
	LessonsCount optional.Field[int32]
}

// Service — тарифы.
type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// NewService создаёт сервис тарифов.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: sqlc.New(pool)}
}

// List — тарифы центра: активные (archived=false) или архивные.
func (s *Service) List(ctx context.Context, centerID uuid.UUID, archived bool) ([]Tariff, error) {
	rows, err := s.q.ListTariffs(ctx, sqlc.ListTariffsParams{CenterID: centerID, Archived: archived})
	if err != nil {
		return nil, err
	}
	out := make([]Tariff, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRow(r))
	}
	return out, nil
}

// Get — тариф центра.
func (s *Service) Get(ctx context.Context, centerID, id uuid.UUID) (Tariff, error) {
	row, err := s.q.GetTariff(ctx, sqlc.GetTariffParams{CenterID: centerID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Tariff{}, ErrNotFound
	}
	if err != nil {
		return Tariff{}, err
	}
	return fromRow(row), nil
}

// Create создаёт тариф.
func (s *Service) Create(ctx context.Context, actor, centerID uuid.UUID, in Input) (Tariff, error) {
	in, err := Validate(in)
	if err != nil {
		return Tariff{}, err
	}
	var t Tariff
	err = s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		row, err := q.CreateTariff(ctx, sqlc.CreateTariffParams{
			CenterID: centerID, Name: in.Name, Kind: in.Kind, Price: in.Price, LessonsCount: in.LessonsCount,
		})
		if err != nil {
			return err
		}
		t = fromRow(row)
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.TariffCreate, Entity: "tariff", EntityID: &t.ID, After: t,
		})
	})
	return t, err
}

// Update применяет частичное изменение. Смена kind запрещена, если на тариф есть записи
// учеников. Новая цена действует со следующего начисления (R6): прошлые начисления хранят свою цену.
func (s *Service) Update(ctx context.Context, actor, centerID, id uuid.UUID, p Patch) (Tariff, error) {
	var t Tariff
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		row, err := q.GetTariffForUpdate(ctx, sqlc.GetTariffForUpdateParams{CenterID: centerID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		before := fromRow(row)

		next := Input{Name: before.Name, Kind: before.Kind, Price: before.Price, LessonsCount: before.LessonsCount}
		if p.Name.Set {
			if p.Name.Value == nil {
				return validate.Errorf("name", "Название — обязательное поле")
			}
			next.Name = *p.Name.Value
		}
		if p.Kind.Set {
			if p.Kind.Value == nil {
				return validate.Errorf("kind", "Тип тарифа — monthly или package")
			}
			next.Kind = *p.Kind.Value
		}
		if p.Price.Set {
			if p.Price.Value == nil {
				return validate.Errorf("price", "Цена — обязательное поле")
			}
			next.Price = *p.Price.Value
		}
		next.LessonsCount = p.LessonsCount.Apply(next.LessonsCount)

		if next, err = Validate(next); err != nil {
			return err
		}
		if next.Kind != before.Kind {
			n, err := q.CountEnrollmentsByTariff(ctx, sqlc.CountEnrollmentsByTariffParams{CenterID: centerID, TariffID: id})
			if err != nil {
				return err
			}
			if n > 0 {
				return ErrInUse
			}
		}

		t = before
		t.Name, t.Kind, t.Price, t.LessonsCount = next.Name, next.Kind, next.Price, next.LessonsCount
		if sameTariff(before, t) {
			return nil
		}
		row, err = q.UpdateTariff(ctx, sqlc.UpdateTariffParams{
			CenterID: centerID, ID: id, Name: t.Name, Kind: t.Kind, Price: t.Price, LessonsCount: t.LessonsCount,
		})
		if err != nil {
			return err
		}
		t = fromRow(row)
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.TariffUpdate, Entity: "tariff", EntityID: &id,
			Before: before, After: t,
		})
	})
	return t, err
}

// Archive переводит тариф в архив. Повторный вызов — без изменений и без записи в журнал.
// Записи учеников на этом тарифе продолжают работать: архив запрещает только новые назначения.
func (s *Service) Archive(ctx context.Context, actor, centerID, id uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		row, err := q.GetTariffForUpdate(ctx, sqlc.GetTariffForUpdateParams{CenterID: centerID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if row.Archived {
			return nil
		}
		if err := q.ArchiveTariff(ctx, sqlc.ArchiveTariffParams{CenterID: centerID, ID: id}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.TariffArchive, Entity: "tariff", EntityID: &id,
			Before: map[string]any{"archived": false}, After: map[string]any{"archived": true},
		})
	})
}

// Validate проверяет тариф и возвращает его с обрезанным названием.
func Validate(in Input) (Input, error) {
	name, err := validate.Name("name", "Название", in.Name)
	if err != nil {
		return in, err
	}
	in.Name = name

	switch in.Kind {
	case KindMonthly:
		if in.LessonsCount != nil {
			return in, validate.Errorf("lessons_count", "У месячного тарифа нет числа занятий — передайте null")
		}
	case KindPackage:
		if in.LessonsCount == nil || *in.LessonsCount < MinLessonsCount || *in.LessonsCount > MaxLessonsCount {
			return in, validate.Errorf("lessons_count", "Число занятий в абонементе — от %d до %d", MinLessonsCount, MaxLessonsCount)
		}
	default:
		return in, validate.Errorf("kind", "Тип тарифа — monthly или package")
	}

	if in.Price < 0 || in.Price > MaxPrice {
		return in, validate.Errorf("price", "Цена — от 0 до %d тыйын", MaxPrice)
	}
	return in, nil
}

func sameTariff(a, b Tariff) bool {
	sameCount := (a.LessonsCount == nil && b.LessonsCount == nil) ||
		(a.LessonsCount != nil && b.LessonsCount != nil && *a.LessonsCount == *b.LessonsCount)
	return a.Name == b.Name && a.Kind == b.Kind && a.Price == b.Price && sameCount
}

func fromRow(r sqlc.Tariff) Tariff {
	return Tariff{ID: r.ID, Name: r.Name, Kind: r.Kind, Price: r.Price, LessonsCount: r.LessonsCount, Archived: r.Archived}
}

func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx, q *sqlc.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx, s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
