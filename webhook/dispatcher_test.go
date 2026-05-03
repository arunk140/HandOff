package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"handoff/config"
)

func TestDispatcherFire(t *testing.T) {
	var called atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	d := NewDispatcher(map[string]string{})

	webhooks := []config.WebhookConfig{
		{
			Type:   "http",
			URL:    server.URL,
			Method: "POST",
			Payload: "metadata",
			Retry: config.RetryConfig{
				Attempts: 1,
			},
		},
	}

	payloadCtx := PayloadContext{
		Request: RequestInfo{
			Method:   "POST",
			Path:     "/test",
			ID:       "test-001",
			ClientIP: "127.0.0.1",
		},
		Response: ResponseInfo{
			StatusCode: 200,
			LatencyMs:  10,
		},
		Timestamp: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	d.Fire(ctx, webhooks, payloadCtx, nil)

	time.Sleep(200 * time.Millisecond)

	if called.Load() != 1 {
		t.Errorf("expected webhook to be called once, got %d", called.Load())
	}
}

func TestDispatcherMultipleWebhooks(t *testing.T) {
	var called atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	d := NewDispatcher(map[string]string{})

	webhooks := []config.WebhookConfig{
		{
			Type:   "http",
			URL:    server.URL,
			Method: "POST",
			Payload: "metadata",
			Retry: config.RetryConfig{
				Attempts: 1,
			},
		},
		{
			Type:   "http",
			URL:    server.URL,
			Method: "POST",
			Payload: "metadata",
			Retry: config.RetryConfig{
				Attempts: 1,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	d.Fire(ctx, webhooks, PayloadContext{}, nil)

	time.Sleep(200 * time.Millisecond)

	if called.Load() != 2 {
		t.Errorf("expected 2 webhook calls, got %d", called.Load())
	}
}
