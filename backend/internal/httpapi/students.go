package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/httpx"
	"okuu/internal/optional"
	"okuu/internal/students"
)

type parentDTO struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Phone    string    `json:"phone"`
	TgStatus string    `json:"tg_status"`
}

type refDTO struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type enrollmentTariffDTO struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Kind  string    `json:"kind"`
	Price int64     `json:"price"` // текущая цена тарифа, тыйыны
}

type enrollmentDTO struct {
	ID            uuid.UUID           `json:"id"`
	Group         refDTO              `json:"group"`
	Tariff        enrollmentTariffDTO `json:"tariff"`
	StartDate     civil.Date          `json:"start_date"`
	EndDate       *civil.Date         `json:"end_date"`
	LessonsLeft   *int32              `json:"lessons_left"`
	BilledThrough *civil.Date         `json:"billed_through"` // monthly: последний период, решение по которому принято
	Active        bool                `json:"active"`
}

type chargeDTO struct {
	ID        uuid.UUID   `json:"id"`
	Amount    int64       `json:"amount"`
	ListPrice int64       `json:"list_price"`
	Discount  int64       `json:"discount"`
	Period    *civil.Date `json:"period"`
	DueDate   civil.Date  `json:"due_date"`
}

type studentDTO struct {
	ID          uuid.UUID         `json:"id"`
	FullName    string            `json:"full_name"`
	BirthDate   *civil.Date       `json:"birth_date"`
	Status      string            `json:"status"`
	Discount    *billing.Discount `json:"discount"`
	CreatedAt   time.Time         `json:"created_at"`
	Parent      *parentDTO        `json:"parent"`
	Enrollments []enrollmentDTO   `json:"enrollments"`
	Balance     int64             `json:"balance"` // тыйыны; < 0 — долг
}

type createdStudentDTO struct {
	studentDTO
	ParentExisting bool `json:"parent_existing"`
}

type studentListItemDTO struct {
	ID        uuid.UUID         `json:"id"`
	FullName  string            `json:"full_name"`
	BirthDate *civil.Date       `json:"birth_date"`
	Status    string            `json:"status"`
	Discount  *billing.Discount `json:"discount"`
	Parent    *parentDTO        `json:"parent"`
	Groups    []refDTO          `json:"groups"`
	Balance   int64             `json:"balance"`
}

type enrollResultDTO struct {
	Enrollment enrollmentDTO `json:"enrollment"`
	Charge     *chargeDTO    `json:"charge"`
}

type createStudentRequest struct {
	FullName  string                       `json:"full_name"`
	BirthDate *civil.Date                  `json:"birth_date"`
	Parent    struct{ Name, Phone string } `json:"parent"`
	Discount  *billing.Discount            `json:"discount"`
}

type patchStudentRequest struct {
	FullName  optional.Field[string]           `json:"full_name"`
	BirthDate optional.Field[civil.Date]       `json:"birth_date"`
	Status    optional.Field[string]           `json:"status"`
	Discount  optional.Field[billing.Discount] `json:"discount"`
	Parent    *struct {
		Name  optional.Field[string] `json:"name"`
		Phone optional.Field[string] `json:"phone"`
	} `json:"parent"`
}

type enrollRequest struct {
	GroupID      uuid.UUID  `json:"group_id"`
	TariffID     *uuid.UUID `json:"tariff_id"`
	StartDate    civil.Date `json:"start_date"`
	FirstMonth   string     `json:"first_month"`
	ManualAmount *int64     `json:"manual_amount"`
}

type endEnrollmentRequest struct {
	EndDate *civil.Date `json:"end_date"`
}

func (h *handlers) listStudents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := students.ListFilter{Q: q.Get("q"), Status: q.Get("status"), Tg: q.Get("tg"), Cursor: q.Get("cursor")}
	if v := q.Get("group"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			validationError(w, "group", "group — id группы")
			return
		}
		f.GroupID = &id
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > students.MaxLimit {
			validationError(w, "limit", "limit — число от 1 до "+strconv.Itoa(students.MaxLimit))
			return
		}
		f.Limit = n
	}

	m, _ := auth.MembershipFrom(r.Context())
	items, next, err := h.students.List(r.Context(), m.CenterID, f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]studentListItemDTO, 0, len(items))
	for _, it := range items {
		groups := make([]refDTO, 0, len(it.Groups))
		for _, g := range it.Groups {
			groups = append(groups, refDTO{ID: g.ID, Name: g.Name})
		}
		out = append(out, studentListItemDTO{
			ID: it.ID, FullName: it.FullName, BirthDate: it.BirthDate, Status: it.Status, Discount: it.Discount,
			Parent: toParentDTO(it.Parent), Groups: groups, Balance: it.Balance,
		})
	}
	httpx.JSON(w, http.StatusOK, pageOf(out, next))
}

