package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"handoff/config"
	"handoff/matcher"
	"handoff/webhook"
)

type ProxyHandler struct {
	cfg        *atomic.Pointer[config.Config]
	dispatcher *webhook.Dispatcher
	proxies    sync.Map
	secrets    map[string]string
}

func NewProxyHandler(cfg *atomic.Pointer[config.Config], secrets map[string]string) *ProxyHandler {
	return &ProxyHandler{
		cfg:        cfg,
		dispatcher: webhook.NewDispatcher(secrets),
		secrets:    secrets,
	}
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfg.Load()
	m := matcher.New(cfg.Routes)
	route := m.Match(r.URL.Path, r.Method)

	if route == nil {
		slog.Warn("no route matched", "path", r.URL.Path, "method", r.Method)
		http.Error(w, "no matching route", http.StatusBadGateway)
		return
	}

	requestID := generateID()
	clientIP := getClientIP(r)
	contentType := r.Header.Get("Content-Type")

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read request body", "error", err, "request_id", requestID)
	}
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	recorder := &responseRecorder{ResponseWriter: w}
	start := time.Now()

	proxy := h.getProxy(route.Backend, cfg)
	proxy.ServeHTTP(recorder, r)

	latency := time.Since(start)

	payloadCtx := webhook.PayloadContext{
		Request: webhook.RequestInfo{
			Method:      r.Method,
			Path:        r.URL.Path,
			Headers:     r.Header,
			ClientIP:    clientIP,
			Query:       r.URL.RawQuery,
			ID:          requestID,
			ContentType: contentType,
		},
		Response: webhook.ResponseInfo{
			StatusCode: recorder.statusCode,
			LatencyMs:  latency.Milliseconds(),
		},
		Timestamp: start,
		Secrets:   h.secrets,
	}

	if len(route.Webhooks) > 0 {
		go func() {
			h.dispatcher.Fire(context.Background(), route.Webhooks, payloadCtx, bodyBytes)
		}()
	}

	slog.Info("request proxied",
		"request_id", requestID,
		"method", r.Method,
		"path", r.URL.Path,
		"backend", route.Backend,
		"status", recorder.statusCode,
		"latency_ms", latency.Milliseconds(),
		"client_ip", clientIP,
	)
}

func (h *ProxyHandler) getProxy(backend string, cfg *config.Config) *httputil.ReverseProxy {
	if p, ok := h.proxies.Load(backend); ok {
		return p.(*httputil.ReverseProxy)
	}

	target, err := url.Parse(backend)
	if err != nil {
		panic(fmt.Sprintf("invalid backend URL: %s", backend))
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: time.Duration(cfg.Global.Timeout),
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	proxy.Transport = transport

	h.proxies.Store(backend, proxy)
	return proxy
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	wrote      bool
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.statusCode = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.statusCode = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
