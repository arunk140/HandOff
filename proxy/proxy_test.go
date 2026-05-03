package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"handoff/config"
)

func TestProxyHandlerNoRoute(t *testing.T) {
	cfg := &config.Config{
		Listen: config.ListenConfig{Port: 8080},
		Global: config.GlobalConfig{Timeout: config.Duration(30 * time.Second)},
		Routes: []config.Route{
			{Path: "/api/**", Backend: "https://example.com"},
		},
	}
	cfg.Validate()

	cfgPtr := &atomic.Pointer[config.Config]{}
	cfgPtr.Store(cfg)

	handler := NewProxyHandler(cfgPtr, nil)

	req := httptest.NewRequest("GET", "/other", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestProxyHandlerForwardsToBackend(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Backend", "true")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend response: " + string(body)))
	}))
	defer backend.Close()

	cfg := &config.Config{
		Listen: config.ListenConfig{Port: 8080},
		Global: config.GlobalConfig{Timeout: config.Duration(30 * time.Second)},
		Routes: []config.Route{
			{Path: "/api/**", Backend: backend.URL},
		},
	}
	cfg.Validate()

	cfgPtr := &atomic.Pointer[config.Config]{}
	cfgPtr.Store(cfg)

	handler := NewProxyHandler(cfgPtr, nil)

	req := httptest.NewRequest("POST", "/api/users", strings.NewReader("hello"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Backend") != "true" {
		t.Errorf("expected X-Backend header from backend")
	}
	body := rec.Body.String()
	if body != "backend response: hello" {
		t.Errorf("expected 'backend response: hello', got '%s'", body)
	}
}

func TestProxyHandlerWebhookFires(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer backend.Close()

	var webhookCalled atomic.Int32
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCalled.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	cfg := &config.Config{
		Listen: config.ListenConfig{Port: 8080},
		Global: config.GlobalConfig{Timeout: config.Duration(30 * time.Second)},
		Routes: []config.Route{
			{
				Path:    "/api/**",
				Backend: backend.URL,
				Webhooks: []config.WebhookConfig{
					{
						Type:    "http",
						URL:     webhookServer.URL,
						Method:  "POST",
						Payload: "metadata",
						Retry:   config.RetryConfig{Attempts: 1},
					},
				},
			},
		},
	}
	cfg.Validate()

	cfgPtr := &atomic.Pointer[config.Config]{}
	cfgPtr.Store(cfg)

	handler := NewProxyHandler(cfgPtr, nil)

	req := httptest.NewRequest("POST", "/api/data", strings.NewReader("test"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	time.Sleep(300 * time.Millisecond)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201 from backend, got %d", rec.Code)
	}
	if webhookCalled.Load() != 1 {
		t.Errorf("expected 1 webhook call, got %d", webhookCalled.Load())
	}
}
