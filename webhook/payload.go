package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
)

type RequestInfo struct {
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	Headers     map[string][]string `json:"headers"`
	ClientIP    string              `json:"client_ip"`
	Query       string              `json:"query,omitempty"`
	ID          string              `json:"id"`
	ContentType string              `json:"content_type,omitempty"`
}

type ResponseInfo struct {
	StatusCode int   `json:"status_code"`
	LatencyMs  int64 `json:"latency_ms"`
}

type PayloadContext struct {
	Request   RequestInfo       `json:"request"`
	Response  ResponseInfo      `json:"response"`
	Timestamp time.Time         `json:"timestamp"`
	Secrets   map[string]string `json:"-"`
}

func BuildPayload(mode, templateStr string, body []byte, ctx PayloadContext) ([]byte, string, error) {
	switch strings.ToLower(mode) {
	case "body":
		ct := ctx.Request.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		return body, ct, nil
	case "custom":
		return buildTemplatePayload(templateStr, ctx)
	default:
		return buildMetadataPayload(ctx)
	}
}

func buildMetadataPayload(ctx PayloadContext) ([]byte, string, error) {
	data, err := json.Marshal(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("metadata payload marshal: %w", err)
	}
	return data, "application/json", nil
}

func buildTemplatePayload(tmplStr string, ctx PayloadContext) ([]byte, string, error) {
	tmpl, err := template.New("payload").Funcs(sprig.FuncMap()).Parse(tmplStr)
	if err != nil {
		return nil, "", fmt.Errorf("template parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return nil, "", fmt.Errorf("template execute: %w", err)
	}
	return buf.Bytes(), "application/json", nil
}

func RenderTemplate(tmplStr string, ctx PayloadContext) (string, error) {
	tmpl, err := template.New("render").Funcs(sprig.FuncMap()).Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", err
	}
	return buf.String(), nil
}