func (h *handlers) createStudent(w http.ResponseWriter, r *http.Request) {
	var req createStudentRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	st, existing, err := h.students.Create(r.Context(), m.UserID, m.CenterID, students.CreateInput{
		FullName: req.FullName, BirthDate: req.BirthDate, Discount: req.Discount,
		Parent: students.ParentInput{Name: req.Parent.Name, Phone: req.Parent.Phone},
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, createdStudentDTO{studentDTO: toStudentDTO(st), ParentExisting: existing})
}

func (h *handlers) getStudent(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "studentId")
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	st, err := h.students.Get(r.Context(), m.CenterID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toStudentDTO(st))
}

func (h *handlers) patchStudent(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "studentId")
	if !ok {
		return
	}
	var req patchStudentRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	p := students.Patch{FullName: req.FullName, BirthDate: req.BirthDate, Status: req.Status, Discount: req.Discount}
	if req.Parent != nil {
		p.Parent = &students.ParentPatch{Name: req.Parent.Name, Phone: req.Parent.Phone}
	}
	m, _ := auth.MembershipFrom(r.Context())
	st, err := h.students.Update(r.Context(), m.UserID, m.CenterID, id, p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toStudentDTO(st))
}

func (h *handlers) createEnrollment(w http.ResponseWriter, r *http.Request) {
	studentID, ok := uuidParam(w, r, "studentId")
	if !ok {
		return
	}
	var req enrollRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	enr, charge, err := h.students.Enroll(r.Context(), m.UserID, m.CenterID, studentID, students.EnrollInput(req))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, enrollResultDTO{Enrollment: toEnrollmentDTO(enr), Charge: toChargeDTO(charge)})
}

func (h *handlers) endEnrollment(w http.ResponseWriter, r *http.Request) {
	studentID, ok := uuidParam(w, r, "studentId")
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "enrollmentId")
	if !ok {
		return
	}
	// Тело необязательно: без него запись завершается сегодняшним днём.
	var req endEnrollmentRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil && !errors.Is(err, io.EOF) {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	enr, err := h.students.EndEnrollment(r.Context(), m.UserID, m.CenterID, studentID, id, req.EndDate)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toEnrollmentDTO(enr))
}

func (h *handlers) renewEnrollment(w http.ResponseWriter, r *http.Request) {
	studentID, ok := uuidParam(w, r, "studentId")
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "enrollmentId")
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	enr, charge, err := h.students.Renew(r.Context(), m.UserID, m.CenterID, studentID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, enrollResultDTO{Enrollment: toEnrollmentDTO(enr), Charge: toChargeDTO(&charge)})
}

func toStudentDTO(s students.Student) studentDTO {
	enr := make([]enrollmentDTO, 0, len(s.Enrollments))
	for _, e := range s.Enrollments {
		enr = append(enr, toEnrollmentDTO(e))
	}
	return studentDTO{
		ID: s.ID, FullName: s.FullName, BirthDate: s.BirthDate, Status: s.Status, Discount: s.Discount,
		CreatedAt: s.CreatedAt, Parent: toParentDTO(s.Parent), Enrollments: enr, Balance: s.Balance,
	}
}

func toParentDTO(p *students.Parent) *parentDTO {
	if p == nil {
		return nil
	}
	return &parentDTO{ID: p.ID, Name: p.Name, Phone: p.Phone, TgStatus: p.TgStatus}
}

func toEnrollmentDTO(e students.Enrollment) enrollmentDTO {
	return enrollmentDTO{
		ID:            e.ID,
		Group:         refDTO{ID: e.GroupID, Name: e.GroupName},
		Tariff:        enrollmentTariffDTO{ID: e.TariffID, Name: e.TariffName, Kind: e.TariffKind, Price: e.TariffPrice},
		StartDate:     e.StartDate,
		EndDate:       e.EndDate,
		LessonsLeft:   e.LessonsLeft,
		BilledThrough: e.BilledThrough,
		Active:        e.Active(),
	}
}

func toChargeDTO(c *billing.Charge) *chargeDTO {
	if c == nil {
		return nil
	}
	return &chargeDTO{ID: c.ID, Amount: c.Amount, ListPrice: c.ListPrice, Discount: c.Discount, Period: c.Period, DueDate: c.DueDate}
}

func validationError(w http.ResponseWriter, field, message string) {
	httpx.JSON(w, http.StatusBadRequest, httpx.ErrorBody{Error: httpx.ErrorDetail{Code: "validation", Message: message, Field: field}})
}
