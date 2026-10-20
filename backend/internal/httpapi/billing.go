package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/httpx"
)

type operationDTO struct {
	ID           uuid.UUID   `json:"id"`
	StudentID    uuid.UUID   `json:"student_id"`
	Kind         string      `json:"kind"`
	Amount       int64       `json:"amount"` // тыйыны со знаком: начисление и возврат < 0, оплата > 0
	Method       *string     `json:"method"`
	Comment      *string     `json:"comment"`
	EnrollmentID *uuid.UUID  `json:"enrollment_id"`
	Period       *civil.Date `json:"period"`
	DueDate      *civil.Date `json:"due_date"`
	ListPrice    *int64      `json:"list_price"`
	Discount     *int64      `json:"discount"`
	CreatedAt    time.Time   `json:"created_at"`
	CreatedBy    *uuid.UUID  `json:"created_by"` // null — система (billing.monthly)
	Cancelled    bool        `json:"cancelled"`
	CancelledAt  *time.Time  `json:"cancelled_at"`
	CancelledBy  *uuid.UUID  `json:"cancelled_by"`
	CancelReason *string     `json:"cancel_reason"`
}

type debtorParentDTO struct {
	Name     *string `json:"name"`
	Phone    *string `json:"phone"`
	TgStatus *string `json:"tg_status"`
}

type debtorDTO struct {
	StudentID     uuid.UUID        `json:"student_id"`
	FullName      string           `json:"full_name"`
	Status        string           `json:"status"`
	Groups        []refDTO         `json:"groups"`
	Parent        *debtorParentDTO `json:"parent"`
	Debt          int64            `json:"debt"` // тыйыны, > 0
	DaysOverdue   int              `json:"days_overdue"`
	OldestDueDate civil.Date       `json:"oldest_due_date"`
}

type summaryDTO struct {
	Period       civil.Date `json:"period"`
	Charged      int64      `json:"charged"`
	Collected    int64      `json:"collected"`
	DebtTotal    int64      `json:"debt_total"`
	DebtorsCount int        `json:"debtors_count"`
}

type debtorsDTO struct {
	listDTO[debtorDTO]
	Summary summaryDTO `json:"summary"`
}

type operationRequest struct {
	Kind    string  `json:"kind"`
	Amount  int64   `json:"amount"`
	Method  *string `json:"method"`
	Comment *string `json:"comment"`
}

type cancelRequest struct {
	Reason string `json:"reason"`
}

// createOperation — оплата / возврат / поправка. Новая — 201; повтор с тем же Idempotency-Key — 200.
func (h *handlers) createOperation(w http.ResponseWriter, r *http.Request) {
	studentID, ok := uuidParam(w, r, "studentId")
	if !ok {
		return
	}
	var req operationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	op, created, err := h.billing.AddOperation(r.Context(), m.UserID, m.CenterID, studentID, billing.OperationInput{
		Kind: req.Kind, Amount: req.Amount, Method: req.Method, Comment: req.Comment,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, toOperationDTO(op))
}

func (h *handlers) cancelOperation(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "ledgerId")
	if !ok {
		return
	}
	var req cancelRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	op, err := h.billing.Cancel(r.Context(), m.UserID, m.Role == auth.RoleOwner, m.CenterID, id, req.Reason)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toOperationDTO(op))
}

func (h *handlers) studentLedger(w http.ResponseWriter, r *http.Request) {
	studentID, ok := uuidParam(w, r, "studentId")
	if !ok {
		return
	}
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > billing.MaxLimit {
			validationError(w, "limit", "limit — число от 1 до "+strconv.Itoa(billing.MaxLimit))
			return
		}
		limit = n
	}
	m, _ := auth.MembershipFrom(r.Context())
	ops, next, err := h.billing.History(r.Context(), m.CenterID, studentID, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]operationDTO, 0, len(ops))
	for _, op := range ops {
		out = append(out, toOperationDTO(op))
	}
	httpx.JSON(w, http.StatusOK, pageOf(out, next))
}

func (h *handlers) listDebtors(w http.ResponseWriter, r *http.Request) {
	var groupID *uuid.UUID
	if v := r.URL.Query().Get("group"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			validationError(w, "group", "group — id группы")
			return
		}
		groupID = &id
	}
	m, _ := auth.MembershipFrom(r.Context())
	list, sum, err := h.billing.Debtors(r.Context(), m.CenterID, groupID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]debtorDTO, 0, len(list))
	for _, d := range list {
		groups := make([]refDTO, 0, len(d.Groups))
		for _, g := range d.Groups {
			groups = append(groups, refDTO{ID: g.ID, Name: g.Name})
		}
		dto := debtorDTO{
			StudentID: d.StudentID, FullName: d.FullName, Status: d.Status, Groups: groups,
			Debt: d.Debt, DaysOverdue: d.DaysOverdue, OldestDueDate: d.OldestDueDate,
		}
		if d.ParentName != nil {
			dto.Parent = &debtorParentDTO{Name: d.ParentName, Phone: d.ParentPhone, TgStatus: d.ParentTgStatus}
		}
		items = append(items, dto)
	}
	httpx.JSON(w, http.StatusOK, debtorsDTO{
		listDTO: listOf(items),
		Summary: summaryDTO{Period: sum.Period, Charged: sum.Charged, Collected: sum.Collected,
			DebtTotal: sum.DebtTotal, DebtorsCount: sum.DebtorsCount},
	})
}

func toOperationDTO(o billing.Operation) operationDTO {
	return operationDTO{
		ID: o.ID, StudentID: o.StudentID, Kind: o.Kind, Amount: o.Amount, Method: o.Method, Comment: o.Comment,
		EnrollmentID: o.EnrollmentID, Period: o.Period, DueDate: o.DueDate, ListPrice: o.ListPrice, Discount: o.Discount,
		CreatedAt: o.CreatedAt, CreatedBy: o.CreatedBy, Cancelled: o.Cancelled(), CancelledAt: o.CancelledAt,
		CancelledBy: o.CancelledBy, CancelReason: o.CancelReason,
	}
}
