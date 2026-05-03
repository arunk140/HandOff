package webhook

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuildPayloadMetadata(t *testing.T) {
	ctx := PayloadContext{
		Request: RequestInfo{
			Method:      "POST",
			Path:        "/api/users",
			ClientIP:    "1.2.3.4",
			ID:          "abc123",
			ContentType: "application/json",
		},
		Response: ResponseInfo{
			StatusCode: 200,
			LatencyMs:  42,
		},
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	data, ct, err := BuildPayload("metadata", "", nil, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/json" {
		t.Errorf("expected content-type application/json, got %s", ct)
	}

	var result PayloadContext
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Request.Method != "POST" {
		t.Errorf("expected POST, got %s", result.Request.Method)
	}
	if result.Response.StatusCode != 200 {
		t.Errorf("expected 200, got %d", result.Response.StatusCode)
	}
}

func TestBuildPayloadBody(t *testing.T) {
	ctx := PayloadContext{
		Request: RequestInfo{
			ContentType: "text/plain",
		},
	}

	body := []byte("hello world")
	data, ct, err := BuildPayload("body", "", body, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "text/plain" {
		t.Errorf("expected content-type text/plain, got %s", ct)
	}
	if string(data) != "hello world" {
		t.Errorf("expected body 'hello world', got '%s'", string(data))
	}
}

func TestBuildPayloadBodyDefaultContentType(t *testing.T) {
	ctx := PayloadContext{}
	body := []byte("raw data")

	_, ct, err := BuildPayload("body", "", body, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/octet-stream" {
		t.Errorf("expected default content-type, got %s", ct)
	}
}

func TestBuildPayloadCustom(t *testing.T) {
	ctx := PayloadContext{
		Request: RequestInfo{
			Method: "GET",
			Path:   "/users",
			ID:     "req-001",
		},
		Response: ResponseInfo{
			StatusCode: 201,
			LatencyMs:  100,
		},
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	tmpl := `{"event":"{{.Request.Method}} {{.Request.Path}}","status":{{.Response.StatusCode}}}`

	data, ct, err := BuildPayload("custom", tmpl, nil, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/json" {
		t.Errorf("expected content-type application/json, got %s", ct)
	}

	expected := `{"event":"GET /users","status":201}`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestRenderTemplate(t *testing.T) {
	ctx := PayloadContext{
		Request: RequestInfo{
			Method: "POST",
			Path:   "/hook",
		},
		Secrets: map[string]string{
			"token": "secret123",
		},
	}

	result, err := RenderTemplate("Bearer {{.Secrets.token}}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result != "Bearer secret123" {
		t.Errorf("expected 'Bearer secret123', got '%s'", result)
	}
}
