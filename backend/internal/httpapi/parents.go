package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/httpx"
	"okuu/internal/telegram"
)

// UpdateQueue — очередь апдейтов бота (реализует *bot.Bot).
type UpdateQueue interface {
	Enqueue(ctx context.Context, u telegram.Update) error
}

type parentInviteDTO struct {
	ID          uuid.UUID         `json:"id"`
	BotURL      string            `json:"bot_url"`
	ExpiresAt   time.Time         `json:"expires_at"`
	Texts       map[string]string `json:"texts"` // ru, ky — для WhatsApp
	Lang        string            `json:"lang"`  // язык родителя: на нём whatsapp_url
	WhatsAppURL string            `json:"whatsapp_url"`
	TgStatus    string            `json:"tg_status"`
}

// createParentInvite — POST /c/{id}/parents/{parentId}/invite: ссылка на бота и текст для WhatsApp.
func (h *handlers) createParentInvite(w http.ResponseWriter, r *http.Request) {
	pid, ok := uuidParam(w, r, "parentId")
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	inv, err := h.parents.CreateInvite(r.Context(), m.UserID, m.CenterID, pid)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, parentInviteDTO{
		ID: inv.ID, BotURL: inv.BotURL, ExpiresAt: inv.ExpiresAt, Texts: inv.Texts, Lang: inv.Lang,
		WhatsAppURL: inv.WhatsAppURL, TgStatus: inv.TgStatus,
	})
}

// webhookEnqueueTimeout — сколько вебхук ждёт места в очереди; дальше 503 и Telegram повторит.
const webhookEnqueueTimeout = 5 * time.Second

// telegramWebhook — POST /webhooks/telegram/{secret}. Секрет проверяется и в пути, и в заголовке
// X-Telegram-Bot-Api-Secret-Token; не совпал — 404, как будто маршрута нет. Апдейт уходит
// в очередь бота, ответ — сразу, не дожидаясь обработки.
func (h *handlers) telegramWebhook(w http.ResponseWriter, r *http.Request) {
	secret := []byte(h.tgWebhookSecret)
	if len(secret) == 0 ||
		subtle.ConstantTimeCompare([]byte(chi.URLParam(r, "secret")), secret) != 1 ||
		subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")), secret) != 1 {
		httpx.Error(w, http.StatusNotFound, "not_found", "Не найдено")
		return
	}
	// Поля апдейта, которые бот не знает, пропускаются: Telegram добавляет новые без предупреждения.
	var u telegram.Update
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, httpx.MaxBodyBytes)).Decode(&u); err != nil {
		badJSON(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), webhookEnqueueTimeout)
	defer cancel()
	if err := h.bot.Enqueue(ctx, u); err != nil {
		h.logger.Warn("telegram: вебхук — очередь занята", "update_id", u.UpdateID, "err", errors.Unwrap(err))
		httpx.Error(w, http.StatusServiceUnavailable, "busy", "Очередь занята, повторите позже")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
