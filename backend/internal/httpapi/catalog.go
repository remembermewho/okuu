package httpapi

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/groups"
	"okuu/internal/httpx"
	"okuu/internal/optional"
	"okuu/internal/tariffs"
)

// --- Тарифы ---

type tariffDTO struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	Price        int64     `json:"price"` // тыйыны
	LessonsCount *int32    `json:"lessons_count"`
	Archived     bool      `json:"archived"`
}

type createTariffRequest struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Price        int64  `json:"price"`
	LessonsCount *int32 `json:"lessons_count"`
}

type patchTariffRequest struct {
	Name         optional.Field[string] `json:"name"`
	Kind         optional.Field[string] `json:"kind"`
	Price        optional.Field[int64]  `json:"price"`
	LessonsCount optional.Field[int32]  `json:"lessons_count"`
}

func (h *handlers) listTariffs(w http.ResponseWriter, r *http.Request) {
	archived, ok := archivedParam(w, r)
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	list, err := h.tariffs.List(r.Context(), m.CenterID, archived)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]tariffDTO, 0, len(list))
	for _, t := range list {
		out = append(out, toTariffDTO(t))
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handlers) createTariff(w http.ResponseWriter, r *http.Request) {
	var req createTariffRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	t, err := h.tariffs.Create(r.Context(), m.UserID, m.CenterID, tariffs.Input{
		Name: req.Name, Kind: req.Kind, Price: req.Price, LessonsCount: req.LessonsCount,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toTariffDTO(t))
}

func (h *handlers) patchTariff(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "tariffId")
	if !ok {
		return
	}
	var req patchTariffRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	t, err := h.tariffs.Update(r.Context(), m.UserID, m.CenterID, id, tariffs.Patch(req))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toTariffDTO(t))
}

func (h *handlers) archiveTariff(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "tariffId")
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	if err := h.tariffs.Archive(r.Context(), m.UserID, m.CenterID, id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toTariffDTO(t tariffs.Tariff) tariffDTO {
	return tariffDTO{ID: t.ID, Name: t.Name, Kind: t.Kind, Price: t.Price, LessonsCount: t.LessonsCount, Archived: t.Archived}
}

// --- Группы ---

type groupDTO struct {
	ID              uuid.UUID     `json:"id"`
	Name            string        `json:"name"`
	TeacherID       *uuid.UUID    `json:"teacher_id"`
	TeacherName     *string       `json:"teacher_name"`
	Schedule        []groups.Slot `json:"schedule"`
	DefaultTariffID *uuid.UUID    `json:"default_tariff_id"`
	Archived        bool          `json:"archived"`
	StudentsCount   int64         `json:"students_count"`
}

type createGroupRequest struct {
	Name            string        `json:"name"`
	TeacherID       *uuid.UUID    `json:"teacher_id"`
	Schedule        []groups.Slot `json:"schedule"`
	DefaultTariffID *uuid.UUID    `json:"default_tariff_id"`
}

type patchGroupRequest struct {
	Name            optional.Field[string]        `json:"name"`
	TeacherID       optional.Field[uuid.UUID]     `json:"teacher_id"`
	Schedule        optional.Field[[]groups.Slot] `json:"schedule"`
	DefaultTariffID optional.Field[uuid.UUID]     `json:"default_tariff_id"`
}

// onlyOwnGroups — для преподавателя только его группы; владелец и админ видят все.
func onlyOwnGroups(r *http.Request) (uuid.UUID, *uuid.UUID) {
	m, _ := auth.MembershipFrom(r.Context())
	if m.Role == auth.RoleTeacher {
		return m.CenterID, &m.UserID
	}
	return m.CenterID, nil
}

func (h *handlers) listGroups(w http.ResponseWriter, r *http.Request) {
	archived, ok := archivedParam(w, r)
	if !ok {
		return
	}
	centerID, teacher := onlyOwnGroups(r)
	list, err := h.groups.List(r.Context(), centerID, archived, teacher)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]groupDTO, 0, len(list))
	for _, g := range list {
		out = append(out, toGroupDTO(g))
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handlers) getGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "groupId")
	if !ok {
		return
	}
	centerID, teacher := onlyOwnGroups(r)
	g, err := h.groups.Get(r.Context(), centerID, id, teacher)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toGroupDTO(g))
}

func (h *handlers) createGroup(w http.ResponseWriter, r *http.Request) {
	var req createGroupRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	g, err := h.groups.Create(r.Context(), m.UserID, m.CenterID, groups.Input(req))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toGroupDTO(g))
}

func (h *handlers) patchGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "groupId")
	if !ok {
		return
	}
	var req patchGroupRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	g, err := h.groups.Update(r.Context(), m.UserID, m.CenterID, id, groups.Patch(req))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toGroupDTO(g))
}

func (h *handlers) archiveGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "groupId")
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	if err := h.groups.Archive(r.Context(), m.UserID, m.CenterID, id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toGroupDTO(g groups.Group) groupDTO {
	return groupDTO{
		ID: g.ID, Name: g.Name, TeacherID: g.TeacherID, TeacherName: g.TeacherName, Schedule: g.Schedule,
		DefaultTariffID: g.DefaultTariffID, Archived: g.Archived, StudentsCount: g.StudentsCount,
	}
}

// archivedParam — ?archived=true|false, по умолчанию false (только активные).
func archivedParam(w http.ResponseWriter, r *http.Request) (bool, bool) {
	v := r.URL.Query().Get("archived")
	if v == "" {
		return false, true
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, httpx.ErrorBody{Error: httpx.ErrorDetail{
			Code: "validation", Message: "archived — true или false", Field: "archived",
		}})
		return false, false
	}
	return b, true
}
