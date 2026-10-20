package httpapi

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/httpx"
	"okuu/internal/importer"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

type importPreviewDTO struct {
	PreviewID uuid.UUID        `json:"preview_id"`
	ExpiresAt time.Time        `json:"expires_at"`
	Summary   importer.Summary `json:"summary"`
	Rows      []importer.Row   `json:"rows"`
}

type importCommitRequest struct {
	PreviewID  uuid.UUID `json:"preview_id"`
	FirstMonth string    `json:"first_month"`
}

func (h *handlers) importTemplate(w http.ResponseWriter, r *http.Request) {
	m, _ := auth.MembershipFrom(r.Context())
	data, err := h.importer.Template(r.Context(), m.CenterID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", xlsxContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="okuu-import.xlsx"`)
	_, _ = w.Write(data)
}

// importPreview — multipart, поле file: .xlsx или .csv до 5 МБ.
func (h *handlers) importPreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, importer.MaxFileBytes+1<<20) // + запас на заголовки multipart
	if err := r.ParseMultipartForm(importer.MaxFileBytes + 1<<20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fileTooLarge(w)
			return
		}
		validationError(w, "file", "Загрузите файл в поле file (multipart/form-data)")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		validationError(w, "file", "Загрузите файл в поле file (multipart/form-data)")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, importer.MaxFileBytes+1))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if len(data) > importer.MaxFileBytes {
		fileTooLarge(w)
		return
	}

	m, _ := auth.MembershipFrom(r.Context())
	p, err := h.importer.Preview(r.Context(), m.UserID, m.CenterID, header.Filename, data)
	if errors.Is(err, importer.ErrFileTooLarge) {
		fileTooLarge(w)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, importPreviewDTO{PreviewID: p.ID, ExpiresAt: p.ExpiresAt, Summary: p.Summary, Rows: p.Rows})
}

func (h *handlers) importCommit(w http.ResponseWriter, r *http.Request) {
	var req importCommitRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	if req.PreviewID == uuid.Nil {
		validationError(w, "preview_id", "preview_id — id из ответа на превью")
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	res, err := h.importer.Commit(r.Context(), m.UserID, m.CenterID, req.PreviewID, req.FirstMonth)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

func fileTooLarge(w http.ResponseWriter) {
	httpx.JSON(w, http.StatusRequestEntityTooLarge, httpx.ErrorBody{Error: httpx.ErrorDetail{
		Code: "file_too_large", Message: "Файл больше 5 МБ — разбейте его на части", Field: "file",
	}})
}
