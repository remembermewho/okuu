package bot

import (
	"context"
	"log/slog"
	"time"

	"okuu/internal/telegram"
)

const (
	pollTimeout = 30 // секунд long polling
	pollBackoff = 5 * time.Second
)

// Poll — режим polling (dev): снимает вебхук и читает getUpdates, пока не отменён ctx.
// Апдейты уходят в очередь бота (Run должен быть запущен).
func Poll(ctx context.Context, api telegram.API, b *Bot, logger *slog.Logger) {
	if err := api.SetWebhook(ctx, telegram.SetWebhookParams{URL: ""}); err != nil {
		logger.Warn("telegram: не удалось снять вебхук перед polling", "err", err)
	}
	logger.Info("telegram: бот запущен (polling)")
	var offset int64
	for ctx.Err() == nil {
		updates, err := api.GetUpdates(ctx, telegram.GetUpdatesParams{Offset: offset, Timeout: pollTimeout, AllowedUpdates: telegram.AllowedUpdates})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warn("telegram: getUpdates", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollBackoff):
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if err := b.Enqueue(ctx, u); err != nil {
				return // ctx отменён
			}
		}
	}
}

// SetupWebhook — режим webhook (prod): Telegram шлёт апдейты на url с заголовком
// X-Telegram-Bot-Api-Secret-Token = secret.
func SetupWebhook(ctx context.Context, api telegram.API, url, secret string) error {
	return api.SetWebhook(ctx, telegram.SetWebhookParams{URL: url, SecretToken: secret, AllowedUpdates: telegram.AllowedUpdates})
}
