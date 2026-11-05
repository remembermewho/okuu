package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/httpx"
	"okuu/internal/reminders"
)

type remindersRequest struct {
	StudentIDs []uuid.UUID `json:"student_ids"`
	All        bool        `json:"all"`
}

type skippedDTO struct {
	StudentID uuid.UUID `json:"student_id"`
	Reason    string    `json:"reason"` // no_telegram | stopped | blocked | rate_limited | no_debt | send_failed
}

type remindersResponse struct {
	Sent     int `json:"sent"`     // учеников, о которых напомнили
	Messages int `json:"messages"` // сообщений родителям (дети одного родителя — одно сообщение)
	// учеников во всех отправленных сообщениях, с братьями и сёстрами, которых показали для полноты
	StudentsInMessages int          `json:"students_in_messages"`
	Skipped            []skippedDTO `json:"skipped"`
}

// sendReminders — POST /c/{id}/reminders: «Напомнить» по ученикам или по всем должникам (R17–R18).
func (h *handlers) sendReminders(w http.ResponseWriter, r *http.Request) {
	var req remindersRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	res, err := h.reminders.Manual(r.Context(), m.UserID, m.CenterID, reminders.ManualInput{StudentIDs: req.StudentIDs, All: req.All})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := remindersResponse{Sent: res.Sent, Messages: res.Messages, StudentsInMessages: res.InMessages,
		Skipped: make([]skippedDTO, 0, len(res.Skipped))}
	for _, s := range res.Skipped {
		out.Skipped = append(out.Skipped, skippedDTO{StudentID: s.StudentID, Reason: s.Reason})
	}
	httpx.JSON(w, http.StatusOK, out)
}
