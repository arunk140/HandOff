package webhook

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"handoff/config"
)

type HTTPAction struct {
	config  config.WebhookConfig
	secrets map[string]string
	client  *http.Client
}

func NewHTTPAction(cfg config.WebhookConfig, secrets map[string]string) *HTTPAction {
	return &HTTPAction{
		config:  cfg,
		secrets: secrets,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (a *HTTPAction) Execute(ctx context.Context, payloadCtx PayloadContext, body []byte) error {
	payloadCtx.Secrets = a.secrets

	bodyBytes, contentType, err := BuildPayload(a.config.Payload, a.config.Template, body, payloadCtx)
	if err != nil {
		return fmt.Errorf("payload build: %w", err)
	}

	url, err := RenderTemplate(a.config.URL, payloadCtx)
	if err != nil {
		return fmt.Errorf("url template: %w", err)
	}

	method := a.config.Method
	if method == "" {
		method = "POST"
	}

	maxAttempts := a.config.Retry.Attempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := backoff(a.config.Retry.Backoff, attempt)
			slog.Warn("webhook retrying", "attempt", attempt+1, "max", maxAttempts, "delay", delay, "url", url)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Content-Type", contentType)

		for k, v := range a.config.Headers {
			renderedKey, err := RenderTemplate(k, payloadCtx)
			if err != nil {
				slog.Warn("webhook header key template error", "key", k, "error", err)
				continue
			}
			renderedVal, err := RenderTemplate(v, payloadCtx)
			if err != nil {
				slog.Warn("webhook header value template error", "key", k, "error", err)
				continue
			}
			req.Header.Set(renderedKey, renderedVal)
		}

		resp, err := a.client.Do(req)
		if err != nil {
			lastErr = err
			slog.Warn("webhook request failed", "attempt", attempt+1, "url", url, "error", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			slog.Info("webhook delivered", "url", url, "status", resp.StatusCode)
			return nil
		}

		lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
		slog.Warn("webhook bad status", "attempt", attempt+1, "url", url, "status", resp.StatusCode)
	}

	return fmt.Errorf("webhook failed after %d attempts: %w", maxAttempts, lastErr)
}

func backoff(strategy string, attempt int) time.Duration {
	switch strategy {
	case "linear":
		return time.Duration(attempt) * time.Second
	default:
		return time.Duration(math.Pow(2, float64(attempt))) * time.Second
	}
}
