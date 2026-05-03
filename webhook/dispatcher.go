package webhook

import (
	"context"
	"log/slog"

	"handoff/config"
)

type Dispatcher struct {
	secrets map[string]string
}

func NewDispatcher(secrets map[string]string) *Dispatcher {
	return &Dispatcher{secrets: secrets}
}

func (d *Dispatcher) Fire(ctx context.Context, webhooks []config.WebhookConfig, payloadCtx PayloadContext, body []byte) {
	for _, wh := range webhooks {
		go func(wh config.WebhookConfig) {
			var action Action
			switch wh.Type {
			case "", "http":
				action = NewHTTPAction(wh, d.secrets)
			default:
				slog.Warn("unknown webhook type", "type", wh.Type)
				return
			}

			if err := action.Execute(ctx, payloadCtx, body); err != nil {
				slog.Error("webhook action failed", "type", wh.Type, "url", wh.URL, "error", err)
			}
		}(wh)
	}
}
