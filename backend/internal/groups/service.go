// Package groups — учебные группы центра (F3): название, преподаватель, расписание,
// тариф по умолчанию. Удаления нет — только архив. Преподаватель видит только свои группы.
package groups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/audit"
	"okuu/internal/db/sqlc"
	"okuu/internal/optional"
	"okuu/internal/validate"
)

// ErrNotFound — группы нет в этом центре (или она чужая для преподавателя).
var ErrNotFound = errors.New("группа не найдена")

// Group — группа со сведениями для списка.
type Group struct {
	ID              uuid.UUID
	Name            string
	TeacherID       *uuid.UUID
	TeacherName     *string
	Schedule        []Slot
	DefaultTariffID *uuid.UUID
	Archived        bool
	StudentsCount   int64
}

// Input — поля новой группы.
type Input struct {
	Name            string
	TeacherID       *uuid.UUID
	Schedule        []Slot
	DefaultTariffID *uuid.UUID
}

// Patch — частичное изменение; teacher_id и default_tariff_id можно сбросить в null.
type Patch struct {
	Name            optional.Field[string]
	TeacherID       optional.Field[uuid.UUID]
	Schedule        optional.Field[[]Slot]
	DefaultTariffID optional.Field[uuid.UUID]
}

// Service — группы.
type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// NewService создаёт сервис групп.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: sqlc.New(pool)}
}

