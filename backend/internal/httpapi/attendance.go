package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"okuu/internal/attendance"
	"okuu/internal/auth"
	"okuu/internal/civil"
	"okuu/internal/httpx"
)

type lessonStudentDTO struct {
	StudentID   uuid.UUID `json:"student_id"`
	FullName    string    `json:"full_name"`
	Status      *string   `json:"status"`       // present | absent | excused | null — не отмечен
	LessonsLeft *int32    `json:"lessons_left"` // только у абонемента
	HasDebt     *bool     `json:"has_debt,omitempty"`
}

type lessonDTO struct {
	ID            uuid.UUID          `json:"id"`
	Group         refDTO             `json:"group"`
	Date          civil.Date         `json:"date"`
	StartTime     civil.TimeOfDay    `json:"start_time"`
	EndTime       *civil.TimeOfDay   `json:"end_time"` // по расписанию; null — внеплановое
	Cancelled     bool               `json:"cancelled"`
	StudentsCount int                `json:"students_count"`
	MarkedCount   int                `json:"marked_count"`
	Students      []lessonStudentDTO `json:"students"`
}

type attendanceHistoryDTO struct {
	LessonID  uuid.UUID       `json:"lesson_id"`
	Date      civil.Date      `json:"date"`
	StartTime civil.TimeOfDay `json:"start_time"`
	Group     refDTO          `json:"group"`
	Status    string          `json:"status"`
	Deducted  bool            `json:"deducted"` // списало ли занятие с абонемента
}

type createLessonRequest struct {
	GroupID   uuid.UUID       `json:"group_id"`
	Date      civil.Date      `json:"date"`
	StartTime civil.TimeOfDay `json:"start_time"`
}

type patchLessonRequest struct {
	Cancelled *bool `json:"cancelled"`
}

type markRequest struct {
	Marks []struct {
		StudentID uuid.UUID `json:"student_id"`
		Status    *string   `json:"status"`
	} `json:"marks"`
}

// viewer — кто смотрит занятия: преподаватель видит только свои группы и не видит денег.
func viewer(r *http.Request) (uuid.UUID, attendance.Viewer) {
	m, _ := auth.MembershipFrom(r.Context())
	return m.CenterID, attendance.Viewer{UserID: m.UserID, Teacher: m.Role == auth.RoleTeacher}
}

func (h *handlers) listLessons(w http.ResponseWriter, r *http.Request) {
	centerID, v := viewer(r)
	date, err := civil.Parse(r.URL.Query().Get("date"))
	if err != nil {
		validationError(w, "date", "date — дата в формате ГГГГ-ММ-ДД")
		return
	}
	list, err := h.attendance.LessonsForDate(r.Context(), centerID, v, date)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]lessonDTO, 0, len(list))
	for _, l := range list {
		out = append(out, toLessonDTO(l, v))
	}
	httpx.JSON(w, http.StatusOK, listOf(out))
}

func (h *handlers) getLessonAttendance(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "lessonId")
	if !ok {
		return
	}
	centerID, v := viewer(r)
	l, err := h.attendance.Get(r.Context(), centerID, v, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toLessonDTO(l, v))
}

func (h *handlers) createLesson(w http.ResponseWriter, r *http.Request) {
	var req createLessonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	centerID, v := viewer(r)
	l, err := h.attendance.Create(r.Context(), v.UserID, centerID, req.GroupID, req.Date, req.StartTime)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toLessonDTO(l, v))
}

func (h *handlers) patchLesson(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "lessonId")
	if !ok {
		return
	}
	var req patchLessonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	if req.Cancelled == nil {
		validationError(w, "cancelled", "cancelled — true или false")
		return
	}
	centerID, v := viewer(r)
	l, err := h.attendance.SetCancelled(r.Context(), v.UserID, centerID, id, *req.Cancelled)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toLessonDTO(l, v))
}

func (h *handlers) putAttendance(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "lessonId")
	if !ok {
		return
	}
	var req markRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	marks := make([]attendance.MarkInput, 0, len(req.Marks))
	for _, m := range req.Marks {
		marks = append(marks, attendance.MarkInput{StudentID: m.StudentID, Status: m.Status})
	}
	centerID, v := viewer(r)
	l, err := h.attendance.Mark(r.Context(), centerID, v, id, marks)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toLessonDTO(l, v))
}

func (h *handlers) studentAttendance(w http.ResponseWriter, r *http.Request) {
	studentID, ok := uuidParam(w, r, "studentId")
	if !ok {
		return
	}
	centerID, _ := viewer(r)
	var month civil.Date
	if m := r.URL.Query().Get("month"); m != "" {
		d, err := civil.Parse(m + "-01")
		if err != nil {
			validationError(w, "month", "month — месяц в формате ГГГГ-ММ")
			return
		}
		month = d
	} else {
		today, err := h.attendance.Today(r.Context(), centerID)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		month = today
	}
	items, err := h.attendance.StudentHistory(r.Context(), centerID, studentID, month)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]attendanceHistoryDTO, 0, len(items))
	for _, it := range items {
		out = append(out, attendanceHistoryDTO{LessonID: it.LessonID, Date: it.Date, StartTime: it.Start,
			Group: refDTO{ID: it.GroupID, Name: it.GroupName}, Status: it.Status, Deducted: it.Deducted})
	}
	httpx.JSON(w, http.StatusOK, listOf(out))
}

// toLessonDTO — занятие для ответа; преподаватель не видит has_debt (деньги, R12 / роли).
func toLessonDTO(l attendance.Lesson, v attendance.Viewer) lessonDTO {
	dto := lessonDTO{
		ID: l.ID, Group: refDTO{ID: l.GroupID, Name: l.GroupName}, Date: l.Date, StartTime: l.Start, EndTime: l.End,
		Cancelled: l.Cancelled, StudentsCount: len(l.Students), Students: make([]lessonStudentDTO, 0, len(l.Students)),
	}
	for _, s := range l.Students {
		st := lessonStudentDTO{StudentID: s.StudentID, FullName: s.FullName, Status: s.Mark, LessonsLeft: s.LessonsLeft}
		if !v.Teacher {
			debt := s.HasDebt
			st.HasDebt = &debt
		}
		if s.Mark != nil {
			dto.MarkedCount++
		}
		dto.Students = append(dto.Students, st)
	}
	return dto
}