// List — группы центра (активные или архивные). onlyTeacher != nil — только его группы.
func (s *Service) List(ctx context.Context, centerID uuid.UUID, archived bool, onlyTeacher *uuid.UUID) ([]Group, error) {
	rows, err := s.q.ListGroups(ctx, sqlc.ListGroupsParams{CenterID: centerID, Archived: archived, TeacherID: onlyTeacher})
	if err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(rows))
	for _, r := range rows {
		g, err := newGroup(sqlc.GetGroupRow(r))
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// Get — группа центра. onlyTeacher != nil и группа не его — ErrNotFound, как и для чужого центра.
func (s *Service) Get(ctx context.Context, centerID, id uuid.UUID, onlyTeacher *uuid.UUID) (Group, error) {
	row, err := s.q.GetGroup(ctx, sqlc.GetGroupParams{CenterID: centerID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, ErrNotFound
	}
	if err != nil {
		return Group{}, err
	}
	if onlyTeacher != nil && (row.TeacherID == nil || *row.TeacherID != *onlyTeacher) {
		return Group{}, ErrNotFound
	}
	return newGroup(row)
}

// Create создаёт группу. Преподаватель — сотрудник этого центра, тариф — этого центра и не архивный.
func (s *Service) Create(ctx context.Context, actor, centerID uuid.UUID, in Input) (Group, error) {
	var id uuid.UUID
	err := s.inTx(ctx, func(tx pgx.Tx, _ *sqlc.Queries) error {
		var err error
		id, err = s.CreateTx(ctx, tx, actor, centerID, in)
		return err
	})
	if err != nil {
		return Group{}, err
	}
	return s.Get(ctx, centerID, id, nil)
}

// CreateTx — то же, что Create, в транзакции вызывающего (импорт); возвращает id группы.
func (s *Service) CreateTx(ctx context.Context, tx pgx.Tx, actor, centerID uuid.UUID, in Input) (uuid.UUID, error) {
	in, err := validateInput(in)
	if err != nil {
		return uuid.Nil, err
	}
	q := s.q.WithTx(tx)
	if err := checkRefs(ctx, q, centerID, in.TeacherID, in.DefaultTariffID); err != nil {
		return uuid.Nil, err
	}
	schedule, err := json.Marshal(in.Schedule)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := q.CreateGroup(ctx, sqlc.CreateGroupParams{
		CenterID: centerID, Name: in.Name, TeacherID: in.TeacherID, Schedule: schedule, DefaultTariffID: in.DefaultTariffID,
	})
	if err != nil {
		return uuid.Nil, err
	}
	if err := audit.Record(ctx, tx, audit.Entry{
		CenterID: centerID, UserID: &actor, Action: audit.GroupCreate, Entity: "group", EntityID: &id, After: snapshot(in),
	}); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// Update применяет частичное изменение. Ссылки (преподаватель, тариф) проверяются только если меняются:
// оставить группе уже архивный тариф можно, назначить новый архивный — нет.
func (s *Service) Update(ctx context.Context, actor, centerID, id uuid.UUID, p Patch) (Group, error) {
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		row, err := q.GetGroupForUpdate(ctx, sqlc.GetGroupForUpdateParams{CenterID: centerID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var schedule []Slot
		if err := json.Unmarshal(row.Schedule, &schedule); err != nil {
			return fmt.Errorf("расписание группы %s: %w", id, err)
		}
		before := Input{Name: row.Name, TeacherID: row.TeacherID, Schedule: schedule, DefaultTariffID: row.DefaultTariffID}

		next := before
		if p.Name.Set {
			if p.Name.Value == nil {
				return validate.Errorf("name", "Название — обязательное поле")
			}
			next.Name = *p.Name.Value
		}
		if p.Schedule.Set {
			if p.Schedule.Value == nil {
				return validate.Errorf("schedule", "Расписание — массив, пустой массив — без расписания")
			}
			next.Schedule = *p.Schedule.Value
		}
		next.TeacherID = p.TeacherID.Apply(next.TeacherID)
		next.DefaultTariffID = p.DefaultTariffID.Apply(next.DefaultTariffID)

		if next, err = validateInput(next); err != nil {
			return err
		}
		var newTeacher, newTariff *uuid.UUID
		if !sameID(next.TeacherID, before.TeacherID) {
			newTeacher = next.TeacherID
		}
		if !sameID(next.DefaultTariffID, before.DefaultTariffID) {
			newTariff = next.DefaultTariffID
		}
		if err := checkRefs(ctx, q, centerID, newTeacher, newTariff); err != nil {
			return err
		}

		if next.Name == before.Name &&
			sameID(next.TeacherID, before.TeacherID) && sameID(next.DefaultTariffID, before.DefaultTariffID) &&
			slices.Equal(next.Schedule, before.Schedule) {
			return nil
		}
		data, err := json.Marshal(next.Schedule)
		if err != nil {
			return err
		}
		if err := q.UpdateGroup(ctx, sqlc.UpdateGroupParams{
			CenterID: centerID, ID: id, Name: next.Name, TeacherID: next.TeacherID, Schedule: data, DefaultTariffID: next.DefaultTariffID,
		}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.GroupUpdate, Entity: "group", EntityID: &id,
			Before: snapshot(before), After: snapshot(next),
		})
	})
	if err != nil {
		return Group{}, err
	}
	return s.Get(ctx, centerID, id, nil)
}

// Archive переводит группу в архив. Повторный вызов — без изменений и без записи в журнал.
func (s *Service) Archive(ctx context.Context, actor, centerID, id uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		row, err := q.GetGroupForUpdate(ctx, sqlc.GetGroupForUpdateParams{CenterID: centerID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if row.Archived {
			return nil
		}
		if err := q.ArchiveGroup(ctx, sqlc.ArchiveGroupParams{CenterID: centerID, ID: id}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.GroupArchive, Entity: "group", EntityID: &id,
			Before: map[string]any{"archived": false}, After: map[string]any{"archived": true},
		})
	})
}

func validateInput(in Input) (Input, error) {
	name, err := validate.Name("name", "Название", in.Name)
	if err != nil {
		return in, err
	}
	in.Name = name
	if in.Schedule == nil {
		in.Schedule = []Slot{}
	}
	if err := ValidateSchedule(in.Schedule); err != nil {
		return in, err
	}
	return in, nil
}

// checkRefs проверяет и блокирует (FOR SHARE) преподавателя и тариф до конца транзакции.
// nil — проверять нечего.
func checkRefs(ctx context.Context, q *sqlc.Queries, centerID uuid.UUID, teacherID, tariffID *uuid.UUID) error {
	if teacherID != nil {
		_, err := q.LockCenterMember(ctx, sqlc.LockCenterMemberParams{CenterID: centerID, UserID: *teacherID})
		if errors.Is(err, pgx.ErrNoRows) {
			return validate.Unprocessablef("teacher_id", "Преподаватель должен быть сотрудником этого центра")
		}
		if err != nil {
			return err
		}
	}
	if tariffID != nil {
		_, err := q.LockActiveTariff(ctx, sqlc.LockActiveTariffParams{CenterID: centerID, ID: *tariffID})
		if errors.Is(err, pgx.ErrNoRows) {
			return validate.Unprocessablef("default_tariff_id", "Тариф не найден в этом центре или в архиве")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func newGroup(r sqlc.GetGroupRow) (Group, error) {
	g := Group{
		ID: r.ID, Name: r.Name, TeacherID: r.TeacherID, TeacherName: r.TeacherName,
		DefaultTariffID: r.DefaultTariffID, Archived: r.Archived, StudentsCount: r.StudentsCount,
	}
	if err := json.Unmarshal(r.Schedule, &g.Schedule); err != nil {
		return Group{}, fmt.Errorf("расписание группы %s: %w", r.ID, err)
	}
	if g.Schedule == nil {
		g.Schedule = []Slot{}
	}
	return g, nil
}

func snapshot(in Input) map[string]any {
	return map[string]any{
		"name": in.Name, "teacher_id": in.TeacherID, "schedule": in.Schedule, "default_tariff_id": in.DefaultTariffID,
	}
}

func sameID(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
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
